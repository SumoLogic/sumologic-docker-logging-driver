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
  defaultSendingFrequency  = 2 * time.Second
  defaultStreamSize = 4000
  defaultBatchSize = 1000

  fileMode = 0700
  fileReaderMaxSize = 1e6

  logOptGzipCompression = "sumo-compress"
  logOptGzipCompressionLevel = "sumo-compress-level"
  logOptProxyUrl = "sumo-proxy-url"
  logOptInsecureSkipVerify = "sumo-insecure-skip-verify"
  logOptRootCaPath = "sumo-root-ca-path"
  logOptServerName = "sumo-server-name"
  logOptSendingFrequency = "sumo-sending-frequency"
  logOptStreamSize = "sumo-stream-size"
  logOptBatchSize = "sumo-batch-size"

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

  gzipCompression := false
	if gzipCompressionStr, exists := info.Config[logOptGzipCompression]; exists {
		gzipCompression, err = strconv.ParseBool(gzipCompressionStr)
		if err != nil {
			return nil, err
		}
	}

	gzipCompressionLevel := gzip.DefaultCompression
	if gzipCompressionLevelStr, exists := info.Config[logOptGzipCompressionLevel]; exists {
		var err error
		gzipCompressionLevel64, err := strconv.ParseInt(gzipCompressionLevelStr, stringToIntBase, stringToIntBitSize)
		if err != nil {
			return nil, err
		}
		gzipCompressionLevel = int(gzipCompressionLevel64)
		if gzipCompressionLevel < gzip.DefaultCompression || gzipCompressionLevel > gzip.BestCompression {
			err := fmt.Errorf("Not supported level '%s' for %s (supported values between %d and %d).",
				gzipCompressionLevelStr, logOptGzipCompressionLevel, gzip.DefaultCompression, gzip.BestCompression)
			return nil, err
		}
	}

  var proxyUrl *url.URL
  proxyUrl = nil
  if proxyUrlStr, exists := info.Config[logOptProxyUrl]; exists {
    proxyUrl, err = url.Parse(proxyUrlStr)
    if err != nil {
      return nil, err
    }
  }

  tlsConfig := &tls.Config{}

  if insecureSkipVerifyStr, exists := info.Config[logOptInsecureSkipVerify]; exists {
		insecureSkipVerify, err := strconv.ParseBool(insecureSkipVerifyStr)
		if err != nil {
			return nil, err
		}
		tlsConfig.InsecureSkipVerify = insecureSkipVerify
	}
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

  transport := &http.Transport{
    Proxy: http.ProxyURL(proxyUrl),
    TLSClientConfig: tlsConfig,
  }

  httpClient := &http.Client{
    Transport: transport,
  }

  sendingFrequency := defaultSendingFrequency
  if sendingFrequencyStr, exists := info.Config[logOptSendingFrequency]; exists {
    sendingFrequency, err = time.ParseDuration(sendingFrequencyStr)
    if err != nil {
      logrus.Error(fmt.Sprintf("Failed to parse value of %s as duration. Using default %v. %v", logOptSendingFrequency, defaultSendingFrequency, err))
      sendingFrequency = defaultSendingFrequency
    }
  }
  streamSize := defaultStreamSize
  if streamSizeStr, exists := info.Config[logOptStreamSize]; exists {
    streamSize64, err := strconv.ParseInt(streamSizeStr, stringToIntBase, stringToIntBitSize)
    streamSize = int(streamSize64)
    if err != nil {
      logrus.Error(fmt.Sprintf("Failed to parse value of %s as integer. Using default %d. %v", logOptStreamSize, defaultStreamSize, err))
      streamSize = defaultStreamSize
    }
  }
  batchSize := defaultBatchSize
  if batchSizeStr, exists := info.Config[logOptStreamSize]; exists {
    batchSize64, err := strconv.ParseInt(batchSizeStr, stringToIntBase, stringToIntBitSize)
    batchSize = int(batchSize64)
    if err != nil {
      logrus.Error(fmt.Sprintf("Failed to parse value of %s as integer. Using default %d. %v", logOptBatchSize, defaultBatchSize, err))
      batchSize = defaultBatchSize
    }
  }

  newSumoLogger := &sumoLogger{
    httpSourceUrl: info.Config[logOptUrl],
    httpClient: httpClient,
    inputQueueFile: inputQueueFile,
    gzipCompression: gzipCompression,
    gzipCompressionLevel: gzipCompressionLevel,
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
  var writer io.Writer
  var gzipWriter *gzip.Writer
  var err error

  if sumoLogger.gzipCompression {
    gzipWriter, err = gzip.NewWriterLevel(&logsBatch, sumoLogger.gzipCompressionLevel)
    if err != nil {
      return err
    }
    writer = gzipWriter
  } else {
    writer = &logsBatch
  }
  for _, log := range logs {
    if _, err := writer.Write(log.line); err != nil {
      return err
    }
  }
  if sumoLogger.gzipCompression {
		err = gzipWriter.Close()
		if err != nil {
			return err
		}
	}

  // TODO: error handling, retries and exponential backoff
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
