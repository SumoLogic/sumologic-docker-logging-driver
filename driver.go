package main

import (
  "bytes"
  "context"
  "encoding/binary"
  "fmt"
  "io"
  "io/ioutil"
  "net/http"
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
  defaultSendingFrequency  = 2 * time.Second
  defaultStreamSize = 4000
  defaultBatchSize = 1000

  fileMode = 0700
  fileReaderMaxSize = 1e6
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
  go consumeLogsFromFifo(newSumoLogger)
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

  // TODO: make options configurable through logOpts
  sendingFrequency := defaultSendingFrequency
  streamSize := defaultStreamSize
  batchSize := defaultBatchSize

  newSumoLogger := &sumoLogger{
    httpSourceUrl: info.Config[logOptUrl],
    httpClient: &http.Client{},
    inputQueueFile: inputQueueFile,
    logQueue: make(chan *sumoLog, streamSize),
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

func consumeLogsFromFifo(sumoLogger *sumoLogger) {
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
      if err := sumoLogger.sendLogs(logs); err != nil {
        logrus.Error(err)
      } else {
        logs = logs[:0]
      }
    case log, open := <-sumoLogger.logQueue:
      if !open {
        if err := sumoLogger.sendLogs(logs); err != nil {
          logrus.Error(err)
        }
        return
      }
      logs = append(logs, log)
      if len(logs) % sumoLogger.batchSize == 0 {
        if err := sumoLogger.sendLogs(logs); err != nil {
          logrus.Error(err)
        } else {
          logs = logs[:0]
        }
      }
    }
  }
}

func (sumoLogger *sumoLogger) sendLogs(logs []*sumoLog) error {
  logsCount := len(logs)
  if logsCount == 0 {
    return nil
  }
  var logsBatch bytes.Buffer
  for _, log := range logs {
    if _, err := logsBatch.Write(log.line); err != nil {
      return err
    }
  }

  // TODO: error handling, retries and exponential backoff
  request, err := http.NewRequest("POST", sumoLogger.httpSourceUrl, bytes.NewBuffer(logsBatch.Bytes()))
  if err != nil {
    return err
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
