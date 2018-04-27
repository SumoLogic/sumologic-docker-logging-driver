package main

import (
  "fmt"
  "encoding/json"
  "net/http"
  "io"

  "github.com/docker/docker/daemon/logger"
  "github.com/docker/docker/pkg/ioutils"
  "github.com/docker/go-plugins-helpers/sdk"
)

const (
  // if you change the name here, don't forget to change it in config.json
  pluginName = "sumologic"
  startLoggingPath = "/LogDriver.StartLogging"
  stopLoggingPath = "/LogDriver.StopLogging"
  readLogsPath = "/LogDriver.ReadLogs"
  capabilitiesPath = "/LogDriver.Capabilities"
)

func main() {
  pluginHandler := sdk.NewHandler(`{"Implements": ["LoggingDriver"]}`)

  sumoDriver := newSumoDriver()
  initHandlers(&pluginHandler, sumoDriver)
  if err := pluginHandler.ServeUnix(pluginName, 0); err != nil {
    panic(err)
  }
}

func initHandlers(pluginHandler *sdk.Handler, sumoDriver SumoDriver) {
  pluginHandler.HandleFunc(startLoggingPath, startLoggingHandler(sumoDriver))
  pluginHandler.HandleFunc(stopLoggingPath, stopLoggingHandler(sumoDriver))
}

type StartLoggingRequest struct {
  File string
  Info logger.Info
}

type StopLoggingRequest struct {
  File string
}

type ReadLogsRequest struct {
  Config logger.ReadConfig
  Info logger.Info
}

type CapabilitiesResponse struct {
  Capability logger.Capability
}

type PluginResponse struct {
  Err string
}

func startLoggingHandler(sumoDriver SumoDriver) func(w http.ResponseWriter, r *http.Request) {
  return func(w http.ResponseWriter, r *http.Request) {
    var req StartLoggingRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
      http.Error(w, err.Error(), http.StatusBadRequest)
      return
    }
    if req.Info.ContainerID == "" {
      respond(w, fmt.Errorf("must provide ContainerID in log context"))
      return
    }
    if _, exists := req.Info.Config[logOptUrl]; !exists {
      respond(w, fmt.Errorf("must provide log-opt: %s", logOptUrl))
      return
    }
    err := sumoDriver.StartLogging(req.File, req.Info)
    respond(w, err)
  }
}

func stopLoggingHandler(sumoDriver SumoDriver) func(w http.ResponseWriter, r *http.Request) {
  return func(w http.ResponseWriter, r *http.Request) {
    var req StopLoggingRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
      http.Error(w, err.Error(), http.StatusBadRequest)
      return
    }
    err := sumoDriver.StopLogging(req.File)
    respond(w, err)
  }
}

func readLogsHandler(sumoDriver SumoDriver) func(w http.ResponseWriter, r *http.Request) {
  return func(w http.ResponseWriter, r *http.Request) {
    var req ReadLogsRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
      http.Error(w, err.Error(), http.StatusBadRequest)
      return
    }

    stream, err := sumoDriver.ReadLogs(req.Config, req.Info)
    if err != nil {
      http.Error(w, err.Error(), http.StatusInternalServerError)
      return
    }
    defer stream.Close()

    w.Header().Set("Content-Type", "application/x-json-stream")
    writeFlusher := ioutils.NewWriteFlusher(w)
    io.Copy(writeFlusher, stream)
  }
}

func capabilitesHandler(sumoDriver SumoDriver) func(w http.ResponseWriter, r *http.Request) {
  return func(w http.ResponseWriter, r *http.Request) {
      json.NewEncoder(w).Encode(&CapabilitiesResponse {
        Capability: logger.Capability{ReadLogs: true},
      })
  }
}

func respond(w http.ResponseWriter, err error) {
  var res PluginResponse
  if err != nil {
    res.Err = err.Error()
  }
  json.NewEncoder(w).Encode(&res)
}
