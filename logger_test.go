package main

import (
  "bytes"
  "compress/gzip"
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
    Partial: testIsPartial,
  }

  inputQueueFile, err := fifo.OpenFifo(context.Background(), filePath, unix.O_RDWR|unix.O_CREAT|unix.O_NONBLOCK, fileMode)
  defer os.Remove(filePath)
  assert.Nil(t, err)

  testSumoLogger := &sumoLogger{
    httpSourceUrl: testHttpSourceUrl,
    httpClient: NewMockHttpClient(http.StatusOK),
    inputQueueFile: inputQueueFile,
    logQueue: make(chan *sumoLog, 4000),
    sendingInterval: 5 * time.Second,
  }

  go testSumoLogger.consumeLogsFromFile()

  enc := logdriver.NewLogEntryEncoder(inputQueueFile)

  t.Run("Consume one log", func(t *testing.T) {
    enc.Encode(testLogMessage)
    consumedLog := <-testSumoLogger.logQueue
    assert.Equal(t, testSource, consumedLog.source, "should read the correct log source")
    assert.Equal(t, testLine, consumedLog.line, "should read the correct log line")
    assert.Equal(t, testIsPartial, consumedLog.isPartial, "should read the correct log partial")
  })

  t.Run("Consume many logs", func(t *testing.T) {
    testLogsCount := 1000
    for i := 0; i < testLogsCount; i++ {
      enc.Encode(testLogMessage)
    }
    for i := 0; i < testLogsCount; i++ {
      consumedLog := <-testSumoLogger.logQueue
      assert.Equal(t, testSource, consumedLog.source, "should read the correct log source")
      assert.Equal(t, testLine, consumedLog.line, "should read the correct log line")
      assert.Equal(t, testIsPartial, consumedLog.isPartial, "should read the correct log partial")
    }
  })
}

func TestQueueLogsForSending(t *testing.T) {
  testSumoLog := &sumoLog{
    source: testSource,
    line: testLine,
    isPartial: testIsPartial,
  }

  t.Run("batchSize=1", func(t *testing.T) {
    testLogQueue := make(chan *sumoLog, 4000)
    testClient := NewMockHttpClient(http.StatusOK)
    testSumoLogger := &sumoLogger{
      httpSourceUrl: testHttpSourceUrl,
      httpClient: testClient,
      logQueue: testLogQueue,
      sendingInterval: 2 * time.Second,
      batchSize: 1,
    }
    go testSumoLogger.bufferLogsForSending()

    testLogQueue <- testSumoLog
    time.Sleep(time.Second)
    assert.Equal(t, 1, testClient.requestCount, "should have received one request")
    testClient.Reset()

    testLogCount := 1000
    for i := 0; i < testLogCount; i++ {
      testLogQueue <- testSumoLog
    }
    time.Sleep(time.Second)
    assert.Equal(t, testLogCount, testClient.requestCount, "should have received %v requests", testLogCount)
    testClient.Reset()
  })

  t.Run("batchSize=10", func(t *testing.T) {
    testLogQueue := make(chan *sumoLog, 4000)
    testClient := NewMockHttpClient(http.StatusOK)
    testBatchSize := 10
    testSumoLogger := &sumoLogger{
      httpSourceUrl: testHttpSourceUrl,
      httpClient: testClient,
      logQueue: testLogQueue,
      sendingInterval: 2 * time.Second,
      batchSize: testBatchSize,
    }
    go testSumoLogger.bufferLogsForSending()

    testLogQueue <- testSumoLog
    time.Sleep(time.Second)
    assert.Equal(t, 0, testClient.requestCount,
      "should have received no requests, fewer messages than batch size (timer should not have clicked in time)")

    testLogCount := 1000
    for i := 0; i < testLogCount; i++ {
      testLogQueue <- testSumoLog
    }
    time.Sleep(2 * time.Second)
    assert.Equal(t, testLogCount / testBatchSize + 1, testClient.requestCount,
      "should have received (number of logs / batch size) requests, " +
      "plus one for the single message we sent previously (timer should have clicked by now)")
    testClient.Reset()
  })

  t.Run("batchSize=1000", func(t *testing.T) {
    testLogQueue := make(chan *sumoLog, 4000)
    testClient := NewMockHttpClient(http.StatusOK)
    testBatchSize := 1000
    testSumoLogger := &sumoLogger{
      httpSourceUrl: testHttpSourceUrl,
      httpClient: testClient,
      logQueue: testLogQueue,
      sendingInterval: 2 * time.Second,
      batchSize: testBatchSize,
    }
    go testSumoLogger.bufferLogsForSending()

    testLogQueue <- testSumoLog
    time.Sleep(time.Second)
    assert.Equal(t, 0, testClient.requestCount,
      "should have received no requests, fewer messages than batch size (timer should not have clicked in time)")

    testLogCount := 1000
    for i := 0; i < testLogCount; i++ {
      testLogQueue <- testSumoLog
    }
    time.Sleep(2 * time.Second)
    assert.Equal(t, testLogCount / testBatchSize + 1, testClient.requestCount,
      "should have received (number of logs / batch size) requests, " +
      "plus one for the single message we sent previously (timer should have clicked by now)")
    testClient.Reset()
  })
}

func TestSendLogs(t *testing.T) {
  testLogQueue := make(chan *sumoLog, 4000)
  var testLogs []*sumoLog
  testClient := NewMockHttpClient(http.StatusOK)
  testSumoLogger := &sumoLogger{
    httpSourceUrl: testHttpSourceUrl,
    httpClient: testClient,
    logQueue: testLogQueue,
    sendingInterval: defaultSendingInterval,
    batchSize: 10,
  }

  testLog := &sumoLog{
    source: testSource,
    line: testLine,
    isPartial: testIsPartial,
  }

  t.Run("logCount=0", func(t *testing.T) {
    testLogs = testSumoLogger.sendLogs(testLogs)
    assert.Equal(t, 0, len(testLogs), "should have no failed logs")
    testClient.Reset()
  })

  t.Run("logCount=1", func(t *testing.T) {
    testLogs = append(testLogs, testLog)
    testLogs = testSumoLogger.sendLogs(testLogs)
    assert.Equal(t, 0, len(testLogs), "should have no failed logs")
    assert.Equal(t, 1, testClient.requestCount, "should have received one request")
    testClient.Reset()
  })

  t.Run("logCount=100", func(t *testing.T) {
    testLogsCount := 100
    for i := 0; i < testLogsCount; i++ {
      testLogs = append(testLogs, testLog)
    }
    testLogs = testSumoLogger.sendLogs(testLogs)
    assert.Equal(t, 0, len(testLogs), "should have no failed logs")
    assert.Equal(t, testLogsCount / testSumoLogger.batchSize, testClient.requestCount, "should have receieved one request per batch")
    testClient.Reset()
  })
}

func TestMakePostRequest(t *testing.T) {
  testLogQueue := make(chan *sumoLog, 4000)

  t.Run("logCount=0, status=OK", func(t *testing.T) {
    var testLogs []*sumoLog

    testClient := NewMockHttpClient(http.StatusOK)
    testSumoLogger := &sumoLogger{
      httpSourceUrl: testHttpSourceUrl,
      httpClient: testClient,
      logQueue: testLogQueue,
      sendingInterval: defaultSendingInterval,
      batchSize: defaultBatchSize,
    }

    err := testSumoLogger.makePostRequest(testLogs)
    assert.Nil(t, err, "should be no errors sending logs")
    assert.Equal(t, 0, testClient.requestCount, "should have received no requests")
  })

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
      logQueue: testLogQueue,
      sendingInterval: defaultSendingInterval,
      batchSize: defaultBatchSize,
    }

    err := testSumoLogger.makePostRequest(testLogs)
    assert.Nil(t, err, "should be no errors sending logs")
    assert.Equal(t, 1, testClient.requestCount, "should have received one request")
  })

  t.Run("logCount=1000, status=OK", func(t *testing.T) {
    logCount := 1000
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
      logQueue: testLogQueue,
      sendingInterval: defaultSendingInterval,
      batchSize: defaultBatchSize,
    }

    err := testSumoLogger.makePostRequest(testLogs)
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
      logQueue: testLogQueue,
      sendingInterval: defaultSendingInterval,
      batchSize: defaultBatchSize,
    }

    err := testSumoLogger.makePostRequest(testLogs)
    assert.NotNil(t, err, "should be an error sending logs")
    assert.Equal(t, 1, testClient.requestCount, "should have received one request, logs are batched")
  })

  t.Run("logCount=1000, status=BadRequest", func(t *testing.T) {
    logCount := 1000
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
      logQueue: testLogQueue,
      sendingInterval: defaultSendingInterval,
      batchSize: defaultBatchSize,
    }

    err := testSumoLogger.makePostRequest(testLogs)
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

  logCount := 100
  for i := 0; i < logCount; i++ {
    testLogs = append(testLogs, testLog)
  }

  err = testSumoLogger.writeMessage(&testLogsBatch, testLogs)
  assert.Nil(t, err, "should be no error when writing logs")
  assert.Equal(t, logCount * (len(testLog.line) + len([]byte("\n"))), testLogsBatch.Len(), "all logs should be written to the writer")
}

func TestWriteMessageGzipCompression(t *testing.T) {
  testSumoLogger := &sumoLogger{
    httpSourceUrl: testHttpSourceUrl,
    gzipCompression: true,
    gzipCompressionLevel: defaultGzipCompressionLevel,
  }
  var testLogs []*sumoLog
  testLog := &sumoLog{
    source: testSource,
    line: testLine,
    isPartial: testIsPartial,
  }
  var testLogsBatch bytes.Buffer

  err := testSumoLogger.writeMessageGzipCompression(&testLogsBatch, testLogs)
  assert.Nil(t, err, "should be no error when writing no logs")

  verifyGzipReader, _ := gzip.NewReader(&testLogsBatch)
  testDecompressedLogs, _ := ioutil.ReadAll(verifyGzipReader)
  assert.Equal(t, 0, len(testDecompressedLogs), "nothing should be written to the writer")
  verifyGzipReader.Close()

  logCount := 100
  for i := 0; i < logCount; i++ {
    testLogs = append(testLogs, testLog)
  }
  err = testSumoLogger.writeMessageGzipCompression(&testLogsBatch, testLogs)
  assert.Nil(t, err, "should be no error when writing logs")

  verifyGzipReader, _ = gzip.NewReader(&testLogsBatch)
  testDecompressedLogs, _ = ioutil.ReadAll(verifyGzipReader)

  assert.Equal(t, logCount * (len(testLog.line) + len([]byte("\n"))), len(testDecompressedLogs), "all logs should be written to the writer")
  verifyGzipReader.Close()
}
