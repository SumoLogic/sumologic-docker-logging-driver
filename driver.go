package main

import (
  "compress/gzip"
  "context"
  "crypto/tls"
  "crypto/x509"
  "fmt"
  "io"
  "io/ioutil"
  "net/http"
  "net/url"
  "sync"
  "syscall"
  "time"

  "github.com/docker/docker/daemon/logger"
  "github.com/pkg/errors"
  "github.com/sirupsen/logrus"
  "github.com/tonistiigi/fifo"
)

const (
  /* Log options that user can set via log-opt flag when starting container. */
  /* HTTP source URL for the SumoLogic HTTP source the logs should be sent to. This option is required. */
  logOptUrl = "sumo-url"
  /* Gzip compression. If set to true, messages will be compressed before sending to Sumo. */
  logOptGzipCompression = "sumo-compress"
  /* Gzip compression level.
    Valid values are -1 (default), 0 (no compression), 1 (best speed) ... 9 (best compression). */
  logOptGzipCompressionLevel = "sumo-compress-level"
  /* Used for TLS configuration.
    Allows users to set a proxy URL. */
  logOptProxyUrl = "sumo-proxy-url"
  /* Used for TLS configuration.
    If set to true, TLS will not perform verification on the certificate presented by the server. */
  logOptInsecureSkipVerify = "sumo-insecure-skip-verify"
  /* Used for TLS configuration.
    Allows users to specify the path to a custom root certificate. */
  logOptRootCaPath = "sumo-root-ca-path"
  /* Used for TLS configuration.
    Allows users to specify server name with which to validate the server certificate. */
  logOptServerName = "sumo-server-name"
  /* The maximum time the driver waits for number of logs to reach the batch size before sending logs,
    even if the number of logs is less than the batch size. */
  logOptSendingInterval = "sumo-sending-interval"
  /* The maximum number of pending logs the container can send to the driver
    before the driver must ingest them. */
  logOptQueueSize = "sumo-queue-size"
  /* The number of logs the driver should wait for before sending them in a batch.
    If the number of logs never reaches the batch size, the driver will send the logs in smaller
    batches at predefined intervals; see sending interval. */
  logOptBatchSize = "sumo-batch-size"

  defaultGzipCompression = false
  defaultGzipCompressionLevel = gzip.DefaultCompression
  defaultInsecureSkipVerify = false
  defaultSendingInterval  = 2 * time.Second
  defaultQueueSize = 4000
  defaultBatchSize = 1000

  fileMode = 0700
)

type SumoDriver interface {
  StartLogging(string, logger.Info) error
  StopLogging(string) error
}

type sumoDriver struct {
  loggers map[string]*sumoLogger
  mu sync.Mutex
}

type HttpClient interface {
  Do(req *http.Request) (*http.Response, error)
}

type sumoLogger struct {
  httpSourceUrl string
  httpClient HttpClient

  proxyUrl *url.URL
  tlsConfig *tls.Config

  gzipCompression bool
  gzipCompressionLevel int

  inputQueueFile io.ReadWriteCloser
  logQueue chan *sumoLog
  sendingInterval time.Duration
  batchSize int
}

type sumoLog struct {
  line []byte
  source string
  time string
  isPartial bool
}

func newSumoDriver() *sumoDriver {
  return &sumoDriver{
    loggers: make(map[string]*sumoLogger),
  }
}

func (sumoDriver *sumoDriver) StartLogging(file string, info logger.Info) error {
  newSumoLogger, err := sumoDriver.startLoggingInternal(file, info)
  if err != nil {
    return err
  }
  go consumeLogsFromFile(newSumoLogger)
  go bufferLogsForSending(newSumoLogger)
  return nil
}

func (sumoDriver *sumoDriver) startLoggingInternal(file string, info logger.Info) (*sumoLogger, error) {
  sumoDriver.mu.Lock()
  if _, exists := sumoDriver.loggers[file]; exists {
    sumoDriver.mu.Unlock()
    return nil, fmt.Errorf("a logger for %q already exists", file)
  }
  sumoDriver.mu.Unlock()

  inputQueueFile, err := fifo.OpenFifo(context.Background(), file, syscall.O_RDONLY, fileMode)
  if err != nil {
    return nil, errors.Wrapf(err, "error opening logger fifo: %q", file)
  }

  gzipCompression := parseLogOptBoolean(info, logOptGzipCompression, defaultGzipCompression)
  gzipCompressionLevel := parseLogOptInt(info, logOptGzipCompressionLevel, defaultGzipCompressionLevel)
  if gzipCompressionLevel < defaultGzipCompressionLevel || gzipCompressionLevel > gzip.BestCompression {
    logrus.Error(fmt.Errorf("Not supported level '%s' for %s (supported values between %d and %d). Using default compression.",
      info.Config[logOptGzipCompressionLevel], logOptGzipCompressionLevel, defaultGzipCompressionLevel, gzip.BestCompression))
    gzipCompressionLevel = defaultGzipCompressionLevel
  }

  tlsConfig := &tls.Config{}
  tlsConfig.InsecureSkipVerify = parseLogOptBoolean(info, logOptInsecureSkipVerify, defaultInsecureSkipVerify)
  if rootCaPath, exists := info.Config[logOptRootCaPath]; exists {
    rootCa, err := ioutil.ReadFile(rootCaPath)
    if err != nil {
      return nil, err
    }
    rootCaPool := x509.NewCertPool()
    rootCaPool.AppendCertsFromPEM(rootCa)
    tlsConfig.RootCAs = rootCaPool
  }
  if serverName, exists := info.Config[logOptServerName]; exists {
    tlsConfig.ServerName = serverName
  }

  transport := &http.Transport{}
  proxyUrl := parseLogOptProxyUrl(info, logOptProxyUrl, nil)
  transport.Proxy = http.ProxyURL(proxyUrl)
  transport.TLSClientConfig = tlsConfig

  httpClient := &http.Client{
    Transport: transport,
  }

  sendingInterval := parseLogOptDuration(info, logOptSendingInterval, defaultSendingInterval)
  zeroSeconds, _ := time.ParseDuration("0s")
  if sendingInterval <= zeroSeconds {
    logrus.Error(fmt.Errorf("%s must be a positive duration, got %s. Using default %s.",
      logOptSendingInterval, sendingInterval.String(), defaultSendingInterval.String()))
    sendingInterval = defaultSendingInterval
  }
  queueSize := parseLogOptInt(info, logOptQueueSize, defaultQueueSize)
  if queueSize <= 0 {
    logrus.Error(fmt.Errorf("%s must be a positive value, got %d. Using default %d.",
      logOptQueueSize, queueSize, defaultQueueSize))
    queueSize = defaultQueueSize
  }
  batchSize := parseLogOptInt(info, logOptBatchSize, defaultBatchSize)
  if batchSize <= 0 {
    logrus.Error(fmt.Errorf("%s must be a positive value, got %d. Using default %d.",
      logOptBatchSize, batchSize, defaultBatchSize))
    batchSize = defaultBatchSize
  }

  newSumoLogger := &sumoLogger{
    httpSourceUrl: info.Config[logOptUrl],
    httpClient: httpClient,
    proxyUrl: proxyUrl,
    tlsConfig: tlsConfig,
    inputQueueFile: inputQueueFile,
    gzipCompression: gzipCompression,
    gzipCompressionLevel: gzipCompressionLevel,
    logQueue: make(chan *sumoLog, queueSize),
    sendingInterval: sendingInterval,
    batchSize: batchSize,
  }

  sumoDriver.mu.Lock()
  sumoDriver.loggers[file] = newSumoLogger
  sumoDriver.mu.Unlock()

  return newSumoLogger, nil
}

func (sumoDriver *sumoDriver) StopLogging(file string) error {
  sumoDriver.mu.Lock()
  sumoLogger, exists := sumoDriver.loggers[file]
  if exists {
    sumoLogger.inputQueueFile.Close()
    delete(sumoDriver.loggers, file)
  }
  sumoDriver.mu.Unlock()
  return nil
}
