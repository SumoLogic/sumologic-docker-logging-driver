package main

import (
  "context"
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
)

func TestDrivers (t *testing.T) {
  testLoggersCount := 100

  for i := 0; i < testLoggersCount; i++ {
    testFifo, err := fifo.OpenFifo(context.Background(), filePath + strconv.Itoa(i + 1), unix.O_WRONLY|unix.O_CREAT|unix.O_NONBLOCK, 0700)
    assert.Nil(t, err)
    defer testFifo.Close()
  }

  info := logger.Info{
    Config: map[string]string{
      logOptUrl: "https://example.org",
    },
    ContainerID: "containeriid",
  }

  t.Run("StartLogging", func(t *testing.T) {
    testSumoDriver := NewSumoDriver()
    assert.Equal(t, 0, len(testSumoDriver.loggers), "there should be no loggers when the driver is initialized")

    err := testSumoDriver.StartLogging(filePath1, info)
    assert.Nil(t, err)
    assert.Equal(t, 1, len(testSumoDriver.loggers), "there should be one logger after calling StartLogging on driver")

    err = testSumoDriver.StartLogging(filePath1, info)
    assert.Error(t, err, "trying to call StartLogging for filepath that already exists should return error")
    assert.Equal(t, 1, len(testSumoDriver.loggers),
      "there should still be one logger after calling StartLogging for filepath that already exists")

    err = testSumoDriver.StartLogging(filePath2, info)
    assert.Nil(t, err)
    assert.Equal(t, 2, len(testSumoDriver.loggers),
      "there should be two loggers now after calling StartLogging on driver for different filepaths")
  })

  t.Run("StopLogging", func(t *testing.T) {
    testSumoDriver := NewSumoDriver()
    assert.Equal(t, 0, len(testSumoDriver.loggers), "there should be no loggers when the driver is initialized")

    err := testSumoDriver.StopLogging(filePath1)
    assert.Nil(t, err, "trying to call StopLogging for nonexistent logger should NOT return error")
    assert.Equal(t, 0, len(testSumoDriver.loggers), "no loggers should be changed after calling StopLogging for nonexistent logger")

    err = testSumoDriver.StartLogging(filePath1, info)
    assert.Nil(t, err)
    assert.Equal(t, 1, len(testSumoDriver.loggers), "there should be one logger after calling StartLogging on driver")

    err = testSumoDriver.StopLogging(filePath2)
    assert.Nil(t, err, "trying to call StopLogging for nonexistent logger should NOT return error")
    assert.Equal(t, 1, len(testSumoDriver.loggers), "no loggers should be changed after calling StopLogging for nonexistent logger")

    err = testSumoDriver.StopLogging(filePath1)
    assert.Nil(t, err, "trying to call StopLogging for existing logger should not return error")
    assert.Equal(t, 0, len(testSumoDriver.loggers), "calling StopLogging on existing logger should remove the logger")
  })

  t.Run("StartLogging, concurrently", func(t *testing.T) {
    testSumoDriver := NewSumoDriver()
    assert.Equal(t, 0, len(testSumoDriver.loggers), "there should be no loggers when the driver is initialized")

    waitForAllLoggers := make(chan int)
    for i := 0; i < testLoggersCount; i++ {
      go func(i int) {
        err := testSumoDriver.StartLogging(filePath + strconv.Itoa(i + 1), info)
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
