# Guide for developers
This project is a plugin for the docker engine, which delivers logs to Sumo Logic by pushing log messages through an HTTP source.
This is the guide for developers who want build and extend the plugin. If you just want use this plugin in your docker environment, please refer to the [readme file](README.md).

## Prerequisite
  * [Download](https://www.docker.com/get-docker) and install latest docker engine
  * [Download](https://golang.org/dl/) and install latest Go language distribution
  * Clone/Download this repository to a local directory, and
  * Get all dependencies with 
  ```bash
  $ go get -d ./...
  ```

## Build and install plugin to docker
In bash, run:
```bash
$ sudo bash ./plugin_install.sh
```
If everything goes fine, you can verify that the plugin is correctly installed with:
```bash
$ docker plugin ls

ID                  NAME                                     DESCRIPTION                       ENABLED
b72ceb1530ff        sumologic/docker-logging-driver:latest   Sumo Logic logging driver         true
```

## Uninstall and cleanup the plugin
In bash, run:
```bash
$ sudo bash ./plugin_uninstall.sh
```

## Run sanity test
  * Make sure the plugin is installed and enabled
  * In bash, run:
```bash
$ docker run --log-driver=sumologic/docker-logging-driver --log-opt sumo-url=<url> -i -t ubuntu bash
```
This will create a bash session in a docker container and send all console contents to a Sumo Logic HTTP source as log lines

## Run unit test
The unit test is written in `XXX_test.go` which `XXX` is the module to be tested. You can launch all unit tests with:
```bash
$ go test -v
```
The unit test do not require docker environment to run. For details about unit test or test framework in Go language, click [here](https://golang.org/pkg/testing/).
