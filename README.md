| TLS Deprecation Notice |
| --- |
| In keeping with industry standard security best practices, as of May 31, 2018, the Sumo Logic service will only support TLS version 1.2 going forward. Verify that all connections to Sumo Logic endpoints are made from software that supports TLS 1.2. |

- [Overview](#overview)
- [Set up Sumo logging driver plugin](#set-up-sumo-logging-driver-plugin)
  * [Step 1 Configure Sumo to receive Docker logs](#step-1-configure-sumo-to-receive-docker-logs)
  * [Step 2 Install Plugin](#step-2-install-plugin)
  * [Step 3 Configure Docker to use the plugin](#step-3-configure-docker-to-use-the-plugin)
    + [Option A Start a container to use Sumo driver](#option-a-start-a-container-to-use-sumo-driver)
    + [Option B Configure all containers on Docker host to use Sumo driver](#option-b-configure-all-containers-on-docker-host-to-use-sumo-driver)
  * [Step 4 Search and analyze container log data](#step-4-search-and-analyze-container-log-data)
- [log-opt options](#log-opt-options)
- [Uninstall the plugin](#uninstall-the-plugin)

# Overview 

Docker logging driver plugins extend Docker's logging capabilities. You can use the Sumo logging driver plugin to send Docker container logs to the Sumo cloud-based service. Once your log data is in Sumo, you can use the Sumo web app to search and analyze your log data.

**Note:** Docker plugins are not yet supported on Windows; see Docker's logging driver plugin [documentation](https://github.com/docker/cli/blob/master/docs/extend/plugins_logging.md). 

The Sumo logging plugin driver is supported by Sumo Logic. If you have issues or questions, create an issue on GitHub.

# Set up Sumo logging driver plugin

Setting up the Sumo plugin involves setting up an HTTP endpoint on Sumo to receive Docker container log data, and configuring Docker to use the plugin.  

## Step 1 Configure Sumo to receive Docker logs

In this step you create, on the Sumo service, an HTTP endpoint to receive your Docker logs. This process involves creating an HTTP source on a hosted collector in Sumo. In Sumo, collectors use sources to receive data.

1. If you don’t already have a Sumo account, you can create one by clicking the **Free Trial** button on https://www.sumologic.com/.

2. Create a hosted collector, following the instructions on [Configure a Hosted Collector](https://help.sumologic.com/Send-Data/Hosted-Collectors/Configure-a-Hosted-Collector) in Sumo help. (If you already have a Sumo hosted collector that you want to use, skip this step.)  

3. Create an HTTP source on the collector you created in the previous step. For instructions, see [HTTP Logs and Metrics Source](https://help.sumologic.com/Send-Data/Sources/02Sources-for-Hosted-Collectors/HTTP-Source) in Sumo help. 

4. When you have configured the HTTP source, Sumo will display the URL of the HTTP endpoint. **Make a note of the URL.** You will use it when you configure Docker to send data to Sumo. 

## Step 2 Install Plugin

On each Docker host with containers from which you want to collect container logs, install the plugin by running the following command in a terminal window:
* `$ docker plugin install store/sumologic/docker-logging-driver:<ver> --alias sumologic --grant-all-permissions`

**NOTE** The `--alias` is required for using it on AWS ECS

To verify that the plugin is installed and enabled, run the following command:

```bash
$ docker plugin ls

ID                  NAME              DESCRIPTION                       ENABLED
b72ceb1530ff        sumologic         Sumo Logic logging driver         true
```

## Step 3 Configure Docker to use the plugin

The Docker daemon on each Docker host has a default logging driver; each container on the Docker host uses the default driver, unless you configure it to use a different logging driver. 

To use the Sumo  plugin, you need to configure one or more containers to use the plugin. Use [Option A](option-a-start-a-container-to-use-sumo-driver) below to use the sumologic plugin on a single container. Use [Option B](option-b-configure-all-containers-on-docker-host-to-use-sumo-driver) to set up all containers on a host to use the plugin.

### Option A Start a container to use Sumo driver

To run a specific container with the logging driver:
* Use the `--log-driver` flag to specify the plugin. 
* Use the `--log-opt` flag to specify the URL for the HTTP source you created in Step 1. 

For example:

`$ docker run --log-driver=sumologic --log-opt sumo-url=sumo_source_url`

where `sumo-source-url` is the URL that Sumo assigned to the HTTP source you created. 


The following command starts the container whose name is `your_container` to use the Sumo plugin, specifies the URL for the HTTP source, and sets several optional `--log-opts` options. For more information about these and other options, see [log-opt options](log--opt-options) below.  

```
$ docker run --log-driver=sumologic \
    --log-opt sumo-url=sumo-source-url \
    --log-opt sumo-batch-size=2000000 \
    --log-opt sumo-queue-size=400 \
    --log-opt sumo-sending-frequency=500ms \
    --log-opt sumo-compress=false \
    --log-opt ... \
    your_container
```    
    
where:

* `sumo_sourceurl` is the URL of your HTTP Source.
* `your_container` identifies a container.

The container should start sending logs to Sumo Logic.

### Option B Configure all containers on Docker host to use Sumo driver

The Docker daemon for a Docker host has a default logging driver, which each container on the host uses unless you configure it to use a different logging driver. This procedure shows you how to update a Docker host’s `daemon.json `file so that all of the containers on the host use the Sumo plugin, and know the URL for for sending logs to the Sumo service.

For more information about configuring Docker using `daemon.json`, see [Daemon Configuration File](https://docs.docker.com/engine/reference/commandline/dockerd/#daemon-configuration-file) in Docker help.

1. Find the Docker host’s `daemon.json` file, located by default in `/etc/docker` on Linux hosts.

2. To set the Sumo as the default logging driver for a Docker host, set the `log-driver` key to “sumologic”. For an example, see the `daemon.json` excerpt below this procedure.

3. To specify the URL for sending logs to Sumo, use the `log-opts` key to set `sumo-url` to the URL of the HTTP source you created in Step 1. For an example, see the `daemon.json` excerpt below this procedure.

4. Specify any other desired log options. For supported options, see [log-opt options](log--opt-options) below.

5. Restart Docker for the changes to take effect. 

Example excerpt from `daemon.json`

```
{
  "log-driver": "sumologic",
  "log-opts": {
    "sumo-url": "https://<deployment>.sumologic.com  		\
     /receiver/v1/http/<source_token>"
  }
}
```
## Step 4 Search and analyze container log data

Once your container or containers are set up to send logs to Sumo, you can log onto the Sumo web app and start searching and analyzing the data. For help in getting started see [Search](https://help.sumologic.com/Search) in Sumo help.

# log-opt options
To specify additional logging driver options, you can use the `--log-opt NAME=VALUE` flag.

| Option                    | Required? | Default Value        | Description
| ------------------------- | :-------: | :------------------: | -------------------------------------- |
| `sumo-url`                  | Yes       |                      | HTTP Source URL
| `sumo-source-category`      | No        | HTTP source category | Source category to appear when searching in Sumo Logic by `_sourceCategory`. Use `{{Tag}}` as the placeholder for the `tag` option. If not specified, the source category of the HTTP source will be used.
| `sumo-source-name`          | No        | container's name     | Source name to appear when searching in Sumo Logic by `_sourceName`. Use `{{Tag}}`as the placeholder for the `tag` option.  If not specified, it will be the container's name.
| `sumo-source-host`          | No        | host name            | Source host to appear when searching in Sumo Logic by `_sourceHost`. Use `{{Tag}}`as the placeholder for the `tag` option. If not specified, it will be the machine host name.
| `sumo-compress`             | No        | `true`               | Enable/disable gzip compression. Boolean.
| `sumo-compress-level`       | No        | `-1`                 | Set the gzip compression level. Valid values are -1 (default), 0 (no compression), 1 (best speed) ... 9 (best compression).
| `sumo-batch-size`           | No        | `1000000`            | The number of bytes of logs the driver should wait for before sending them in bulk. If the number of bytes never reaches `sumo-batch-size`, the driver will send the logs in smaller batches at predefined intervals; see `sumo-sending-interval`.
| `sumo-sending-interval`     | No        | `2s`                 | The maximum time the driver waits for number of logs to reach `sumo-batch-size` before sending the logs, even if the number of logs is less than the batch size. In the format 72h3m5s, valid time units are "ns", "us" (or "µs"), "ms", "s", "m", and "h".
| `sumo-proxy-url`            | No        |                      | Set a proxy URL.
| `sumo-insecure-skip-verify` | No        | `false`              | Ignore server certificate validation. Boolean.
| `sumo-root-ca-path`         | No        |                      | Set the path to a custom root certificate.
| `sumo-server-name`          | No        |                      | Name used to validate the server certificate. By default, uses hostname of the `sumo-url`.
| `sumo-queue-size`           | No        | `100`                | The maximum number of log batches of size `sumo-batch-size` we can store in memory in the event of network failure, before we begin dropping batches. Thus in the worst case, the plugin will use `sumo-batch-size` * `sumo-queue-size` bytes of memory per container (default 100 MB).
| `tag`                       | No        | `{{.ID}}`            | Specifies a tag for messages, which can be used in the "source category", "source name", and "source host" fields. Certain tokens of the form {{X}} are supported. Default value is `{{.ID}}`, the first 12 characters of the container ID. For more information and a list of supported tokens, see [Log tags for logging driver](https://docs.docker.com/engine/admin/logging/log_tags/) in Docker help. 


# Uninstall the plugin
To cleanly disable and remove the plugin, run:

```
$ docker plugin disable sumologic/docker-logging-driver
$ docker plugin rm sumologic/docker-logging-driver
```
