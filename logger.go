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
  timer := time.NewTicker(sumoLogger.sendingInterval)
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
