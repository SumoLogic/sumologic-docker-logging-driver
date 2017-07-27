package main

import (
  "bytes"
  "context"
  "io/ioutil"
  "net/http"
  "os"
  "testing"
  "time"

  "github.com/docker/docker/api/types/plugins/logdriver"
  "github.com/stretchr/testify/assert"
  "github.com/tonistiigi/fifo"
  "golang.org/x/sys/unix"
)

type mockHttpClient struct {
  requestCount int
  statusCode int
}

func (m *mockHttpClient) Do(req *http.Request) (*http.Response, error) {
  m.requestCount += 1
  return &http.Response{
      Body: ioutil.NopCloser(bytes.NewBuffer([]byte("ERROR EXPECTED, mock response for testing"))),
      StatusCode: m.statusCode,
    }, nil
}

func (m *mockHttpClient) Reset() {
  m.requestCount = 0
}

func NewMockHttpClient(statusCode int) *mockHttpClient {
  return &mockHttpClient{
    requestCount: 0,
    statusCode: statusCode,
  }
}

func TestConsumeLogsFromFifo(t *testing.T) {
  testLogMessage := &logdriver.LogEntry{
		Source: testSource,
		TimeNano: testTime,
		Line: testLine,
		Partial: testPartial,
  }

  fifoLogStream, err := fifo.OpenFifo(context.Background(), filePath, unix.O_RDWR|unix.O_CREAT|unix.O_NONBLOCK, 0700)
  defer os.Remove(filePath)
  assert.Nil(t, err)

  testSumoLogger := &sumoLogger{
    httpSourceUrl: httpSourceUrl,
    client: NewMockHttpClient(http.StatusOK),
    fifoLogStream: fifoLogStream,
    logStream: make(chan *sumoLog, 4000),
    frequency: 5 * time.Second,
  }

  go consumeLogsFromFifo(testSumoLogger)

  enc := logdriver.NewLogEntryEncoder(fifoLogStream)

  t.Run("Consume one log", func(t *testing.T) {
    enc.Encode(testLogMessage)
    consumedLog := <-testSumoLogger.logStream
    assert.Equal(t, testSource, consumedLog.Source, "should read the correct log source")
    assert.Equal(t, testLine, consumedLog.Line, "should read the correct log line")
    assert.Equal(t, testPartial, consumedLog.Partial, "should read the correct log partial")
  })

  t.Run("Consume many logs", func(t *testing.T) {
    testLogsCount := 1000
    for i := 0; i < testLogsCount; i++ {
      enc.Encode(testLogMessage)
    }
    for i := 0; i < testLogsCount; i++ {
      consumedLog := <-testSumoLogger.logStream
      assert.Equal(t, testSource, consumedLog.Source, "should read the correct log source")
      assert.Equal(t, testLine, consumedLog.Line, "should read the correct log line")
      assert.Equal(t, testPartial, consumedLog.Partial, "should read the correct log partial")
    }
  })
}

func TestQueueLogsForSending(t *testing.T) {
  testSumoLog := &sumoLog{
		Source: testSource,
		Line: testLine,
		Partial: testPartial,
  }

  t.Run("batchSize=1", func(t *testing.T) {
    testLogStream := make(chan *sumoLog, 4000)
    testClient := NewMockHttpClient(http.StatusOK)
    testSumoLogger := &sumoLogger{
      httpSourceUrl: httpSourceUrl,
      client: testClient,
      logStream: testLogStream,
      frequency: 2 * time.Second,
      batchSize: 1,
    }
    go queueLogsForSending(testSumoLogger)

    testLogStream <- testSumoLog
    time.Sleep(time.Second)
    assert.Equal(t, 1, testClient.requestCount, "should have received one request")
    testClient.Reset()

    testLogCount := 1000
    for i := 0; i < testLogCount; i++ {
      testLogStream <- testSumoLog
    }
    time.Sleep(time.Second)
    assert.Equal(t, testLogCount, testClient.requestCount, "should have received %v requests", testLogCount)
    testClient.Reset()
  })

  t.Run("batchSize=10", func(t *testing.T) {
    testLogStream := make(chan *sumoLog, 4000)
    testClient := NewMockHttpClient(http.StatusOK)
    testBatchSize := 10
    testSumoLogger := &sumoLogger{
      httpSourceUrl: httpSourceUrl,
      client: testClient,
      logStream: testLogStream,
      frequency: 2 * time.Second,
      batchSize: testBatchSize,
    }
    go queueLogsForSending(testSumoLogger)

    testLogStream <- testSumoLog
    time.Sleep(time.Second)
    assert.Equal(t, 0, testClient.requestCount,
      "should have received no requests, fewer messages than batch size (timer should not have clicked in time)")
    testClient.Reset()

    testLogCount := 1000
    for i := 0; i < testLogCount; i++ {
      testLogStream <- testSumoLog
    }
    time.Sleep(2 * time.Second)
    assert.Equal(t, testLogCount / testBatchSize + 1, testClient.requestCount,
      "should have received (number of logs / batch size) requests, " +
      "plus one for the single message we sent previously (timer should have clicked by now)")
    testClient.Reset()
  })

  t.Run("batchSize=1000", func(t *testing.T) {
    testLogStream := make(chan *sumoLog, 4000)
    testClient := NewMockHttpClient(http.StatusOK)
    testBatchSize := 1000
    testSumoLogger := &sumoLogger{
      httpSourceUrl: httpSourceUrl,
      client: testClient,
      logStream: testLogStream,
      frequency: 2 * time.Second,
      batchSize: testBatchSize,
    }
    go queueLogsForSending(testSumoLogger)

    testLogStream <- testSumoLog
    time.Sleep(time.Second)
    assert.Equal(t, 0, testClient.requestCount,
      "should have received no requests, fewer messages than batch size (timer should not have clicked in time)")
    testClient.Reset()

    testLogCount := 1000
    for i := 0; i < testLogCount; i++ {
      testLogStream <- testSumoLog
    }
    time.Sleep(2 * time.Second)
    assert.Equal(t, testLogCount / testBatchSize + 1, testClient.requestCount,
      "should have received (number of logs / batch size) requests, " +
      "plus one for the single message we sent previously (timer should have clicked by now)")
    testClient.Reset()
  })
}

func TestSendLogs(t *testing.T) {
  testLogStream := make(chan *sumoLog, 4000)

  t.Run("logCount=0, status=OK", func(t *testing.T) {
    var testLogs []*sumoLog

    testClient := NewMockHttpClient(http.StatusOK)
    testSumoLogger := &sumoLogger{
      httpSourceUrl: httpSourceUrl,
      client: testClient,
      logStream: testLogStream,
      frequency: defaultFrequency,
      batchSize: defaultBatchSize,
    }

    testLogs = testSumoLogger.sendLogs(testLogs)
    assert.Equal(t, 0, len(testLogs), "should be no logs in queue")
    assert.Equal(t, 0, testClient.requestCount, "should have received no requests")
  })

  t.Run("logCount=1, status=OK", func(t *testing.T) {
    var testLogs []*sumoLog
    testLog := &sumoLog{
  		Source: testSource,
  		Line: testLine,
  		Partial: testPartial,
    }
    testLogs = append(testLogs, testLog)

    testClient := NewMockHttpClient(http.StatusOK)
    testSumoLogger := &sumoLogger{
      httpSourceUrl: httpSourceUrl,
      client: testClient,
      logStream: testLogStream,
      frequency: defaultFrequency,
      batchSize: defaultBatchSize,
    }

    testLogs = testSumoLogger.sendLogs(testLogs)
    assert.Equal(t, 0, len(testLogs), "should be no logs in queue")
    assert.Equal(t, 1, testClient.requestCount, "should have received one request")
  })

  t.Run("logCount=1000, status=OK", func(t *testing.T) {
    logCount := 1000
    var testLogs []*sumoLog
    testLog := &sumoLog{
  		Source: testSource,
  		Line: testLine,
  		Partial: testPartial,
    }
    for i := 0; i < logCount; i++ {
      testLogs = append(testLogs, testLog)
    }

    testClient := NewMockHttpClient(http.StatusOK)
    testSumoLogger := &sumoLogger{
      httpSourceUrl: httpSourceUrl,
      client: testClient,
      logStream: testLogStream,
      frequency: defaultFrequency,
      batchSize: defaultBatchSize,
    }

    testLogs = testSumoLogger.sendLogs(testLogs)
    assert.Equal(t, 0, len(testLogs), "should be no logs in queue")
    assert.Equal(t, 1, testClient.requestCount, "should have received one request, logs are batched")
  })

  t.Run("logCount=1, status=BadRequest", func(t *testing.T) {
    var testLogs []*sumoLog
    testLog := &sumoLog{
  		Source: testSource,
  		Line: testLine,
  		Partial: testPartial,
    }
    testLogs = append(testLogs, testLog)

    testClient := NewMockHttpClient(http.StatusBadRequest)
    testSumoLogger := &sumoLogger{
      httpSourceUrl: httpSourceUrl,
      client: testClient,
      logStream: testLogStream,
      frequency: defaultFrequency,
      batchSize: defaultBatchSize,
    }

    testLogs = testSumoLogger.sendLogs(testLogs)
    assert.Equal(t, 1, len(testLogs), "all logs should be returned to queue")
    assert.Equal(t, 1, testClient.requestCount, "should have received one request, logs are batched")
  })

  t.Run("logCount=1000, status=BadRequest", func(t *testing.T) {
    logCount := 1000
    var testLogs []*sumoLog
    testLog := &sumoLog{
  		Source: testSource,
  		Line: testLine,
  		Partial: testPartial,
    }
    for i := 0; i < logCount; i++ {
      testLogs = append(testLogs, testLog)
    }

    testClient := NewMockHttpClient(http.StatusBadRequest)
    testSumoLogger := &sumoLogger{
      httpSourceUrl: httpSourceUrl,
      client: testClient,
      logStream: testLogStream,
      frequency: defaultFrequency,
      batchSize: defaultBatchSize,
    }

    testLogs = testSumoLogger.sendLogs(testLogs)
    assert.Equal(t, logCount, len(testLogs), "all logs should be returned to queue")
    assert.Equal(t, 1, testClient.requestCount, "should have received one request, logs are batched")
  })
}
