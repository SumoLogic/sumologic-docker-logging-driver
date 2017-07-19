package main

import (
  "context"
  "fmt"
  "io"
  "net/http"
  "sync"
  "syscall"

  "github.com/docker/docker/daemon/logger"
  "github.com/pkg/errors"
  "github.com/tonistiigi/fifo"
)

type SumoDriver interface {
  StartLogging(string, logger.Info) error
  StopLogging(string) error
}

type sumoDriver struct {
  loggers map[string]*sumoLogger
  mu sync.Mutex
}

type sumoLogger struct {
  httpSourceUrl string
  client *http.Client
  fifoLogStream io.ReadWriteCloser
}

func NewSumoDriver() *sumoDriver {
  return &sumoDriver{
    loggers: make(map[string]*sumoLogger),
  }
}

func (sumoDriver *sumoDriver) StartLogging(file string, info logger.Info) error {
  sumoDriver.mu.Lock()
  if _, exists := sumoDriver.loggers[file]; exists {
    sumoDriver.mu.Unlock()
    return fmt.Errorf("a logger for %q already exists", file)
  }
  sumoDriver.mu.Unlock()

  fifoLogStream, err := fifo.OpenFifo(context.Background(), file, syscall.O_RDONLY, 0700)
	if err != nil {
		return errors.Wrapf(err, "error opening logger fifo: %q", file)
	}

  newSumoLogger := &sumoLogger{
    httpSourceUrl: info.Config[logOptUrl],
    client: &http.Client{},
    fifoLogStream: fifoLogStream,
  }

  sumoDriver.mu.Lock()
  sumoDriver.loggers[file] = newSumoLogger
  sumoDriver.mu.Unlock()

  go consumeLogs(newSumoLogger)

  return nil
}

func (sumoDriver *sumoDriver) StopLogging(file string) error {
	sumoDriver.mu.Lock()
	logger, exists := sumoDriver.loggers[file]
	if exists {
		logger.fifoLogStream.Close()
		delete(sumoDriver.loggers, file)
	}
	sumoDriver.mu.Unlock()
  return nil
}

func consumeLogs(sumoLogger *sumoLogger) {
  return
}
