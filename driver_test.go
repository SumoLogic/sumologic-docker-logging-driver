package main

import (
  "context"
  "os"
  "strconv"
  "testing"

  "github.com/docker/docker/daemon/logger"
  "github.com/stretchr/testify/assert"
  "github.com/tonistiigi/fifo"
  "golang.org/x/sys/unix"
)

const (
  filePath  = "/tmp/test"
  filePath1 = "/tmp/test1"
  filePath2 = "/tmp/test2"

  httpSourceUrl = "https://example.org"

  testSource = "sumo-test"
  testTime = 1234567890
  testIsPartial = false
)

var (
  testLine = []byte("a test log message")
)

func TestDrivers (t *testing.T) {
  testLoggersCount := 100

  for i := 0; i < testLoggersCount; i++ {
    testFifo, err := fifo.OpenFifo(context.Background(), filePath + strconv.Itoa(i + 1), unix.O_RDWR|unix.O_CREAT|unix.O_NONBLOCK, fileMode)
    assert.Nil(t, err)
    defer testFifo.Close()
    defer os.Remove(filePath + strconv.Itoa(i + 1))
  }

  info := logger.Info{
    Config: map[string]string{
      logOptUrl: httpSourceUrl,
    },
    ContainerID: "containeriid",
  }

  t.Run("startLoggingInternal", func(t *testing.T) {
    testSumoDriver := newSumoDriver()
    assert.Equal(t, 0, len(testSumoDriver.loggers), "there should be no loggers when the driver is initialized")

    testSumoLogger1, err := testSumoDriver.startLoggingInternal(filePath1, info)
    assert.Nil(t, err)
    assert.Equal(t, 1, len(testSumoDriver.loggers), "there should be one logger after calling StartLogging on driver")
    assert.Equal(t, info.Config[logOptUrl], testSumoLogger1.httpSourceUrl, "http source url should be configured correctly")

    _, err = testSumoDriver.startLoggingInternal(filePath1, info)
    assert.Error(t, err, "trying to call StartLogging for filepath that already exists should return error")
    assert.Equal(t, 1, len(testSumoDriver.loggers),
      "there should still be one logger after calling StartLogging for filepath that already exists")

    testSumoLogger2, err := testSumoDriver.startLoggingInternal(filePath2, info)
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

    _, err = testSumoDriver.startLoggingInternal(filePath1, info)
    assert.Nil(t, err)
    assert.Equal(t, 1, len(testSumoDriver.loggers), "there should be one logger after calling StartLogging on driver")

    err = testSumoDriver.StopLogging(filePath2)
    assert.Nil(t, err, "trying to call StopLogging for nonexistent logger should NOT return error")
    assert.Equal(t, 1, len(testSumoDriver.loggers), "no loggers should be changed after calling StopLogging for nonexistent logger")

    err = testSumoDriver.StopLogging(filePath1)
    assert.Nil(t, err, "trying to call StopLogging for existing logger should not return error")
    assert.Equal(t, 0, len(testSumoDriver.loggers), "calling StopLogging on existing logger should remove the logger")
  })

  t.Run("startLoggingInternal, concurrently", func(t *testing.T) {
    testSumoDriver := newSumoDriver()
    assert.Equal(t, 0, len(testSumoDriver.loggers), "there should be no loggers when the driver is initialized")

    waitForAllLoggers := make(chan int)
    for i := 0; i < testLoggersCount; i++ {
      go func(i int) {
        _, err := testSumoDriver.startLoggingInternal(filePath + strconv.Itoa(i + 1), info)
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
