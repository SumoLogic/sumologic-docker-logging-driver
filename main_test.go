package main

import (
  "bytes"
  "encoding/json"
  "fmt"
  "io/ioutil"
  "net/http"
  "net/http/httptest"
  "testing"

  "github.com/docker/docker/daemon/logger"
  "github.com/stretchr/testify/assert"
)

const (
  fileString = "/run/docker/logging/b227f9375aee6474f9195fc53710159e7afbc2af872d6287380dfbffb0a2e224"
)

type mockSumoDriver struct {
  StartLoggingCallsCount int
  StopLoggingCallsCount int
}

func (m *mockSumoDriver) StartLogging(file string, info logger.Info) error {
  m.StartLoggingCallsCount += 1
  return nil
}

func (m *mockSumoDriver) StopLogging(file string) error {
  m.StopLoggingCallsCount += 1
  return nil
}

func NewMockSumoDriver() *mockSumoDriver {
  return &mockSumoDriver{
    StartLoggingCallsCount: 0,
    StopLoggingCallsCount: 0,
  }
}

func TestHandlers(t *testing.T) {
  mockSumoDriver := NewMockSumoDriver()

  mockServer := http.NewServeMux()
  mockServer.HandleFunc(startLoggingPath, startLoggingHandler(mockSumoDriver))
  mockServer.HandleFunc(stopLoggingPath, stopLoggingHandler(mockSumoDriver))

  t.Run("make StartLogging request with missing ContainerID", func(t *testing.T) {
    defer resetCallsCount(mockSumoDriver)
    req := StartLoggingRequest{
      File: fileString,
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
      File: fileString,
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
      File: fileString,
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
      File: fileString,
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

}

func resetCallsCount(m *mockSumoDriver) {
  m.StartLoggingCallsCount = 0
  m.StopLoggingCallsCount = 0
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
