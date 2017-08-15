package main

import (
  "context"
  "fmt"
  "io"
  "net/http"
  "sync"
  "syscall"
  "time"

  "github.com/docker/docker/daemon/logger"
  "github.com/pkg/errors"
  "github.com/tonistiigi/fifo"
)

const (
  logOptUrl = "sumo-url"

  defaultSendingIntervalMs = 2000 * time.Millisecond
  defaultQueueSizeItems = 500
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

  inputFile io.ReadWriteCloser
  logQueue chan *sumoLog
  logBatchQueue chan []*sumoLog
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
    return nil, fmt.Errorf("a logger for %q already exists", file)
  }
  sumoDriver.mu.Unlock()

  httpClient := &http.Client{}

  sendingInterval := defaultSendingIntervalMs
  queueSize := defaultQueueSizeItems
  batchSize := defaultBatchSizeBytes

  /* https://github.com/containerd/fifo */
  inputFile, err := fifo.OpenFifo(context.Background(), file, syscall.O_RDONLY, fileMode)
  if err != nil {
    return nil, errors.Wrapf(err, "error opening logger fifo: %q", file)
  }

  newSumoLogger := &sumoLogger{
    httpSourceUrl: info.Config[logOptUrl],
    httpClient: httpClient,
    inputFile: inputFile,
    logQueue: make(chan *sumoLog, 10 * queueSize),
    logBatchQueue: make(chan []*sumoLog, queueSize),
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
    sumoLogger.inputFile.Close()
    delete(sumoDriver.loggers, file)
  }
  sumoDriver.mu.Unlock()
  return nil
}
