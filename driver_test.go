package main

import (
  "compress/gzip"
  "context"
  "crypto/tls"
  "io/ioutil"
  "net/url"
  "os"
  "strconv"
  "testing"
  "time"

  "github.com/docker/docker/daemon/logger"
  "github.com/sirupsen/logrus"
  "github.com/stretchr/testify/assert"
  "github.com/tonistiigi/fifo"
  "golang.org/x/sys/unix"
)

const (
  filePath  = "/tmp/test"
  filePath1 = "/tmp/test1"
  filePath2 = "/tmp/test2"

  testHttpSourceUrl = "https://example.org"
  testProxyUrlStr = "https://example.org"

  testSource = "sumo-test"
  testTime = 1234567890
  testIsPartial = false
)

var (
  testLine = []byte("a test log message")
)

func TestDriversDefaultConfig (t *testing.T) {
  logrus.SetOutput(ioutil.Discard)
  testLoggersCount := 100

  for i := 0; i < testLoggersCount; i++ {
    testFifo, err := fifo.OpenFifo(context.Background(), filePath + strconv.Itoa(i + 1), unix.O_RDWR|unix.O_CREAT|unix.O_NONBLOCK, fileMode)
    assert.Nil(t, err)
    defer testFifo.Close()
    defer os.Remove(filePath + strconv.Itoa(i + 1))
  }

  testContainerID := "12345678901234567890"
  testContainerName := "test_container_name"

  info := logger.Info{
    Config: map[string]string{
      logOptUrl: testHttpSourceUrl,
    },
    ContainerID: testContainerID,
    ContainerName: testContainerName,
  }

  t.Run("NewSumoLogger", func(t *testing.T) {
    testSumoDriver := newSumoDriver()
    assert.Equal(t, 0, len(testSumoDriver.loggers), "there should be no loggers when the driver is initialized")

    testSumoLogger1, err := testSumoDriver.NewSumoLogger(filePath1, info)
    assert.Nil(t, err)
    assert.Equal(t, 1, len(testSumoDriver.loggers), "there should be one logger after calling StartLogging on driver")
    assert.Equal(t, testHttpSourceUrl, testSumoLogger1.httpSourceUrl, "http source url should be configured correctly")
    assert.Equal(t, defaultGzipCompression, testSumoLogger1.gzipCompression, "compression not specified, should be false")
    assert.Equal(t, defaultGzipCompressionLevel, testSumoLogger1.gzipCompressionLevel, "compression level not specified, should be default value")
    assert.Equal(t, defaultSendingIntervalMs, testSumoLogger1.sendingInterval, "sending interval not specified, should be default value")
    assert.Equal(t, defaultQueueSizeItems, cap(testSumoLogger1.logBatchQueue), "queue size not specified, should be default value")
    assert.Equal(t, defaultBatchSizeBytes, testSumoLogger1.batchSize, "batch size not specified, should be default value")
    assert.Equal(t, &tls.Config{}, testSumoLogger1.tlsConfig, "tls configs not specified, should be default value")
    assert.Nil(t, testSumoLogger1.proxyUrl, "proxy url not specified, should be default value")
    assert.Equal(t, testContainerID[:12], testSumoLogger1.tag, "tag not specified, should be default value")
    assert.Equal(t, defaultSourceCategory, testSumoLogger1.sourceCategory, "source category not specified, should be default value")
    assert.Equal(t, testContainerName, testSumoLogger1.sourceName, "source name not specified, should be default value")

    _, err = testSumoDriver.NewSumoLogger(filePath1, info)
    assert.Error(t, err, "trying to call StartLogging for filepath that already exists should return error")
    assert.Equal(t, 1, len(testSumoDriver.loggers),
      "there should still be one logger after calling StartLogging for filepath that already exists")

    testSumoLogger2, err := testSumoDriver.NewSumoLogger(filePath2, info)
    assert.Nil(t, err)
    assert.Equal(t, 2, len(testSumoDriver.loggers),
      "there should be two loggers now after calling StartLogging on driver for different filepaths")
    assert.Equal(t, info.Config[logOptUrl], testSumoLogger2.httpSourceUrl, "http source url should be configured correctly")

    err = testSumoDriver.StopLogging(filePath1)
    assert.Nil(t, err, "trying to call StopLogging for existing logger should not return error")
    assert.Equal(t, 1, len(testSumoDriver.loggers), "calling StopLogging on existing logger should remove the logger")
    err = testSumoDriver.StopLogging(filePath2)
    assert.Nil(t, err, "trying to call StopLogging for existing logger should not return error")
    assert.Equal(t, 0, len(testSumoDriver.loggers), "calling StopLogging on existing logger should remove the logger")
  })

  t.Run("StopLogging", func(t *testing.T) {
    testSumoDriver := newSumoDriver()
    assert.Equal(t, 0, len(testSumoDriver.loggers), "there should be no loggers when the driver is initialized")

    err := testSumoDriver.StopLogging(filePath1)
    assert.Nil(t, err, "trying to call StopLogging for nonexistent logger should NOT return error")
    assert.Equal(t, 0, len(testSumoDriver.loggers), "no loggers should be changed after calling StopLogging for nonexistent logger")

    _, err = testSumoDriver.NewSumoLogger(filePath1, info)
    assert.Nil(t, err)
    assert.Equal(t, 1, len(testSumoDriver.loggers), "there should be one logger after calling StartLogging on driver")

    err = testSumoDriver.StopLogging(filePath2)
    assert.Nil(t, err, "trying to call StopLogging for nonexistent logger should NOT return error")
    assert.Equal(t, 1, len(testSumoDriver.loggers), "no loggers should be changed after calling StopLogging for nonexistent logger")

    err = testSumoDriver.StopLogging(filePath1)
    assert.Nil(t, err, "trying to call StopLogging for existing logger should not return error")
    assert.Equal(t, 0, len(testSumoDriver.loggers), "calling StopLogging on existing logger should remove the logger")
  })

  t.Run("NewSumoLogger, concurrently", func(t *testing.T) {
    testSumoDriver := newSumoDriver()
    assert.Equal(t, 0, len(testSumoDriver.loggers), "there should be no loggers when the driver is initialized")

    waitForAllLoggers := make(chan int)
    for i := 0; i < testLoggersCount; i++ {
      go func(i int) {
        _, err := testSumoDriver.NewSumoLogger(filePath + strconv.Itoa(i + 1), info)
        assert.Nil(t, err)
        waitForAllLoggers <- i
      }(i)
    }
    for i := 0; i < testLoggersCount; i ++ {
      <-waitForAllLoggers
    }
    assert.Equal(t, testLoggersCount, len(testSumoDriver.loggers),
      "there should be %v loggers now after calling StartLogging on driver that many times on different filepaths", testLoggersCount)
  })
}

func TestDriversLogOpts (t *testing.T) {
  logrus.SetOutput(ioutil.Discard)

  testFifo, err := fifo.OpenFifo(context.Background(), filePath, unix.O_RDWR|unix.O_CREAT|unix.O_NONBLOCK, fileMode)
  assert.Nil(t, err)
  defer testFifo.Close()
  defer os.Remove(filePath)

  testProxyUrl, _ := url.Parse(testProxyUrlStr)
  testInsecureSkipVerify := true
  testServerName := "sumologic.net"

  testTlsConfig := &tls.Config{
    InsecureSkipVerify: testInsecureSkipVerify,
    ServerName: testServerName,
  }

  testGzipCompression := false
  testGzipCompressionLevel := gzip.BestCompression
  testSendingInterval := time.Second
  testQueueSize := 2000
  testBatchSize := 1000

  t.Run("NewSumoLogger with correct log opts", func(t *testing.T) {
    info := logger.Info{
      Config: map[string]string{
        logOptUrl: testHttpSourceUrl,
        logOptProxyUrl: testProxyUrlStr,
        logOptInsecureSkipVerify: strconv.FormatBool(testInsecureSkipVerify),
        logOptServerName: testServerName,
        logOptGzipCompression: strconv.FormatBool(testGzipCompression),
        logOptGzipCompressionLevel: strconv.Itoa(testGzipCompressionLevel),
        logOptSendingInterval: testSendingInterval.String(),
        logOptQueueSize: strconv.Itoa(testQueueSize),
        logOptBatchSize: strconv.Itoa(testBatchSize),
      },
      ContainerID: "containeriid",
    }

    testSumoDriver := newSumoDriver()
    assert.Equal(t, 0, len(testSumoDriver.loggers), "there should be no loggers when the driver is initialized")

    testSumoLogger, err := testSumoDriver.NewSumoLogger(filePath, info)
    assert.Nil(t, err)
    assert.Equal(t, 1, len(testSumoDriver.loggers), "there should be one logger after calling StartLogging on driver")
    assert.Equal(t, testHttpSourceUrl, testSumoLogger.httpSourceUrl, "http source url should be configured correctly")
    assert.Equal(t, testGzipCompression, testSumoLogger.gzipCompression, "compression specified, should be specified value")
    assert.Equal(t, testGzipCompressionLevel, testSumoLogger.gzipCompressionLevel, "compression level specified, should be specified value")
    assert.Equal(t, testSendingInterval, testSumoLogger.sendingInterval, "sending interval specified, should be specified value")
    assert.Equal(t, testQueueSize, cap(testSumoLogger.logBatchQueue), "queue size specified, should be specified value")
    assert.Equal(t, testBatchSize, testSumoLogger.batchSize, "batch size specified, should be specified value")
    assert.Equal(t, testProxyUrl, testSumoLogger.proxyUrl, "proxy url specified, should be specified value")
    assert.Equal(t, testTlsConfig, testSumoLogger.tlsConfig, "tls config options specified, should be specified value")
  })

  t.Run("NewSumoLogger with bad insecure skip verify", func(t *testing.T) {
    info := logger.Info{
      Config: map[string]string{
        logOptUrl: testHttpSourceUrl,
        logOptProxyUrl: testProxyUrlStr,
        logOptInsecureSkipVerify: "truee",
        logOptServerName: testServerName,
        logOptGzipCompression: strconv.FormatBool(testGzipCompression),
        logOptGzipCompressionLevel: strconv.Itoa(testGzipCompressionLevel),
        logOptSendingInterval: testSendingInterval.String(),
        logOptQueueSize: strconv.Itoa(testQueueSize),
        logOptBatchSize: strconv.Itoa(testBatchSize),
      },
      ContainerID: "containeriid",
    }

    testTlsConfigNoInsecureSkipVerify := &tls.Config{
      InsecureSkipVerify: defaultInsecureSkipVerify,
      ServerName: testServerName,
    }

    testSumoDriver := newSumoDriver()
    assert.Equal(t, 0, len(testSumoDriver.loggers), "there should be no loggers when the driver is initialized")

    testSumoLogger, err := testSumoDriver.NewSumoLogger(filePath, info)
    assert.Nil(t, err)
    assert.Equal(t, 1, len(testSumoDriver.loggers), "there should be one logger after calling StartLogging on driver")
    assert.Equal(t, testHttpSourceUrl, testSumoLogger.httpSourceUrl, "http source url should be configured correctly")
    assert.Equal(t, testGzipCompression, testSumoLogger.gzipCompression, "compression specified, should be specified value")
    assert.Equal(t, testGzipCompressionLevel, testSumoLogger.gzipCompressionLevel, "compression level specified, should be specified value")
    assert.Equal(t, testSendingInterval, testSumoLogger.sendingInterval, "sending interval specified, should be specified value")
    assert.Equal(t, testQueueSize, cap(testSumoLogger.logBatchQueue), "queue size specified, should be specified value")
    assert.Equal(t, testBatchSize, testSumoLogger.batchSize, "batch size specified, should be specified value")
    assert.Equal(t, testProxyUrl, testSumoLogger.proxyUrl, "proxy url specified, should be specified value")
    assert.Equal(t, testTlsConfigNoInsecureSkipVerify, testSumoLogger.tlsConfig,
      "server name specified, should be specified value; insecure skip verify specified incorrectly, should be default value")
  })

  t.Run("NewSumoLogger with bad gzip compression", func(t *testing.T) {
    info := logger.Info{
      Config: map[string]string{
        logOptUrl: testHttpSourceUrl,
        logOptProxyUrl: testProxyUrlStr,
        logOptInsecureSkipVerify: strconv.FormatBool(testInsecureSkipVerify),
        logOptServerName: testServerName,
        logOptGzipCompression: "truee",
        logOptGzipCompressionLevel: strconv.Itoa(testGzipCompressionLevel),
        logOptSendingInterval: testSendingInterval.String(),
        logOptQueueSize: strconv.Itoa(testQueueSize),
        logOptBatchSize: strconv.Itoa(testBatchSize),
      },
      ContainerID: "containeriid",
    }

    testSumoDriver := newSumoDriver()
    assert.Equal(t, 0, len(testSumoDriver.loggers), "there should be no loggers when the driver is initialized")

    testSumoLogger, err := testSumoDriver.NewSumoLogger(filePath, info)
    assert.Nil(t, err)
    assert.Equal(t, 1, len(testSumoDriver.loggers), "there should be one logger after calling StartLogging on driver")
    assert.Equal(t, testHttpSourceUrl, testSumoLogger.httpSourceUrl, "http source url should be configured correctly")
    assert.Equal(t, defaultGzipCompression, testSumoLogger.gzipCompression, "compression specified incorrectly, should be default value")
    assert.Equal(t, testGzipCompressionLevel, testSumoLogger.gzipCompressionLevel, "compression level specified, should be specified value")
    assert.Equal(t, testSendingInterval, testSumoLogger.sendingInterval, "sending interval specified, should be specified value")
    assert.Equal(t, testQueueSize, cap(testSumoLogger.logBatchQueue), "queue size specified, should be specified value")
    assert.Equal(t, testBatchSize, testSumoLogger.batchSize, "batch size specified, should be specified value")
    assert.Equal(t, testProxyUrl, testSumoLogger.proxyUrl, "proxy url specified, should be specified value")
    assert.Equal(t, testTlsConfig, testSumoLogger.tlsConfig, "tls config options specified, should be specified value")
  })

  t.Run("NewSumoLogger with bad gzip compression level", func(t *testing.T) {
    info := logger.Info{
      Config: map[string]string{
        logOptUrl: testHttpSourceUrl,
        logOptProxyUrl: testProxyUrlStr,
        logOptInsecureSkipVerify: strconv.FormatBool(testInsecureSkipVerify),
        logOptServerName: testServerName,
        logOptGzipCompression: strconv.FormatBool(testGzipCompression),
        logOptGzipCompressionLevel: "2o",
        logOptSendingInterval: testSendingInterval.String(),
        logOptQueueSize: strconv.Itoa(testQueueSize),
        logOptBatchSize: strconv.Itoa(testBatchSize),
      },
      ContainerID: "containeriid",
    }

    testSumoDriver := newSumoDriver()
    assert.Equal(t, 0, len(testSumoDriver.loggers), "there should be no loggers when the driver is initialized")

    testSumoLogger, err := testSumoDriver.NewSumoLogger(filePath, info)
    assert.Nil(t, err)
    assert.Equal(t, 1, len(testSumoDriver.loggers), "there should be one logger after calling StartLogging on driver")
    assert.Equal(t, testHttpSourceUrl, testSumoLogger.httpSourceUrl, "http source url should be configured correctly")
    assert.Equal(t, testGzipCompression, testSumoLogger.gzipCompression, "compression specified, should be specified value")
    assert.Equal(t, defaultGzipCompressionLevel, testSumoLogger.gzipCompressionLevel, "compression level specified incorrectly, should be default value")
    assert.Equal(t, testSendingInterval, testSumoLogger.sendingInterval, "sending interval specified, should be specified value")
    assert.Equal(t, testQueueSize, cap(testSumoLogger.logBatchQueue), "queue size specified, should be specified value")
    assert.Equal(t, testBatchSize, testSumoLogger.batchSize, "batch size specified, should be specified value")
    assert.Equal(t, testProxyUrl, testSumoLogger.proxyUrl, "proxy url specified, should be specified value")
    assert.Equal(t, testTlsConfig, testSumoLogger.tlsConfig, "tls config options specified, should be specified value")
  })

  t.Run("NewSumoLogger with unsupported gzip compression level", func(t *testing.T) {
    info := logger.Info{
      Config: map[string]string{
        logOptUrl: testHttpSourceUrl,
        logOptProxyUrl: testProxyUrlStr,
        logOptInsecureSkipVerify: strconv.FormatBool(testInsecureSkipVerify),
        logOptServerName: testServerName,
        logOptGzipCompression: strconv.FormatBool(testGzipCompression),
        logOptGzipCompressionLevel: "20",
        logOptSendingInterval: testSendingInterval.String(),
        logOptQueueSize: strconv.Itoa(testQueueSize),
        logOptBatchSize: strconv.Itoa(testBatchSize),
      },
      ContainerID: "containeriid",
    }

    testSumoDriver := newSumoDriver()
    assert.Equal(t, 0, len(testSumoDriver.loggers), "there should be no loggers when the driver is initialized")

    testSumoLogger, err := testSumoDriver.NewSumoLogger(filePath, info)
    assert.Nil(t, err)
    assert.Equal(t, 1, len(testSumoDriver.loggers), "there should be one logger after calling StartLogging on driver")
    assert.Equal(t, testHttpSourceUrl, testSumoLogger.httpSourceUrl, "http source url should be configured correctly")
    assert.Equal(t, testGzipCompression, testSumoLogger.gzipCompression, "compression specified, should be specified value")
    assert.Equal(t, defaultGzipCompressionLevel, testSumoLogger.gzipCompressionLevel, "compression level specified incorrectly, should be default value")
    assert.Equal(t, testSendingInterval, testSumoLogger.sendingInterval, "sending interval specified, should be specified value")
    assert.Equal(t, testQueueSize, cap(testSumoLogger.logBatchQueue), "queue size specified, should be specified value")
    assert.Equal(t, testBatchSize, testSumoLogger.batchSize, "batch size specified, should be specified value")
    assert.Equal(t, testProxyUrl, testSumoLogger.proxyUrl, "proxy url specified, should be specified value")
    assert.Equal(t, testTlsConfig, testSumoLogger.tlsConfig, "tls config options specified, should be specified value")
  })

  t.Run("NewSumoLogger with bad sending interval", func(t *testing.T) {
    info := logger.Info{
      Config: map[string]string{
        logOptUrl: testHttpSourceUrl,
        logOptProxyUrl: testProxyUrlStr,
        logOptInsecureSkipVerify: strconv.FormatBool(testInsecureSkipVerify),
        logOptServerName: testServerName,
        logOptGzipCompression: strconv.FormatBool(testGzipCompression),
        logOptGzipCompressionLevel: strconv.Itoa(testGzipCompressionLevel),
        logOptSendingInterval: "72h3n0.5s",
        logOptQueueSize: strconv.Itoa(testQueueSize),
        logOptBatchSize: strconv.Itoa(testBatchSize),
      },
      ContainerID: "containeriid",
    }

    testSumoDriver := newSumoDriver()
    assert.Equal(t, 0, len(testSumoDriver.loggers), "there should be no loggers when the driver is initialized")

    testSumoLogger, err := testSumoDriver.NewSumoLogger(filePath, info)
    assert.Nil(t, err)
    assert.Equal(t, 1, len(testSumoDriver.loggers), "there should be one logger after calling StartLogging on driver")
    assert.Equal(t, testHttpSourceUrl, testSumoLogger.httpSourceUrl, "http source url should be configured correctly")
    assert.Equal(t, testGzipCompression, testSumoLogger.gzipCompression, "compression specified, should be specified value")
    assert.Equal(t, testGzipCompressionLevel, testSumoLogger.gzipCompressionLevel, "compression level specified, should be specified value")
    assert.Equal(t, defaultSendingIntervalMs, testSumoLogger.sendingInterval, "sending interval specified incorrectly, should be default value")
    assert.Equal(t, testQueueSize, cap(testSumoLogger.logBatchQueue), "queue size specified, should be specified value")
    assert.Equal(t, testBatchSize, testSumoLogger.batchSize, "batch size specified, should be specified value")
    assert.Equal(t, testProxyUrl, testSumoLogger.proxyUrl, "proxy url specified, should be specified value")
    assert.Equal(t, testTlsConfig, testSumoLogger.tlsConfig, "tls config options specified, should be specified value")
  })

  t.Run("NewSumoLogger with unsupported sending interval", func(t *testing.T) {
    info := logger.Info{
      Config: map[string]string{
        logOptUrl: testHttpSourceUrl,
        logOptProxyUrl: testProxyUrlStr,
        logOptInsecureSkipVerify: strconv.FormatBool(testInsecureSkipVerify),
        logOptServerName: testServerName,
        logOptGzipCompression: strconv.FormatBool(testGzipCompression),
        logOptGzipCompressionLevel: strconv.Itoa(testGzipCompressionLevel),
        logOptSendingInterval: "0s",
        logOptQueueSize: strconv.Itoa(testQueueSize),
        logOptBatchSize: strconv.Itoa(testBatchSize),
      },
      ContainerID: "containeriid",
    }

    testSumoDriver := newSumoDriver()
    assert.Equal(t, 0, len(testSumoDriver.loggers), "there should be no loggers when the driver is initialized")

    testSumoLogger, err := testSumoDriver.NewSumoLogger(filePath, info)
    assert.Nil(t, err)
    assert.Equal(t, 1, len(testSumoDriver.loggers), "there should be one logger after calling StartLogging on driver")
    assert.Equal(t, testHttpSourceUrl, testSumoLogger.httpSourceUrl, "http source url should be configured correctly")
    assert.Equal(t, testGzipCompression, testSumoLogger.gzipCompression, "compression specified, should be specified value")
    assert.Equal(t, testGzipCompressionLevel, testSumoLogger.gzipCompressionLevel, "compression level specified, should be specified value")
    assert.Equal(t, defaultSendingIntervalMs, testSumoLogger.sendingInterval, "sending interval specified incorrectly, should be default value")
    assert.Equal(t, testQueueSize, cap(testSumoLogger.logBatchQueue), "queue size specified, should be specified value")
    assert.Equal(t, testBatchSize, testSumoLogger.batchSize, "batch size specified, should be specified value")
    assert.Equal(t, testProxyUrl, testSumoLogger.proxyUrl, "proxy url specified, should be specified value")
    assert.Equal(t, testTlsConfig, testSumoLogger.tlsConfig, "tls config options specified, should be specified value")
  })

  t.Run("NewSumoLogger with bad queue size", func(t *testing.T) {
    info := logger.Info{
      Config: map[string]string{
        logOptUrl: testHttpSourceUrl,
        logOptProxyUrl: testProxyUrlStr,
        logOptInsecureSkipVerify: strconv.FormatBool(testInsecureSkipVerify),
        logOptServerName: testServerName,
        logOptGzipCompression: strconv.FormatBool(testGzipCompression),
        logOptGzipCompressionLevel: strconv.Itoa(testGzipCompressionLevel),
        logOptSendingInterval: testSendingInterval.String(),
        logOptQueueSize: "2ooo",
        logOptBatchSize: strconv.Itoa(testBatchSize),
      },
      ContainerID: "containeriid",
    }

    testSumoDriver := newSumoDriver()
    assert.Equal(t, 0, len(testSumoDriver.loggers), "there should be no loggers when the driver is initialized")

    testSumoLogger, err := testSumoDriver.NewSumoLogger(filePath, info)
    assert.Nil(t, err)
    assert.Equal(t, 1, len(testSumoDriver.loggers), "there should be one logger after calling StartLogging on driver")
    assert.Equal(t, testHttpSourceUrl, testSumoLogger.httpSourceUrl, "http source url should be configured correctly")
    assert.Equal(t, testGzipCompression, testSumoLogger.gzipCompression, "compression specified, should be specified value")
    assert.Equal(t, testGzipCompressionLevel, testSumoLogger.gzipCompressionLevel, "compression level specified, should be specified value")
    assert.Equal(t, testSendingInterval, testSumoLogger.sendingInterval, "sending interval specified, should be specified value")
    assert.Equal(t, defaultQueueSizeItems, cap(testSumoLogger.logBatchQueue), "queue size specified incorrectly, should be default value")
    assert.Equal(t, testBatchSize, testSumoLogger.batchSize, "batch size specified, should be specified value")
    assert.Equal(t, testProxyUrl, testSumoLogger.proxyUrl, "proxy url specified, should be specified value")
    assert.Equal(t, testTlsConfig, testSumoLogger.tlsConfig, "tls config options specified, should be specified value")
  })

  t.Run("NewSumoLogger with unsupported queue size", func(t *testing.T) {
    info := logger.Info{
      Config: map[string]string{
        logOptUrl: testHttpSourceUrl,
        logOptProxyUrl: testProxyUrlStr,
        logOptInsecureSkipVerify: strconv.FormatBool(testInsecureSkipVerify),
        logOptServerName: testServerName,
        logOptGzipCompression: strconv.FormatBool(testGzipCompression),
        logOptGzipCompressionLevel: strconv.Itoa(testGzipCompressionLevel),
        logOptSendingInterval: testSendingInterval.String(),
        logOptQueueSize: "-2000",
        logOptBatchSize: strconv.Itoa(testBatchSize),
      },
      ContainerID: "containeriid",
    }

    testSumoDriver := newSumoDriver()
    assert.Equal(t, 0, len(testSumoDriver.loggers), "there should be no loggers when the driver is initialized")

    testSumoLogger, err := testSumoDriver.NewSumoLogger(filePath, info)
    assert.Nil(t, err)
    assert.Equal(t, 1, len(testSumoDriver.loggers), "there should be one logger after calling StartLogging on driver")
    assert.Equal(t, testHttpSourceUrl, testSumoLogger.httpSourceUrl, "http source url should be configured correctly")
    assert.Equal(t, testGzipCompression, testSumoLogger.gzipCompression, "compression specified, should be specified value")
    assert.Equal(t, testGzipCompressionLevel, testSumoLogger.gzipCompressionLevel, "compression level specified, should be specified value")
    assert.Equal(t, testSendingInterval, testSumoLogger.sendingInterval, "sending interval specified, should be specified value")
    assert.Equal(t, defaultQueueSizeItems, cap(testSumoLogger.logBatchQueue), "queue size specified incorrectly, should be default value")
    assert.Equal(t, testBatchSize, testSumoLogger.batchSize, "batch size specified, should be specified value")
    assert.Equal(t, testProxyUrl, testSumoLogger.proxyUrl, "proxy url specified, should be specified value")
    assert.Equal(t, testTlsConfig, testSumoLogger.tlsConfig, "tls config options specified, should be specified value")
  })

  t.Run("NewSumoLogger with bad batch size", func(t *testing.T) {
    info := logger.Info{
      Config: map[string]string{
        logOptUrl: testHttpSourceUrl,
        logOptProxyUrl: testProxyUrlStr,
        logOptInsecureSkipVerify: strconv.FormatBool(testInsecureSkipVerify),
        logOptServerName: testServerName,
        logOptGzipCompression: strconv.FormatBool(testGzipCompression),
        logOptGzipCompressionLevel: strconv.Itoa(testGzipCompressionLevel),
        logOptSendingInterval: testSendingInterval.String(),
        logOptQueueSize: strconv.Itoa(testQueueSize),
        logOptBatchSize: "2ooo",
      },
      ContainerID: "containeriid",
    }

    testSumoDriver := newSumoDriver()
    assert.Equal(t, 0, len(testSumoDriver.loggers), "there should be no loggers when the driver is initialized")

    testSumoLogger, err := testSumoDriver.NewSumoLogger(filePath, info)
    assert.Nil(t, err)
    assert.Equal(t, 1, len(testSumoDriver.loggers), "there should be one logger after calling StartLogging on driver")
    assert.Equal(t, testHttpSourceUrl, testSumoLogger.httpSourceUrl, "http source url should be configured correctly")
    assert.Equal(t, testGzipCompression, testSumoLogger.gzipCompression, "compression specified, should be specified value")
    assert.Equal(t, testGzipCompressionLevel, testSumoLogger.gzipCompressionLevel, "compression level specified, should be specified value")
    assert.Equal(t, testSendingInterval, testSumoLogger.sendingInterval, "sending interval specified, should be specified value")
    assert.Equal(t, testQueueSize, cap(testSumoLogger.logBatchQueue), "queue size specified, should be specified value")
    assert.Equal(t, defaultBatchSizeBytes, testSumoLogger.batchSize, "batch size specified incorrectly, should be default value")
    assert.Equal(t, testProxyUrl, testSumoLogger.proxyUrl, "proxy url specified, should be specified value")
    assert.Equal(t, testTlsConfig, testSumoLogger.tlsConfig, "tls config options specified, should be specified value")
  })

  t.Run("NewSumoLogger with unsupported batch size", func(t *testing.T) {
    info := logger.Info{
      Config: map[string]string{
        logOptUrl: testHttpSourceUrl,
        logOptProxyUrl: testProxyUrlStr,
        logOptInsecureSkipVerify: strconv.FormatBool(testInsecureSkipVerify),
        logOptServerName: testServerName,
        logOptGzipCompression: strconv.FormatBool(testGzipCompression),
        logOptGzipCompressionLevel: strconv.Itoa(testGzipCompressionLevel),
        logOptSendingInterval: testSendingInterval.String(),
        logOptQueueSize: strconv.Itoa(testQueueSize),
        logOptBatchSize: "-2000",
      },
      ContainerID: "containeriid",
    }

    testSumoDriver := newSumoDriver()
    assert.Equal(t, 0, len(testSumoDriver.loggers), "there should be no loggers when the driver is initialized")

    testSumoLogger, err := testSumoDriver.NewSumoLogger(filePath, info)
    assert.Nil(t, err)
    assert.Equal(t, 1, len(testSumoDriver.loggers), "there should be one logger after calling StartLogging on driver")
    assert.Equal(t, testHttpSourceUrl, testSumoLogger.httpSourceUrl, "http source url should be configured correctly")
    assert.Equal(t, testGzipCompression, testSumoLogger.gzipCompression, "compression specified, should be specified value")
    assert.Equal(t, testGzipCompressionLevel, testSumoLogger.gzipCompressionLevel, "compression level specified, should be specified value")
    assert.Equal(t, testSendingInterval, testSumoLogger.sendingInterval, "sending interval specified, should be specified value")
    assert.Equal(t, testQueueSize, cap(testSumoLogger.logBatchQueue), "queue size specified, should be specified value")
    assert.Equal(t, defaultBatchSizeBytes, testSumoLogger.batchSize, "batch size specified incorrectly, should be default value")
    assert.Equal(t, testProxyUrl, testSumoLogger.proxyUrl, "proxy url specified, should be specified value")
    assert.Equal(t, testTlsConfig, testSumoLogger.tlsConfig, "tls config options specified, should be specified value")
  })

  t.Run("NewSumoLogger with metadata", func(t *testing.T) {
    testContainerID := "123456789012345678901234567890"
    testContainerName := "testContainerName"
    testContainerImageID := "987654321098765432109876543210"
    testContainerImageName := "testContainerImageName"
    testDaemonName := "testDaemonName"

    testTag := "{{.DaemonName}}/{{.ImageName}}/{{.Name}}/{{.FullID}}-{{.ImageID}}"

    testSourceCategory := "testSourceCategory:{{.Tag}}/test"
    testSourceName := "{{.Tag}}/test"
    testSourceHost := "/test/{{.Tag}}"

    info := logger.Info{
      Config: map[string]string{
        logOptUrl: testHttpSourceUrl,
        "tag": testTag,
        logOptSourceCategory: testSourceCategory,
        logOptSourceName: testSourceName,
        logOptSourceHost: testSourceHost,
      },
      ContainerID: testContainerID,
      ContainerName: testContainerName,
      ContainerImageID: testContainerImageID,
      ContainerImageName: testContainerImageName,
      DaemonName: testDaemonName,
    }

    expectedTag := testDaemonName + "/" + testContainerImageName + "/" +
                   testContainerName + "/" + testContainerID + "-" + testContainerImageID[:12]
    expectedSourceCategory := "testSourceCategory:" + expectedTag + "/test"
    expectedSourceName := expectedTag + "/test"
    expectedSourceHost := "/test/" + expectedTag

    testSumoDriver := newSumoDriver()
    assert.Equal(t, 0, len(testSumoDriver.loggers), "there should be no loggers when the driver is initialized")

    testSumoLogger, err := testSumoDriver.NewSumoLogger(filePath, info)
    assert.Nil(t, err)
    assert.Equal(t, 1, len(testSumoDriver.loggers),
      "there should be one logger after calling StartLogging on driver")
    assert.Equal(t, expectedTag, testSumoLogger.tag,
      "tag specified, should be expected value")
    assert.Equal(t, expectedSourceCategory, testSumoLogger.sourceCategory,
      "sourceCategory specified, should be expected value")
    assert.Equal(t, expectedSourceName, testSumoLogger.sourceName,
      "sourceName specified, should be expected value")
    assert.Equal(t, expectedSourceHost, testSumoLogger.sourceHost,
      "sourceHost specified, should be expected value")
  })
}
