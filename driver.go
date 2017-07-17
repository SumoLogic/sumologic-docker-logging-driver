package main

import (
  "sync"

  "github.com/docker/docker/daemon/logger"
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
}

func NewSumoDriver() *sumoDriver {
  return &sumoDriver{
    loggers: make(map[string]*sumoLogger),
  }
}

func (sumoDriver *sumoDriver) StartLogging(file string, info logger.Info) error {
  return nil
}

func (sumoDriver *sumoDriver) StopLogging(file string) error {
  return nil
}
