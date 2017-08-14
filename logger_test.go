package main

import (
  "bytes"
  "compress/gzip"
  "context"
  "io/ioutil"
  "math"
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
  m.requestReceivedSignal = make(chan bool, defaultQueueSize)
}

func NewMockHttpClient(statusCode int) *mockHttpClient {
  return &mockHttpClient{
    requestCount: 0,
    statusCode: statusCode,
    requestReceivedSignal: make(chan bool, defaultQueueSize),
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
    logQueue: make(chan *sumoLog, defaultQueueSize),
    logBatchQueue: make(chan []*sumoLog, defaultQueueSize),
    sendingInterval: time.Second,
  }

  go testSumoLogger.consumeLogsFromFile()

  enc := logdriver.NewLogEntryEncoder(inputFile)

  t.Run("Consume one log", func(t *testing.T) {
    enc.Encode(testLogMessage)
    consumedLog := <-testSumoLogger.logQueue
    assert.Equal(t, testSource, consumedLog.source, "should read the correct log source")
    assert.Equal(t, testLine, consumedLog.line, "should read the correct log line")
    assert.Equal(t, testIsPartial, consumedLog.isPartial, "should read the correct log partial")
  })

  t.Run("Consume many logs", func(t *testing.T) {
    testLogsCount := 100
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

func TestBatchLogs(t *testing.T) {
  testSumoLog := &sumoLog{
    source: testSource,
    line: testLine,
    isPartial: testIsPartial,
  }

  t.Run("batchSize=1 byte", func(t *testing.T) {
    testLogQueue := make(chan *sumoLog, 10 * defaultQueueSize)
    testLogBatchQueue := make(chan []*sumoLog, defaultQueueSize)
    testSumoLogger := &sumoLogger{
      httpSourceUrl: testHttpSourceUrl,
      logQueue: testLogQueue,
      logBatchQueue: testLogBatchQueue,
      sendingInterval: 2 * time.Second,
      batchSize: 1,
    }
    go testSumoLogger.batchLogs()

    testLogQueue <- testSumoLog
    testLogBatch := <-testLogBatchQueue
    assert.Equal(t, 1, len(testLogBatch), "should have received one batch from single log")
    assert.Equal(t, testLine, testLogBatch[0].line, "should have received the correct log")

    testLogCount := 1000
    for i := 0; i < testLogCount; i++ {
      testLogQueue <- testSumoLog
    }
    for i := 0; i < testLogCount; i++ {
      <-testLogBatchQueue
    }
    assert.Equal(t, 0, len(testLogBatchQueue), "should have emptied out the batch queue")
  })

  t.Run("batchSize=10 bytes", func(t *testing.T) {
    testBatchSize := 10
    testLogQueue := make(chan *sumoLog, 10 * defaultQueueSize)
    testLogBatchQueue := make(chan []*sumoLog, defaultQueueSize)
    testSumoLogger := &sumoLogger{
      httpSourceUrl: testHttpSourceUrl,
      logQueue: testLogQueue,
      logBatchQueue: testLogBatchQueue,
      sendingInterval: time.Hour,
      batchSize: testBatchSize,
    }
    go testSumoLogger.batchLogs()

    testLogQueue <- testSumoLog
    testLogBatch := <-testLogBatchQueue
    assert.Equal(t, 1, len(testLogBatch), "should have received one batch from single log")
    assert.Equal(t, testLine, testLogBatch[0].line, "should have received the correct log")

    testLogCount := 1000
    for i := 0; i < testLogCount; i++ {
      testLogQueue <- testSumoLog
    }
    for i := 0; i < testLogCount; i++ {
      <-testLogBatchQueue
    }
    assert.Equal(t, 0, len(testLogBatchQueue), "should have emptied out the batch queue")
  })

  t.Run("batchSize=1800 bytes", func(t *testing.T) {
    testBatchSize := 1800
    testLogQueue := make(chan *sumoLog, 10 * defaultQueueSize)
    testLogBatchQueue := make(chan []*sumoLog, defaultQueueSize)
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
    assert.Equal(t, 1, len(testLogBatch), "should have received one batch from single log (expect timer to tick)")
    assert.Equal(t, testLine, testLogBatch[0].line, "should have received the correct log")

    testLogCount := 1000
    for i := 0; i < testLogCount; i++ {
      testLogQueue <- testSumoLog
    }
    for i := 0; i < len(testLine) * testLogCount / testBatchSize ; i++ {
      <-testLogBatchQueue
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

  t.Run("status=BadRequest", func (t *testing.T) {
    testLogBatchQueue := make(chan []*sumoLog, 4000)
    defer close(testLogBatchQueue)
    testClient := NewMockHttpClient(http.StatusBadRequest)
    testSumoLogger := &sumoLogger{
      httpSourceUrl: testHttpSourceUrl,
      httpClient: testClient,
      logBatchQueue: testLogBatchQueue,
    }
    go testSumoLogger.handleBatchedLogs()
    testLogBatchQueue <- testLogBatch
    testRetryCount := 3
    testElapsedTime := initialRetryInterval * time.Duration(math.Pow(retryMultiplier, float64(testRetryCount)))
    time.Sleep(testElapsedTime)
    for i := 0; i < testRetryCount + 1; i++ {
      <-testClient.requestReceivedSignal
    }
    assert.Equal(t, 0, len(testLogBatchQueue))
    assert.Equal(t, testRetryCount + 1, testClient.requestCount)
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
      sendingInterval: defaultSendingInterval,
      batchSize: defaultBatchSize,
    }

    err := testSumoLogger.sendLogs(testLogs)
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
      logBatchQueue: testLogBatchQueue,
      sendingInterval: defaultSendingInterval,
      batchSize: defaultBatchSize,
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
      sendingInterval: defaultSendingInterval,
      batchSize: defaultBatchSize,
    }

    err := testSumoLogger.sendLogs(testLogs)
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
      logBatchQueue: testLogBatchQueue,
      sendingInterval: defaultSendingInterval,
      batchSize: defaultBatchSize,
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
