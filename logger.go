package main

import (
  "bytes"
  "compress/gzip"
  "encoding/binary"
  "fmt"
  "io"
  "io/ioutil"
  "net/http"
  "net/url"
  "strconv"
  "time"

  "github.com/docker/docker/api/types/plugins/logdriver"
  "github.com/docker/docker/daemon/logger"
  protoio "github.com/gogo/protobuf/io"
  "github.com/sirupsen/logrus"
)

const (
  fileReaderMaxSize = 1e6
  stringToIntBase = 10
  stringToIntBitSize = 32
)

func (sumoLogger *sumoLogger) consumeLogsFromFile() {
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

func (sumoLogger *sumoLogger) bufferLogsForSending() {
  timer := time.NewTicker(sumoLogger.sendingInterval)
  var logsBuffer []*sumoLog
  for {
    select {
    case <-timer.C:
      logsBuffer = sumoLogger.sendLogs(logsBuffer)
    case log, open := <-sumoLogger.logQueue:
      if !open {
        sumoLogger.sendLogs(logsBuffer)
        return
      }
      logsBuffer = append(logsBuffer, log)
      if len(logsBuffer) % sumoLogger.batchSize == 0 {
        logsBuffer = sumoLogger.sendLogs(logsBuffer)
      }
    }
  }
}

func (sumoLogger *sumoLogger) sendLogs(logs []*sumoLog) []*sumoLog {
  var failedLogsToRetry []*sumoLog
  logsCount := len(logs)
  for i := 0; i < logsCount; i += sumoLogger.batchSize {
    upperBound := i + sumoLogger.batchSize
    if upperBound > logsCount {
      upperBound = logsCount
    }
    // TODO: exponential backoff here? using golang exponential backoff pkg
    if err := sumoLogger.makePostRequest(logs[i:upperBound]); err != nil {
      logrus.Error(err)
      failedLogsToRetry = logs[i:logsCount]
      return failedLogsToRetry
    }
  }
  failedLogsToRetry = logs[:0]
  return failedLogsToRetry
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

func parseLogOptIntPositive(info logger.Info, logOptKey string, defaultValue int) int {
  if input, exists := info.Config[logOptKey]; exists {
    inputValue64, err := strconv.ParseInt(input, stringToIntBase, stringToIntBitSize)
    if err != nil {
      logrus.Error(fmt.Errorf("Failed to parse value of %s as integer. Using default %d. %v",
        logOptKey, defaultValue, err))
      return defaultValue
    }
    inputValue := int(inputValue64)
    if inputValue <= 0 {
      logrus.Error(fmt.Errorf("%s must be a positive value, got %d. Using default %d.",
        logOptKey, inputValue, defaultValue))
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
      logrus.Error(fmt.Errorf("Failed to parse value of %s as duration. Using default %v. %v",
        logOptKey, defaultValue, err))
      return defaultValue
    }
    zeroSeconds, _ := time.ParseDuration("0s")
    if inputValue <= zeroSeconds {
      logrus.Error(fmt.Errorf("%s must be a positive duration, got %s. Using default %s.",
        logOptKey, inputValue.String(), defaultValue.String()))
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

func parseLogOptGzipCompressionLevel(info logger.Info, logOptKey string, defaultValue int) int {
  if input, exists := info.Config[logOptKey]; exists {
    inputValue64, err := strconv.ParseInt(input, stringToIntBase, stringToIntBitSize)
    if err != nil {
      logrus.Error(fmt.Errorf("Failed to parse value of %s as integer. Using default %d. %v",
        logOptKey, defaultValue, err))
      return defaultValue
    }
    inputValue := int(inputValue64)
    if inputValue < defaultValue || inputValue > gzip.BestCompression {
      logrus.Error(fmt.Errorf("Not supported level '%d' for %s (supported values between %d and %d). Using default compression.",
        inputValue, logOptKey, defaultValue, gzip.BestCompression))
      return defaultValue
    }
    return inputValue
  }
  return defaultValue
}
