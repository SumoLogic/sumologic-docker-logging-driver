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
  defaultFrequency  = 2 * time.Second
	defaultStreamSize = 4000
	defaultBatchSize = 1000
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
  client HttpClient
  fifoLogStream io.ReadWriteCloser
  logStream chan *sumoLog
  frequency time.Duration
  batchSize int
  closed bool
}

type sumoLog struct {
	Line   []byte
	Source string
	Time   string
  Partial bool
}

func NewSumoDriver() *sumoDriver {
  return &sumoDriver{
    loggers: make(map[string]*sumoLogger),
  }
}

func (sumoDriver *sumoDriver) StartLogging(file string, info logger.Info) error {
  newSumoLogger, err := sumoDriver.startLogging(file, info)
  if err != nil {
    return err
  }
  go consumeLogsFromFifo(newSumoLogger)
  go queueLogsForSending(newSumoLogger)
  return nil
}

func (sumoDriver *sumoDriver) startLogging(file string, info logger.Info) (*sumoLogger, error) {
  sumoDriver.mu.Lock()
  if _, exists := sumoDriver.loggers[file]; exists {
    sumoDriver.mu.Unlock()
    return nil, fmt.Errorf("a logger for %q already exists", file)
  }
  sumoDriver.mu.Unlock()

  fifoLogStream, err := fifo.OpenFifo(context.Background(), file, syscall.O_RDONLY, 0700)
	if err != nil {
		return nil, errors.Wrapf(err, "error opening logger fifo: %q", file)
	}

  // TODO: make options configurable through logOpts
  frequency := defaultFrequency
  streamSize := defaultStreamSize
  batchSize := defaultBatchSize

  newSumoLogger := &sumoLogger{
    httpSourceUrl: info.Config[logOptUrl],
    client: &http.Client{},
    fifoLogStream: fifoLogStream,
    logStream: make(chan *sumoLog, streamSize),
    frequency: frequency,
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
    sumoLogger.closed = true
		sumoLogger.fifoLogStream.Close()
		delete(sumoDriver.loggers, file)
	}
	sumoDriver.mu.Unlock()
  return nil
}

func consumeLogsFromFifo(sumoLogger *sumoLogger) {
  dec := protoio.NewUint32DelimitedReader(sumoLogger.fifoLogStream, binary.BigEndian, 1e6)
  defer dec.Close()
	var buf logdriver.LogEntry
	for {
    if sumoLogger.closed {
      return
    }
		if err := dec.ReadMsg(&buf); err != nil {
			if err == io.EOF {
				sumoLogger.fifoLogStream.Close()
        close(sumoLogger.logStream)
				return
			}
      logrus.Error(err)
			dec = protoio.NewUint32DelimitedReader(sumoLogger.fifoLogStream, binary.BigEndian, 1e6)
		}

    // TODO: handle multi-line detection via Partial
		log := &sumoLog{
      Line: buf.Line,
      Source: buf.Source,
      Time: time.Unix(0, buf.TimeNano).String(),
      Partial: buf.Partial,
    }
    sumoLogger.logStream <- log
		buf.Reset()
	}
}

func queueLogsForSending(sumoLogger *sumoLogger) {
  timer := time.NewTicker(sumoLogger.frequency)
  var logs []*sumoLog
  for {
    if sumoLogger.closed {
      return
    }
    select {
    case <-timer.C:
      if sumoLogger.closed {
        return
      }
      logs = sumoLogger.sendLogs(logs)
    case log, open := <-sumoLogger.logStream:
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
  logsCount := len(logs)
  if logsCount == 0 {
    return logs
  }
  var logsBatch bytes.Buffer
  for _, log := range logs {
    if _, err := logsBatch.Write(log.Line); err != nil {
      logrus.Error(err)
    }
  }

  // TODO: error handling, retries and exponential backoff
  request, err := http.NewRequest("POST", sumoLogger.httpSourceUrl, bytes.NewBuffer(logsBatch.Bytes()))
  if err != nil {
    logrus.Error(err)
  }
  response, err := sumoLogger.client.Do(request)
  if err != nil {
    logrus.Error(err)
  }

  defer response.Body.Close()
  if response.StatusCode != http.StatusOK {
    body, err := ioutil.ReadAll(response.Body)
    if err != nil {
      logrus.Error(err)
    }
    logrus.Error(fmt.Errorf("%s: Failed to send event: %s - %s", pluginName, response.Status, body))
    return logs
  }
  return logs[:0]
}
