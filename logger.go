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
  maxElapsedTime = 3 * time.Minute
  initialRetryInterval = 500 * time.Millisecond
  initialElapsedTime = 0 * time.Minute
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
  var logBatch []*sumoLog
  batchSize := 0
  for {
    select {
    case log, open := <-sumoLogger.logQueue:
      if !open {
        sumoLogger.logBatchQueue <- logBatch
        close(sumoLogger.logBatchQueue)
        return
      }
      logBatch = append(logBatch, log)
      batchSize += len(log.line)
      if batchSize >= sumoLogger.batchSize {
        sumoLogger.logBatchQueue <- logBatch
        logBatch = nil
        batchSize = 0
      }
    case <-ticker.C:
      if len(logBatch) > 0 {
        sumoLogger.logBatchQueue <- logBatch
        logBatch = nil
        batchSize = 0
      }
    }
  }
}

func (sumoLogger *sumoLogger) handleBatchedLogs() {
  retryInterval := initialRetryInterval
  elapsedTime := initialElapsedTime
  for {
    logBatch, open := <-sumoLogger.logBatchQueue
    if !open {
      return
    }
    for {
      err := sumoLogger.sendLogs(logBatch)
      if err == nil {
        retryInterval = initialRetryInterval
        elapsedTime = initialElapsedTime
        break
      }
      time.Sleep(retryInterval)
      elapsedTime += retryInterval
      if retryInterval < maxRetryInterval {
        retryInterval *= retryMultiplier
      }
      if elapsedTime > maxElapsedTime {
        elapsedTime = initialElapsedTime
        logrus.Error(fmt.Errorf("could not send log batch after %s. Batch dropped.", maxElapsedTime.String()))
        break
      }
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
