package main

import (
  "bytes"
  "compress/gzip"
  "context"
  "crypto/tls"
  "crypto/x509"
  "encoding/binary"
  "fmt"
  "io"
  "io/ioutil"
  "net/http"
  "net/url"
  "strconv"
  "sync"
  "syscall"
  "time"

  "github.com/docker/docker/api/types/plugins/logdriver"
  "github.com/docker/docker/daemon/logger"
  protoio "github.com/gogo/protobuf/io"
  "github.com/pkg/errors"
  "github.com/sirupsen/logrus"
  "github.com/tonistiigi/fifo"
)

const (
  defaultInsecureSkipVerify = false
  defaultGzipCompression = false
  defaultGzipCompressionLevel = gzip.DefaultCompression
  defaultSendingFrequency  = 2 * time.Second
  defaultQueueSize = 4000
  defaultBatchSize = 1000

  logOptGzipCompression = "sumo-compress"
  logOptGzipCompressionLevel = "sumo-compress-level"
  logOptProxyUrl = "sumo-proxy-url"
  logOptInsecureSkipVerify = "sumo-insecure-skip-verify"
  logOptRootCaPath = "sumo-root-ca-path"
  logOptServerName = "sumo-server-name"
  logOptSendingFrequency = "sumo-sending-frequency"
  logOptQueueSize = "sumo-queue-size"
  logOptBatchSize = "sumo-batch-size"

  fileMode = 0700
  fileReaderMaxSize = 1e6
  stringToIntBase = 10
  stringToIntBitSize = 32
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
  sendingFrequency time.Duration
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
  go queueLogsForSending(newSumoLogger)
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

  sendingFrequency := parseLogOptDuration(info, logOptSendingFrequency, defaultSendingFrequency)
  if sendingFrequency <= 0 {
    logrus.Error(fmt.Errorf("%s must be a positive duration. Using default duration.", logOptSendingFrequency))
    sendingFrequency = defaultSendingFrequency
  }
  queueSize := parseLogOptInt(info, logOptQueueSize, defaultQueueSize)
  if queueSize <= 0 {
    logrus.Error(fmt.Errorf("%s must be a positive value, got %d. Using default queue size.", logOptQueueSize, queueSize))
    queueSize = defaultQueueSize
  }
  batchSize := parseLogOptInt(info, logOptBatchSize, defaultBatchSize)
  if batchSize <= 0 {
    logrus.Error(fmt.Errorf("%s must be a positive value, got %d. Using default batch size.", logOptBatchSize, batchSize))
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
    sendingFrequency: sendingFrequency,
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

func consumeLogsFromFile(sumoLogger *sumoLogger) {
  dec := protoio.NewUint32DelimitedReader(sumoLogger.inputQueueFile, binary.BigEndian, fileReaderMaxSize)
  defer dec.Close()
  var buf logdriver.LogEntry
  for {
    if err := dec.ReadMsg(&buf); err != nil {
      if err == io.EOF {
        sumoLogger.inputQueueFile.Close()
        close(sumoLogger.logQueue)
        return
      }
      logrus.Error(err)
      dec = protoio.NewUint32DelimitedReader(sumoLogger.inputQueueFile, binary.BigEndian, fileReaderMaxSize)
    }

    // TODO: handle multi-line detection via Partial
    log := &sumoLog{
      line: buf.Line,
      source: buf.Source,
      time: time.Unix(0, buf.TimeNano).String(),
      isPartial: buf.Partial,
    }
    sumoLogger.logQueue <- log
    buf.Reset()
  }
}

func queueLogsForSending(sumoLogger *sumoLogger) {
  timer := time.NewTicker(sumoLogger.sendingFrequency)
  var logs []*sumoLog
  for {
    select {
    case <-timer.C:
      logs = sumoLogger.sendLogs(logs)
    case log, open := <-sumoLogger.logQueue:
      if !open {
        sumoLogger.sendLogs(logs)
        return
      }
      logs = append(logs, log)
      if len(logs) % sumoLogger.batchSize == 0 {
        logs = sumoLogger.sendLogs(logs)
      }
    }
  }
}

func (sumoLogger *sumoLogger) sendLogs(logs []*sumoLog) []*sumoLog {
  var failedLogs []*sumoLog
  logsCount := len(logs)
  for i := 0; i < logsCount; i += sumoLogger.batchSize {
    upperBound := i + sumoLogger.batchSize
    if upperBound > logsCount {
      upperBound = logsCount
    }
    if err := sumoLogger.makePostRequest(logs[i:upperBound]); err != nil {
      logrus.Error(err)
      failedLogs = logs[i:logsCount]
      return failedLogs
    }
  }
  failedLogs = logs[:0]
  return failedLogs
}

func (sumoLogger *sumoLogger) makePostRequest(logs []*sumoLog) error {
  logsCount := len(logs)
  if logsCount == 0 {
    return nil
  }

  var logsBatch bytes.Buffer
  if sumoLogger.gzipCompression {
    if err := sumoLogger.writeMessageGzipCompression(&logsBatch, logs); err != nil {
      return err
    }
  } else {
    if err := sumoLogger.writeMessage(&logsBatch, logs); err != nil{
      return err
    }
  }

  // TODO: error handling, retries and exponential backoff
  request, err := http.NewRequest("POST", sumoLogger.httpSourceUrl, bytes.NewBuffer(logsBatch.Bytes()))
  if err != nil {
    return err
  }
  request.Header.Add("Content-Type", "text/plain")
  if sumoLogger.gzipCompression {
    request.Header.Add("Content-Encoding", "gzip")
  }

  response, err := sumoLogger.httpClient.Do(request)
  if err != nil {
    return err
  }

  defer response.Body.Close()
  if response.StatusCode != http.StatusOK {
    body, err := ioutil.ReadAll(response.Body)
    if err != nil {
      return err
    }
    return fmt.Errorf("%s: Failed to send event: %s - %s", pluginName, response.Status, body)
  }
  return nil
}

func (sumoLogger *sumoLogger) writeMessage(writer io.Writer, logs []*sumoLog) error {
  for _, log := range logs {
    if _, err := writer.Write(append(log.line, []byte("\n")...)); err != nil {
      return err
    }
  }
  return nil
}

func (sumoLogger *sumoLogger) writeMessageGzipCompression(writer io.Writer, logs []*sumoLog) error {
  gzipWriter, err := gzip.NewWriterLevel(writer, sumoLogger.gzipCompressionLevel)
  if err != nil {
    return err
  }
  if err := sumoLogger.writeMessage(gzipWriter, logs); err != nil {
    return err
  }
  if err := gzipWriter.Close(); err != nil {
    return err
  }
  return nil
}

func parseLogOptInt(info logger.Info, logOptKey string, defaultValue int) int {
  if input, exists := info.Config[logOptKey]; exists {
    inputValue, err := strconv.ParseInt(input, stringToIntBase, stringToIntBitSize)
    if err != nil {
      logrus.Error(fmt.Errorf("Failed to parse value of %s as integer. Using default %d. %v",
        logOptKey, defaultValue, err))
      return defaultValue
    }
    return int(inputValue)
  }
  return defaultValue
}

func parseLogOptDuration(info logger.Info, logOptKey string, defaultValue time.Duration) time.Duration {
  if input, exists := info.Config[logOptKey]; exists {
    inputValue, err := time.ParseDuration(input)
    if err != nil {
      logrus.Error(fmt.Errorf("Failed to parse value of %s as duration. Using default %v. %v",
        logOptKey, defaultValue, err))
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
      logrus.Error(fmt.Errorf("Failed to parse value of %s as boolean. Using default %t. %v",
        logOptKey, defaultValue, err))
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
      logrus.Error(fmt.Errorf("Failed to parse value of %s as url. Initializing without proxy. %v",
        logOptKey, defaultValue, err))
      return defaultValue
    }
    return inputValue
  }
  return defaultValue
}
