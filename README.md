# docker-logging-driver

**Disclaimer:** This repo is still under development.  Please do not use for production workloads until it is officially released.

This is SumoLogic's logging driver plugin for Docker.
It collects logs from specified Docker containers and sends them to a SumoLogic Hosted Collector via an HTTP source.
Logging driver plugins are currently not supported on Windows; see Docker's logging driver plugin [documentation].

[documentation]: https://github.com/docker/cli/blob/master/docs/extend/plugins_logging.md

## Setup

To install, run `plugin_install.sh`.

You can verify that the plugin `sumologic` has been installed and enabled by running `docker plugin ls`:

```bash
$ docker plugin ls
ID              NAME               DESCRIPTION                 ENABLED
cb0021522669    sumologic:latest   SumoLogic logging driver    true
```

## Usage
Once installed, the plugin can be used as any other Docker logging driver.
To run a specific container with the logging driver, you can use the `--log-driver` flag:
```bash
$ docker run --log-driver=sumologic ...
```

### SumoLogic Options
To specify additional logging driver options, you can use the `--log-opt NAME=VALUE` flag.

| Option                      | Required? | Default Value | Description
| --------------------------- | :-------: | :-----------: | -------------------------------------- |
| `sumo-url`                  | Yes       |               | HTTP Source URL
| `sumo-compress`             | No        | true          | Enable/disable gzip compression. Boolean.
| `sumo-compress-level`       | No        | -1            | Set the gzip compression level. Valid values are -1 (default), 0 (no compression), 1 (best speed) ... 9 (best compression).
| `sumo-batch-size`           | No        | 1000000       | The number of bytes of logs the driver should wait for before sending them in bulk. If the number of bytes never reaches `sumo-batch-size`, the driver will send the logs in smaller batches at predefined intervals; see `sumo-sending-interval`.
| `sumo-sending-interval`     | No        | 2s            | The maximum time the driver waits for number of logs to reach `sumo-batch-size` before sending the logs, even if the number of logs is less than the batch size. In the format 72h3m5s, valid time units are "ns", "us" (or "µs"), "ms", "s", "m", "h".
| `sumo-proxy-url`            | No        |               | Set a proxy URL.
| `sumo-insecure-skip-verify` | No        | false         | Ignore server certificate validation. Boolean.
| `sumo-root-ca-path`         | No        |               | Set the path to a custom root certificate.
| `sumo-server-name`          | No        |               | Name used to validate the server certificate. By default, uses hostname of the `sumo-url`.
| `sumo-queue-size`           | No        | 100           | The maximum number of log batches of size sumo-batch-size we can store in memory in the event of network failure, before we begin dropping batches.

```bash
$ docker run --log-driver=sumologic \
    --log-opt sumo-url=https://example.sumologic.net/receiver/v1/http/token \
    --log-opt sumo-batch-size=2000000 \
    --log-opt sumo-queue-size=400 \
    --log-opt sumo-sending-frequency=500ms \
    --log-opt sumo-compress=false \
    --log-opt ...
    your/container
```

### Setting Default Options
To set the `sumologic` logging driver as the default, find the `daemon.json` file located in `/etc/docker` on Linux hosts.
Set the `log-driver` and `log-opts` keys to the desired values and restart Docker for the changes to take effect. For more information about +configuring Docker using `daemon.json`, see [daemon.json].

[daemon.json]: https://docs.docker.com/engine/reference/commandline/dockerd/#daemon-configuration-file

```json
{
  "log-driver": "sumologic",
  "log-opts": {
    "sumo-url": "https://example.sumologic.net/receiver/v1/http/token"
  }
}
```

Now all containers started with `docker run your/container` will send logs to SumoLogic.

## Uninstall
To cleanly disable and remove the plugin, run `plugin_uninstall.sh`.
