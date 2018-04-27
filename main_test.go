package main

import (
  "bytes"
  "encoding/json"
  "fmt"
  "io"
  "io/ioutil"
  "net/http"
  "net/http/httptest"
  "testing"
  "strings"

  "github.com/docker/docker/daemon/logger"
  "github.com/stretchr/testify/assert"
)

const (
  filePathRequestField = "/tmp/test"
)

type mockSumoDriver struct {
  StartLoggingCallsCount int
  StopLoggingCallsCount int
  ReadLogsCallCount int
}

type nopReadCloser struct {
  io.ReadCloser
}

func (m *mockSumoDriver) StartLogging(file string, info logger.Info) error {
  m.StartLoggingCallsCount += 1
  return nil
}

func (m *mockSumoDriver) StopLogging(file string) error {
  m.StopLoggingCallsCount += 1
  return nil
}


func (m *mockSumoDriver) ReadLogs(config logger.ReadConfig, info logger.Info) (io.ReadCloser, error) {
  m.ReadLogsCallCount += 1
  reader := io.MultiReader(strings.NewReader("This is a line of reader"))
  readCloser := io.ReadCloser{
    Reader: reader,
    Closer: io.Closer(),
  }
  return readCloser, nil
}

func NewMockSumoDriver() *mockSumoDriver {
  return &mockSumoDriver{
    StartLoggingCallsCount: 0,
    StopLoggingCallsCount: 0,
    ReadLogsCallCount: 0,
  }
}

func TestHandlers(t *testing.T) {
  mockSumoDriver := NewMockSumoDriver()

  mockServer := http.NewServeMux()
  mockServer.HandleFunc(startLoggingPath, startLoggingHandler(mockSumoDriver))
  mockServer.HandleFunc(stopLoggingPath, stopLoggingHandler(mockSumoDriver))
  mockServer.HandleFunc(readLogsPath, readLogsHandler(mockSumoDriver))

  t.Run("make StartLogging request with missing ContainerID", func(t *testing.T) {
    defer resetCallsCount(mockSumoDriver)
    req := StartLoggingRequest{
      File: filePathRequestField,
      Info: logger.Info{
        Config: map[string]string{
          logOptUrl: "https://example.org",
        },
      },
    }
    resp, respBody, err := makeRequest(startLoggingPath, req, mockServer)
    if err != nil {
      t.Fatal(err)
    }

    assert.Equal(t, http.StatusOK, resp.StatusCode, "should get a 200 response")
    assert.Equal(t, 0, mockSumoDriver.StartLoggingCallsCount, "should not have called StartLogging on the driver")
    assert.Equal(t, 0, mockSumoDriver.StopLoggingCallsCount, "should not have called StopLogging on the driver")
    assert.Contains(t, respBody.Err, "ContainerID", "error message should mention ContainerID")
  })

  t.Run(fmt.Sprintf("make StartLogging request with missing log-opt: %s", logOptUrl), func(t *testing.T) {
    defer resetCallsCount(mockSumoDriver)
    req := StartLoggingRequest{
      File: filePathRequestField,
      Info: logger.Info{
        Config: map[string]string{},
        ContainerID: "containeriid",
      },
    }
    resp, respBody, err := makeRequest(startLoggingPath, req, mockServer)
    if err != nil {
      t.Fatal(err)
    }

    assert.Equal(t, http.StatusOK, resp.StatusCode, "should get a 200 response")
    assert.Equal(t, 0, mockSumoDriver.StartLoggingCallsCount, "should not have called StartLogging on the driver")
    assert.Equal(t, 0, mockSumoDriver.StopLoggingCallsCount, "should not have called StopLogging on the driver")
    assert.Contains(t, respBody.Err, logOptUrl, "error message should mention %s", logOptUrl)
  })

  t.Run("make StartLogging request with correct config", func(t *testing.T) {
    defer resetCallsCount(mockSumoDriver)
    req := StartLoggingRequest{
      File: filePathRequestField,
      Info: logger.Info{
        Config: map[string]string{
          logOptUrl: "https://example.org",
        },
        ContainerID: "containeriid",
      },
    }
    resp, respBody, err := makeRequest(startLoggingPath, req, mockServer)
    if err != nil {
      t.Fatal(err)
    }

    assert.Equal(t, http.StatusOK, resp.StatusCode, "should get a 200 response")
    assert.Equal(t, 1, mockSumoDriver.StartLoggingCallsCount, "should have called StartLogging on the driver exactly once")
    assert.Equal(t, 0, mockSumoDriver.StopLoggingCallsCount, "should not have called StopLogging on the driver")
    assert.Equal(t, "", respBody.Err, "error message should be empty")
  })

  t.Run("make StopLogging request", func(t *testing.T) {
    defer resetCallsCount(mockSumoDriver)
    req := StopLoggingRequest{
      File: filePathRequestField,
    }
    resp, respBody, err := makeRequest(stopLoggingPath, req, mockServer)
    if err != nil {
      t.Fatal(err)
    }
    assert.Equal(t, http.StatusOK, resp.StatusCode, "should get a 200 response")
    assert.Equal(t, 0, mockSumoDriver.StartLoggingCallsCount, "should not have called StartLogging on the driver")
    assert.Equal(t, 1, mockSumoDriver.StopLoggingCallsCount, "should have called StopLogging on the driver exactly once")
    assert.Equal(t, "", respBody.Err, "error message should be empty")
  })

  t.Run("make ReadLogs request", func(t *testing.T) {
    defer resetCallsCount(mockSumoDriver)
    // start logger request
    startLoggingReq := StartLoggingRequest{
      File: filePathRequestField,
      Info: logger.Info{
        Config: map[string]string{
          logOptUrl: "https://example.org",
        },
        ContainerID: "containeriid",
      },
    }
    _, _, err := makeRequest(startLoggingPath, startLoggingReq, mockServer)
    if err != nil {
      t.Fatal(err)
    }
    // read logs request
    req := ReadLogsRequest{
      Config: logger.ReadConfig{
        Tail: 1,
      },
      Info: logger.Info{
        Config: map[string]string{logOptUrl: "https://example.org"},
        ContainerID: "containerid",
      },
    }
    resp, _, err := makeRequest(readLogsPath, req, mockServer)
    if err != nil {
      t.Fatal(err)
    }
    assert.Equal(t, http.StatusOK, resp.StatusCode, "should get a 200 response")
    assert.Equal(t, 0, mockSumoDriver.StartLoggingCallsCount, "should not have called StartLogging on the driver")
    assert.Equal(t, 0, mockSumoDriver.StopLoggingCallsCount, "should not have called StopLogging on the driver exactly once")
    assert.Equal(t, 1, mockSumoDriver.ReadLogsCallCount, "should have called ReadLogs on the driver exactly once")
  })
}

func resetCallsCount(m *mockSumoDriver) {
  m.StartLoggingCallsCount = 0
  m.StopLoggingCallsCount = 0
  m.ReadLogsCallCount = 0
}

func makeRequest(requestPath string, request interface{}, mockServer *http.ServeMux) (*http.Response, *PluginResponse, error) {
  requestBody, err := json.Marshal(request)
  if err != nil {
    return nil, nil, err
  }
  req, err := http.NewRequest("POST", requestPath, bytes.NewBuffer(requestBody))
  if err != nil {
    return nil, nil, err
  }
  respRecorder := httptest.NewRecorder()
  mockServer.ServeHTTP(respRecorder, req)

  resp := respRecorder.Result()
  body, err := ioutil.ReadAll(resp.Body)
  if err != nil {
    return nil, nil, err
  }
  var respBody PluginResponse
  err = json.Unmarshal(body, &respBody)
  if err != nil {
    return nil, nil, err
  }
  return resp, &respBody, err
}
