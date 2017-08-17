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
  "strconv"
  "sync"
  "syscall"
  "time"

  "github.com/docker/docker/daemon/logger"
  "github.com/pkg/errors"
  "github.com/tonistiigi/fifo"
  "github.com/sirupsen/logrus"
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
  /* The maximum number of log batches of size sumo-batch-size we can store in memory
    in the event of network failure before we begin dropping batches. */
  logOptQueueSize = "sumo-queue-size"
  /* The number of bytes of logs the driver should wait for before sending them in a batch.
    If the number of bytes never reaches the batch size, the driver will send the logs in smaller
    batches at predefined intervals; see sending interval. */
  logOptBatchSize = "sumo-batch-size"

  defaultGzipCompression = true
  defaultGzipCompressionLevel = gzip.DefaultCompression
  defaultInsecureSkipVerify = false

  defaultSendingIntervalMs = 2000 * time.Millisecond
  defaultQueueSizeItems = 100
  defaultBatchSizeBytes = 1000000

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

  inputFile io.ReadWriteCloser
  logQueue chan *sumoLog
  logBatchQueue chan *sumoLogBatch
  sendingInterval time.Duration
  batchSize int
}

func newSumoDriver() *sumoDriver {
  return &sumoDriver{
    loggers: make(map[string]*sumoLogger),
  }
}

func (sumoDriver *sumoDriver) StartLogging(file string, info logger.Info) error {
  newSumoLogger, err := sumoDriver.NewSumoLogger(file, info)
  if err != nil {
    return err
  }
  go newSumoLogger.consumeLogsFromFile()
  go newSumoLogger.batchLogs()
  go newSumoLogger.handleBatchedLogs()
  return nil
}

func (sumoDriver *sumoDriver) NewSumoLogger(file string, info logger.Info) (*sumoLogger, error) {
  sumoDriver.mu.Lock()
  if _, exists := sumoDriver.loggers[file]; exists {
    sumoDriver.mu.Unlock()
    return nil, fmt.Errorf("%s: a logger for %q already exists", pluginName, file)
  }
  sumoDriver.mu.Unlock()

  gzipCompression := parseLogOptBoolean(info, logOptGzipCompression, defaultGzipCompression)
  gzipCompressionLevel := parseLogOptGzipCompressionLevel(info, logOptGzipCompressionLevel, defaultGzipCompressionLevel)

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
    Timeout: 30 * time.Second,
  }

  sendingInterval := parseLogOptDuration(info, logOptSendingInterval, defaultSendingIntervalMs)
  queueSize := parseLogOptIntPositive(info, logOptQueueSize, defaultQueueSizeItems)
  batchSize := parseLogOptIntPositive(info, logOptBatchSize, defaultBatchSizeBytes)

  /* https://github.com/containerd/fifo */
  inputFile, err := fifo.OpenFifo(context.Background(), file, syscall.O_RDONLY, fileMode)
  if err != nil {
    return nil, errors.Wrapf(err, "error opening logger fifo: %q", file)
  }

  newSumoLogger := &sumoLogger{
    httpSourceUrl: info.Config[logOptUrl],
    httpClient: httpClient,
    proxyUrl: proxyUrl,
    tlsConfig: tlsConfig,
    inputFile: inputFile,
    gzipCompression: gzipCompression,
    gzipCompressionLevel: gzipCompressionLevel,
    logQueue: make(chan *sumoLog, 10 * queueSize),
    logBatchQueue: make(chan *sumoLogBatch, queueSize),
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
    logrus.Info(fmt.Sprintf("%s: Stopping logging driver for closed container.", pluginName))
    sumoLogger.inputFile.Close()
    delete(sumoDriver.loggers, file)
  }
  sumoDriver.mu.Unlock()
  return nil
}

func parseLogOptIntPositive(info logger.Info, logOptKey string, defaultValue int) int {
  if input, exists := info.Config[logOptKey]; exists {
    inputValue64, err := strconv.ParseInt(input, stringToIntBase, stringToIntBitSize)
    if err != nil {
      logrus.Error(fmt.Errorf("%s: Failed to parse value of %s as integer. Using default %d. %v",
        pluginName, logOptKey, defaultValue, err))
      return defaultValue
    }
    inputValue := int(inputValue64)
    if inputValue <= 0 {
      logrus.Error(fmt.Errorf("%s: %s must be a positive value, got %d. Using default %d.",
        pluginName, logOptKey, inputValue, defaultValue))
      return defaultValue
    }
    return inputValue
  }
  return defaultValue
}

func parseLogOptDuration(info logger.Info, logOptKey string, defaultValue time.Duration) time.Duration {
  if input, exists := info.Config[logOptKey]; exists {
    inputValue, err := time.ParseDuration(input)
    if err != nil {
      logrus.Error(fmt.Errorf("%s: Failed to parse value of %s as duration. Using default %v. %v",
        pluginName, logOptKey, defaultValue, err))
      return defaultValue
    }
    zeroSeconds, _ := time.ParseDuration("0s")
    if inputValue <= zeroSeconds {
      logrus.Error(fmt.Errorf("%s: %s must be a positive duration, got %s. Using default %s.",
        pluginName, logOptKey, inputValue.String(), defaultValue.String()))
      return defaultValue
    }
    return inputValue
  }
  return defaultValue
}

func parseLogOptBoolean(info logger.Info, logOptKey string, defaultValue bool) bool {
  if input, exists := info.Config[logOptKey]; exists {
    inputValue, err := strconv.ParseBool(input)
    if err != nil {
      logrus.Error(fmt.Errorf("%s: Failed to parse value of %s as boolean. Using default %t. %v",
        pluginName, logOptKey, defaultValue, err))
      return defaultValue
    }
    return inputValue
  }
  return defaultValue
}

func parseLogOptProxyUrl(info logger.Info, logOptKey string, defaultValue *url.URL) *url.URL {
  if input, exists := info.Config[logOptKey]; exists {
    inputValue, err := url.Parse(input)
    if err != nil {
      logrus.Error(fmt.Errorf("%s: Failed to parse value of %s as url. Initializing without proxy. %v",
        pluginName, logOptKey, defaultValue, err))
      return defaultValue
    }
    return inputValue
  }
  return defaultValue
}

func parseLogOptGzipCompressionLevel(info logger.Info, logOptKey string, defaultValue int) int {
  if input, exists := info.Config[logOptKey]; exists {
    inputValue64, err := strconv.ParseInt(input, stringToIntBase, stringToIntBitSize)
    if err != nil {
      logrus.Error(fmt.Errorf("%s: Failed to parse value of %s as integer. Using default %d. %v",
        pluginName, logOptKey, defaultValue, err))
      return defaultValue
    }
    inputValue := int(inputValue64)
    if inputValue < defaultValue || inputValue > gzip.BestCompression {
      logrus.Error(fmt.Errorf(
        "%s: Not supported level '%d' for %s (supported values between %d and %d). Using default compression.",
        pluginName, inputValue, logOptKey, defaultValue, gzip.BestCompression))
      return defaultValue
    }
    return inputValue
  }
  return defaultValue
}
