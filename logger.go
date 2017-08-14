package main

import (
  "bytes"
  "compress/gzip"
  "encoding/binary"
  "fmt"
  "io"
  "io/ioutil"
  "net/http"
  "time"

  "github.com/docker/docker/api/types/plugins/logdriver"
  protoio "github.com/gogo/protobuf/io"
  "github.com/sirupsen/logrus"
)

const (
  maxRetryInterval = 30 * time.Second
  initialRetryInterval = 500 * time.Millisecond
  retryMultiplier = 2

  fileReaderMaxSize = 1e6
  stringToIntBase = 10
  stringToIntBitSize = 32
)

type sumoLog struct {
  line []byte
  source string
  time string
  isPartial bool
}

type sumoLogBatch struct {
  logs []*sumoLog
  size int
}

func NewSumoLogBatch() *sumoLogBatch {
  return &sumoLogBatch{
    logs: nil,
    size: 0,
  }
}

func (sumoLogBatch *sumoLogBatch) Reset() {
  sumoLogBatch.logs = nil
  sumoLogBatch.size = 0
}

func (sumoLogger *sumoLogger) consumeLogsFromFile() {
  /* https://github.com/gogo/protobuf/blob/master/io/uint32.go */
  dec := protoio.NewUint32DelimitedReader(sumoLogger.inputFile, binary.BigEndian, fileReaderMaxSize)
  defer dec.Close()
  var log logdriver.LogEntry
  for {
    if err := dec.ReadMsg(&log); err != nil {
      if err == io.EOF {
        sumoLogger.inputFile.Close()
        close(sumoLogger.logQueue)
        return
      }
      logrus.Error(err)
      dec = protoio.NewUint32DelimitedReader(sumoLogger.inputFile, binary.BigEndian, fileReaderMaxSize)
    }
    sumoLog := &sumoLog{
      line: log.Line,
      source: log.Source,
      time: time.Unix(0, log.TimeNano).String(),
      isPartial: log.Partial,
    }
    sumoLogger.logQueue <- sumoLog
    log.Reset()
  }
}

func (sumoLogger *sumoLogger) batchLogs() {
  ticker := time.NewTicker(sumoLogger.sendingInterval)
  logBatch := NewSumoLogBatch()
  for {
    select {
    case log, open := <-sumoLogger.logQueue:
      if !open {
        sumoLogger.logBatchQueue <- logBatch.logs
        close(sumoLogger.logBatchQueue)
        return
      }
      logBatch.logs = append(logBatch.logs, log)
      logBatch.size += len(log.line)
      if logBatch.size >= sumoLogger.batchSize {
        sumoLogger.pushBatchToQueue(logBatch)
      }
    case <-ticker.C:
      if len(logBatch.logs) > 0 {
        sumoLogger.pushBatchToQueue(logBatch)
      }
    }
  }
}

func (sumoLogger *sumoLogger) pushBatchToQueue(logBatch *sumoLogBatch) {
  select {
  case sumoLogger.logBatchQueue <- logBatch.logs:
    logBatch.Reset()
  default:
    <-sumoLogger.logBatchQueue
    logrus.Error(fmt.Errorf("log batch queue full, dropping oldest batch"))
    sumoLogger.logBatchQueue <- logBatch.logs
  }
}

func (sumoLogger *sumoLogger) handleBatchedLogs() {
  retryInterval := initialRetryInterval
  for {
    logBatch, open := <-sumoLogger.logBatchQueue
    if !open {
      return
    }
    for {
      err := sumoLogger.sendLogs(logBatch)
      if err == nil {
        retryInterval = initialRetryInterval
        break
      }
      time.Sleep(retryInterval)
      if retryInterval < maxRetryInterval {
        retryInterval *= retryMultiplier
        if retryInterval > maxRetryInterval {
          retryInterval = maxRetryInterval
        }
      }
      // if elapsedTime > maxElapsedTime {
      //   elapsedTime = initialElapsedTime
      //   logrus.Error(fmt.Errorf("could not send log batch after %s. Batch dropped.", maxElapsedTime.String()))
      //   break
      // }
    }
  }
}

func (sumoLogger *sumoLogger) sendLogs(logs []*sumoLog) error {
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
