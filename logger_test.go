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
  "github.com/sirupsen/logrus"
  "github.com/stretchr/testify/assert"
  "github.com/tonistiigi/fifo"
  "golang.org/x/sys/unix"
)

type mockHttpClient struct {
  requestCount int
  statusCode int
  requestReceivedSignal chan bool
}

func (m *mockHttpClient) Do(req *http.Request) (*http.Response, error) {
  m.requestCount += 1
  m.requestReceivedSignal <- true
  return &http.Response{
      Body: ioutil.NopCloser(bytes.NewBuffer([]byte("ERROR EXPECTED, mock response for testing"))),
      StatusCode: m.statusCode,
    }, nil
}

func (m *mockHttpClient) Reset() {
  m.requestCount = 0
  m.requestReceivedSignal = make(chan bool, defaultQueueSizeItems)
}

func NewMockHttpClient(statusCode int) *mockHttpClient {
  return &mockHttpClient{
    requestCount: 0,
    statusCode: statusCode,
    requestReceivedSignal: make(chan bool, defaultQueueSizeItems),
  }
}

func TestConsumeLogsFromFile(t *testing.T) {
  testLogMessage := &logdriver.LogEntry{
    Source: testSource,
    TimeNano: testTime,
    Line: testLine,
    Partial: testIsPartial,
  }

  inputFile, err := fifo.OpenFifo(context.Background(), filePath, unix.O_RDWR|unix.O_CREAT|unix.O_NONBLOCK, fileMode)
  defer os.Remove(filePath)
  assert.Nil(t, err)

  testSumoLogger := &sumoLogger{
    httpSourceUrl: testHttpSourceUrl,
    inputFile: inputFile,
    logQueue: make(chan *sumoLog, 10 * defaultQueueSizeItems),
    logBatchQueue: make(chan []*sumoLog, defaultQueueSizeItems),
    sendingInterval: time.Second,
  }

  go testSumoLogger.consumeLogsFromFile()

  enc := logdriver.NewLogEntryEncoder(inputFile)

  t.Run("logCount=1", func(t *testing.T) {
    enc.Encode(testLogMessage)
    consumedLog := <-testSumoLogger.logQueue
    assert.Equal(t, testSource, consumedLog.source, "should read the correct log source")
    assert.Equal(t, testLine, consumedLog.line, "should read the correct log line")
    assert.Equal(t, testIsPartial, consumedLog.isPartial, "should read the correct log partial")
  })

  t.Run("logCount=100000", func(t *testing.T) {
    testLogsCount := 100000
    go func() {
      for i := 0; i < testLogsCount; i++ {
        enc.Encode(testLogMessage)
      }
    }()
    for i := 0; i < testLogsCount; i++ {
      consumedLog := <-testSumoLogger.logQueue
      assert.Equal(t, testSource, consumedLog.source, "should read the correct log source")
      assert.Equal(t, testLine, consumedLog.line, "should read the correct log line")
      assert.Equal(t, testIsPartial, consumedLog.isPartial, "should read the correct log partial")
    }
  })
}

func TestBatchLogs(t *testing.T) {
  logrus.SetOutput(ioutil.Discard)
  testSumoLog := &sumoLog{
    source: testSource,
    line: testLine,
    isPartial: testIsPartial,
  }

  t.Run("batchSize=1 byte", func(t *testing.T) {
    testLogQueue := make(chan *sumoLog, 10 * defaultQueueSizeItems)
    testLogBatchQueue := make(chan []*sumoLog, defaultQueueSizeItems)
    testSumoLogger := &sumoLogger{
      httpSourceUrl: testHttpSourceUrl,
      logQueue: testLogQueue,
      logBatchQueue: testLogBatchQueue,
      sendingInterval: 400 * time.Millisecond,
      batchSize: 1,
    }
    go testSumoLogger.batchLogs()

    testLogQueue <- testSumoLog
    time.Sleep(500 * time.Millisecond)
    assert.Equal(t, 0, len(testLogBatchQueue), "should have dropped the log for being too large")
  })

  t.Run("batchSize=200 bytes, logCount=1", func(t *testing.T) {
    testBatchSize := 200
    testLogQueue := make(chan *sumoLog, 100 * defaultQueueSizeItems)
    testLogBatchQueue := make(chan []*sumoLog, defaultQueueSizeItems)
    testSumoLogger := &sumoLogger{
      httpSourceUrl: testHttpSourceUrl,
      logQueue: testLogQueue,
      logBatchQueue: testLogBatchQueue,
      sendingInterval: time.Second,
      batchSize: testBatchSize,
    }
    go testSumoLogger.batchLogs()

    testLogQueue <- testSumoLog
    testLogBatch := <-testLogBatchQueue
    assert.Equal(t, 1, len(testLogBatch), "should have received only one log in the batch")
    assert.Equal(t, testLine, testLogBatch[0].line, "should have received the correct log")
    assert.Equal(t, 0, len(testLogBatchQueue), "should have emptied out the batch queue")
  })

  t.Run("batchSize=200 bytes, logCount=100000", func(t *testing.T) {
    testBatchSize := 200
    testLogQueue := make(chan *sumoLog, 100 * defaultQueueSizeItems)
    testLogBatchQueue := make(chan []*sumoLog, defaultQueueSizeItems)
    testSumoLogger := &sumoLogger{
      httpSourceUrl: testHttpSourceUrl,
      logQueue: testLogQueue,
      logBatchQueue: testLogBatchQueue,
      sendingInterval: time.Hour,
      batchSize: testBatchSize,
    }
    go testSumoLogger.batchLogs()

    testLogCount := 100000
    go func() {
      for i := 0; i < testLogCount; i++ {
        testLogQueue <- testSumoLog
      }
    }()
    for i := 0; i < testLogCount / (testBatchSize / len(testLine)); i++ {
      testLogBatch := <-testLogBatchQueue
      assert.Equal(t, testBatchSize / len(testLine), len(testLogBatch),
        "should have correct number of logs in a batch")
      assert.Equal(t, testLine, testLogBatch[0].line, "should have received the correct log")
    }
    assert.Equal(t, 0, len(testLogBatchQueue), "should have emptied out the batch queue")
  })

  t.Run("batchSize=2000000 bytes, logCount=1", func(t *testing.T) {
    testBatchSize := 2000000
    testLogQueue := make(chan *sumoLog, 100 * defaultQueueSizeItems)
    testLogBatchQueue := make(chan []*sumoLog, defaultQueueSizeItems)
    testSumoLogger := &sumoLogger{
      httpSourceUrl: testHttpSourceUrl,
      logQueue: testLogQueue,
      logBatchQueue: testLogBatchQueue,
      sendingInterval: time.Second,
      batchSize: testBatchSize,
    }
    go testSumoLogger.batchLogs()

    testLogQueue <- testSumoLog
    testLogBatch := <-testLogBatchQueue
    assert.Equal(t, 1, len(testLogBatch), "should have received only one log in the batch")
    assert.Equal(t, testLine, testLogBatch[0].line, "should have received the correct log")
    assert.Equal(t, 0, len(testLogBatchQueue), "should have emptied out the batch queue")
  })

  t.Run("batchSize=2000000 bytes, logCount=1000000", func(t *testing.T) {
    testBatchSize := 2000000
    testLogQueue := make(chan *sumoLog, 100 * defaultQueueSizeItems)
    testLogBatchQueue := make(chan []*sumoLog, defaultQueueSizeItems)
    testSumoLogger := &sumoLogger{
      httpSourceUrl: testHttpSourceUrl,
      logQueue: testLogQueue,
      logBatchQueue: testLogBatchQueue,
      sendingInterval: time.Hour,
      batchSize: testBatchSize,
    }
    go testSumoLogger.batchLogs()

    testLogCount := 1000000
    go func() {
      for i := 0; i < testLogCount; i++ {
        testLogQueue <- testSumoLog
      }
    }()
    for i := 0; i < testLogCount / (testBatchSize / len(testLine)); i++ {
      testLogBatch := <-testLogBatchQueue
      assert.Equal(t, testBatchSize / len(testLine), len(testLogBatch),
        "should have correct number of logs in a batch")
      assert.Equal(t, testLine, testLogBatch[0].line, "should have received the correct log")
    }
    assert.Equal(t, 0, len(testLogBatchQueue), "should have emptied out the batch queue")
  })
}

func TestHandleBatchedLogs(t *testing.T) {
  testSumoLog := &sumoLog{
    source: testSource,
    line: testLine,
    isPartial: testIsPartial,
  }
  testLogBatch := []*sumoLog{testSumoLog}

  t.Run("status=OK", func (t *testing.T) {
    testLogBatchQueue := make(chan []*sumoLog, 4000)
    defer close(testLogBatchQueue)
    testClient := NewMockHttpClient(http.StatusOK)
    testSumoLogger := &sumoLogger{
      httpSourceUrl: testHttpSourceUrl,
      httpClient: testClient,
      logBatchQueue: testLogBatchQueue,
    }
    go testSumoLogger.handleBatchedLogs()
    testLogBatchQueue <- testLogBatch
    <-testClient.requestReceivedSignal
    assert.Equal(t, 0, len(testLogBatchQueue))
    assert.Equal(t, 1, testClient.requestCount)
  })
}

func TestSendLogs(t *testing.T) {
  testLogBatchQueue := make(chan []*sumoLog, 4000)

  t.Run("logCount=1, status=OK", func(t *testing.T) {
    var testLogs []*sumoLog
    testLog := &sumoLog{
      source: testSource,
      line: testLine,
      isPartial: testIsPartial,
    }
    testLogs = append(testLogs, testLog)

    testClient := NewMockHttpClient(http.StatusOK)
    testSumoLogger := &sumoLogger{
      httpSourceUrl: testHttpSourceUrl,
      httpClient: testClient,
      logBatchQueue: testLogBatchQueue,
      sendingInterval: defaultSendingIntervalMs,
      batchSize: defaultBatchSizeBytes,
    }

    err := testSumoLogger.sendLogs(testLogs)
    assert.Nil(t, err, "should be no errors sending logs")
    assert.Equal(t, 1, testClient.requestCount, "should have received one request")
  })

  t.Run("logCount=100000, status=OK", func(t *testing.T) {
    logCount := 100000
    var testLogs []*sumoLog
    testLog := &sumoLog{
      source: testSource,
      line: testLine,
      isPartial: testIsPartial,
    }
    for i := 0; i < logCount; i++ {
      testLogs = append(testLogs, testLog)
    }

    testClient := NewMockHttpClient(http.StatusOK)
    testSumoLogger := &sumoLogger{
      httpSourceUrl: testHttpSourceUrl,
      httpClient: testClient,
      logBatchQueue: testLogBatchQueue,
      sendingInterval: defaultSendingIntervalMs,
      batchSize: defaultBatchSizeBytes,
    }

    err := testSumoLogger.sendLogs(testLogs)
    assert.Nil(t, err, "should be no errors sending logs")
    assert.Equal(t, 1, testClient.requestCount, "should have received one request, logs are batched")
  })

  t.Run("logCount=1, status=BadRequest", func(t *testing.T) {
    var testLogs []*sumoLog
    testLog := &sumoLog{
      source: testSource,
      line: testLine,
      isPartial: testIsPartial,
    }
    testLogs = append(testLogs, testLog)

    testClient := NewMockHttpClient(http.StatusBadRequest)
    testSumoLogger := &sumoLogger{
      httpSourceUrl: testHttpSourceUrl,
      httpClient: testClient,
      logBatchQueue: testLogBatchQueue,
      sendingInterval: defaultSendingIntervalMs,
      batchSize: defaultBatchSizeBytes,
    }

    err := testSumoLogger.sendLogs(testLogs)
    assert.NotNil(t, err, "should be an error sending logs")
    assert.Equal(t, 1, testClient.requestCount, "should have received one request, logs are batched")
  })

  t.Run("logCount=100000, status=BadRequest", func(t *testing.T) {
    logCount := 100000
    var testLogs []*sumoLog
    testLog := &sumoLog{
      source: testSource,
      line: testLine,
      isPartial: testIsPartial,
    }
    for i := 0; i < logCount; i++ {
      testLogs = append(testLogs, testLog)
    }

    testClient := NewMockHttpClient(http.StatusBadRequest)
    testSumoLogger := &sumoLogger{
      httpSourceUrl: testHttpSourceUrl,
      httpClient: testClient,
      logBatchQueue: testLogBatchQueue,
      sendingInterval: defaultSendingIntervalMs,
      batchSize: defaultBatchSizeBytes,
    }

    err := testSumoLogger.sendLogs(testLogs)
    assert.NotNil(t, err, "should be an error sending logs")
    assert.Equal(t, 1, testClient.requestCount, "should have received one request, logs are batched")
  })
}

func TestWriteMessage(t *testing.T) {
  testSumoLogger := &sumoLogger{
    httpSourceUrl: testHttpSourceUrl,
  }
  var testLogs []*sumoLog
  testLog := &sumoLog{
    source: testSource,
    line: testLine,
    isPartial: testIsPartial,
  }
  var testLogsBatch bytes.Buffer

  err := testSumoLogger.writeMessage(&testLogsBatch, testLogs)
  assert.Nil(t, err, "should be no error when writing no logs")
  assert.Equal(t, 0, testLogsBatch.Len(), "nothing should be written to the writer")

  logCount := 100000
  for i := 0; i < logCount; i++ {
    testLogs = append(testLogs, testLog)
  }

  err = testSumoLogger.writeMessage(&testLogsBatch, testLogs)
  assert.Nil(t, err, "should be no error when writing logs")
  assert.Equal(t, logCount * (len(testLog.line) + len([]byte("\n"))), testLogsBatch.Len(), "all logs should be written to the writer")
}
