# Overview

Docker released Enterprise Edition 2.0. We have successfully re-certified our Sumo Logging Driver Plugin against this major release.

For more details about the certification process of our docker plugin, please refer to the guide: https://docs.docker.com/docker-store/certify-plugins-logging/

Docker provides tool `inspectDockerLoggingPlugin` to certify the content for publication on Docker Store by ensuring that your Docker logging plugins conform to best practices.

# Set up testing environments

Before using `inspectDockerLoggingPlugin` tool, environment should be set up firstly, including:
1. install `git`

2. configure credentials

3. configure endpoints

# Downloads

## inspectDockerLoggingPlugin
`inspectDockerLoggingPlugin` could be downloaded directly from links:

`OS/Architecture` | Download Link
--- | ---
`Windows/X86`	| https://s3.amazonaws.com/store-logos-us-east-1/certification/windows/inspectDockerLoggingPlugin.exe
`Linux/X86`	| https://s3.amazonaws.com/store-logos-us-east-1/certification/linux/inspectDockerLoggingPlugin
`Linux/IBMZ` | https://s3.amazonaws.com/store-logos-us-east-1/certification/zlinux/inspectDockerLoggingPlugin
`Linux/IBMPOWER` | https://s3.amazonaws.com/store-logos-us-east-1/certification/power/inspectDockerLoggingPlugin

Set permissions on `inspectDockerLoggingPlugin` so that it is executable:
```
chmod u+x inspectDockerLoggingPlugin
```

## HTTP Mock Server
Docker provides the sample `http_api_endpoint` server to configure logging plugin to send log data to it. This sample server emulates a HTTP Endpoint. It supports `POST` and `PUT` to log data, `GET` to retrieve data, and `DELETE` to delete data. More details could be found on this page: https://docs.docker.com/docker-store/certify-plugins-logging/.

The Linux binary of this server could be downloaded directly from this repo.

Set permissions on `http_api_endpoint` so that it is executable:
```
chmod u+x http_api_endpoint
```

If you have more questions about how to use this server, you could use its help menu by:
```
./http_api_endpoint --help
```

The logging plugin would use `http://127.0.0.1:80` by running following command:
```
./http_api_endpoint &
```
Then you could use `GET` to retrieve logs:
```
curl -s -X GET http://127.0,0,1:80
```

## Inspect file
Download `quotes.txt` file: https://s3.amazonaws.com/store-logos-us-east-1/certification/quotes.txt.

Put the file in the same directory with `inspectDockerLoggingPlugin`.

## Script files
`./inspectDockerLoggingPlugin` provides the option of `-test-script <FILE_NAME>`, by which an optional custom script is used to test the Docker Logging plugin. For the custom script, please download file `run_sumologic_driver.sh`.

`./inspectDockerLoggingPlugin` provides another option `-get-logs-script <FILE_NAME>`, by which an optional custom script is used to retrieve logs. For the custom script, please download file `log_sumologic_driver.sh`

# Certification

Inspect a docker logging plugin with messages sent to standard output.

With the following command, you could go through all options supported by this tool:
```
./inspectDockerLoggingPlugin -help
```
## Command

The command to run `inspectDockerLoggingPlugin`:
```
sudo ./inspectDockerLoggingPlugin -json -test-script ./run_sumologic_driver.sh -get-logs-script ./log_sumologic_driver.sh -product-id=<PRODUCT_ID> store/sumologic/docker-logging-driver:<VERSION>
```
