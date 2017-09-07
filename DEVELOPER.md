# Guide for developers
This project is a plugin working in docker engine for delivering log to Sumo Logic cloud service through HTTP source.
This is the guide for developers want build and extend the plugin. If you just want use this plugin in your docker environment, please refer to the [readme file](README.md).

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
If everything goes fine, you can verify the plugin correctly installed with:
```bash
$ docker plugin ls
ID                  NAME                DESCRIPTION                 ENABLED
2dcbb3a32956        sumologic:latest    Sumo Logic logging driver   true
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
$ docker run --log-driver=sumologic --log-opt sumo-url=<url> -i -t ubuntu bash
```
This will create a bash session in docker container and send all console contents to Sumo HTTP source as log lines

## Run unit test
The unit test is written in `XXX_test.go` which `XXX` is the module to be tested. You can launch all unite tests with:
```bash
$ go test -v
```
The unit test do not require docker environment to run. For details about unit test or test framework in Go language, click [here](https://golang.org/pkg/testing/).
