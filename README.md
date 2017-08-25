# sumologic-docker-logging-driver

A Docker logging driver plugin to send logs to Sumo Logic.

**Disclaimer:** This plugin is still being developed.  We recommend using this plugin in non-production environments.

**Note:** Docker plugins are not yet supported on Windows; see Docker's logging driver plugin [documentation].

[documentation]: https://github.com/docker/cli/blob/master/docs/extend/plugins_logging.md

## Setup

### Install Plugin

To install, run `plugin_install.sh`.

You can verify that the plugin `sumologic` has been installed and enabled by running `docker plugin ls`:

```bash
$ docker plugin ls
ID              NAME               DESCRIPTION                 ENABLED
cb0021522669    sumologic:latest   SumoLogic logging driver    true
```

### Create an HTTP Metrics Source in Sumo Logic
Create a [Sumo Logic account](https://www.sumologic.com/) if you don't currently have one.

Follow these instructions for [setting up an HTTP Source](https://help.sumologic.com/Send-Data/Sources/02Sources-for-Hosted-Collectors/HTTP-Source/zGenerate-a-new-URL-for-an-HTTP-Source) in Sumo Logic.  Be sure to obtain the URL endpoint after creating an HTTP Source.

## Usage
Once installed, the plugin can be used as any other Docker logging driver.
To run a specific container with the logging driver, you can use the `--log-driver` flag:
```bash
$ docker run --log-driver=sumologic --log-opt sumo-url=https://<deployment>.sumologic.com/receiver/v1/http/<source_token>
```

### Sumo Logic Options
To specify additional logging driver options, you can use the `--log-opt NAME=VALUE` flag.

| Option                    | Required? | Default Value | Description
| ------------------------- | :-------: | :-----------: | -------------------------------------- |
| sumo-url                  | Yes       |               | HTTP Source URL
| sumo-source-category      | No        | `dockerlog` | Source category to appear when searching on Sumo Logic by `_sourceCategory`. To include the log tag in the source category, use `{{.Tag}}`; see `tag`.
| sumo-compress             | No        | true          | Enable/disable gzip compression. Boolean.
| sumo-compress-level       | No        | -1            | Set the gzip compression level. Valid values are -1 (default), 0 (no compression), 1 (best speed) ... 9 (best compression).
| sumo-batch-size           | No        | 1000000       | The number of bytes of logs the driver should wait for before sending them in bulk. If the number of bytes never reaches `sumo-batch-size`, the driver will send the logs in smaller batches at predefined intervals; see `sumo-sending-interval`.
| sumo-sending-interval     | No        | 2s            | The maximum time the driver waits for number of logs to reach `sumo-batch-size` before sending the logs, even if the number of logs is less than the batch size. In the format 72h3m5s, valid time units are "ns", "us" (or "µs"), "ms", "s", "m", "h".
| sumo-proxy-url            | No        |               | Set a proxy URL.
| sumo-insecure-skip-verify | No        | false         | Ignore server certificate validation. Boolean.
| sumo-root-ca-path         | No        |               | Set the path to a custom root certificate.
| sumo-server-name          | No        |               | Name used to validate the server certificate. By default, uses hostname of the `sumo-url`.
| sumo-queue-size           | No        | 100           | The maximum number of log batches of size `sumo-batch-size` we can store in memory in the event of network failure, before we begin dropping batches. Thus in the worst case, the plugin will use `sumo-batch-size` * `sumo-queue-size` bytes of memory per container (default 100 MB).
| tag                       | No        |               | Specify a tag for messages, which interprets some markup. Default value is {{.ID}} (first 12 characters of the container ID). Refer to the [tag log-opt documentation] for customizing the log tag format.

[tag log-opt documentation]: https://docs.docker.com/engine/admin/logging/log_tags/

### Example

```bash
$ docker run --log-driver=sumologic \
    --log-opt sumo-url=https://<deployment>.sumologic.com/receiver/v1/http/<source_token> \
    --log-opt sumo-batch-size=2000000 \
    --log-opt sumo-queue-size=400 \
    --log-opt sumo-sending-frequency=500ms \
    --log-opt sumo-compress=false \
    --log-opt ...
    your/container
```

### Setting Default Options
To set the `sumologic` logging driver as the default, find the `daemon.json` file located in `/etc/docker` on Linux hosts.
Set the `log-driver` and `log-opts` keys to the desired values and restart Docker for the changes to take effect. For more information about configuring Docker using `daemon.json`, see [daemon.json].

[daemon.json]: https://docs.docker.com/engine/reference/commandline/dockerd/#daemon-configuration-file

```json
{
  "log-driver": "sumologic",
  "log-opts": {
    "sumo-url": "https://<deployment>.sumologic.com/receiver/v1/http/<source_token>"
  }
}
```

Now all containers started with `docker run your/container` will send logs to Sumo Logic.

## Uninstall
To cleanly disable and remove the plugin, run `plugin_uninstall.sh`.
