# sumologic-docker-logging-driver

A Docker logging driver plugin to send logs to Sumo Logic.

**Note:** Docker plugins are not yet supported on Windows; see Docker's logging driver plugin [documentation].

[documentation]: https://github.com/docker/cli/blob/master/docs/extend/plugins_logging.md

## Setup

### Install Plugin

Install the plugin (latest) from docker repository by running `docker plugin install`:

```bash
$ docker plugin install sumologic/docker-logging-driver

Plugin "sumologic/docker-logging-driver" is requesting the following privileges:
 - network: [host]
Do you grant the above permissions? [y/N] y
latest: Pulling from sumologic/docker-logging-driver
e8aad069319f: Download complete
Digest: sha256:764cdd72b451c4091d53e514f96d8499bf498e9ce8fc32d70b574c48d93b0cd4
Status: Downloaded newer image for sumologic/docker-logging-driver:latest
Installed plugin sumologic/docker-logging-driver
```

You can verify that the plugin `sumologic` has been installed and enabled by running `docker plugin ls`:

```bash
$ docker plugin ls

ID                  NAME                                     DESCRIPTION                       ENABLED
b72ceb1530ff        sumologic/docker-logging-driver:latest   Sumo Logic logging driver         true
```

### Create HTTP Source in Sumo Logic
Create a [Sumo Logic account](https://www.sumologic.com/) if you don't currently have one.

Follow these instructions for [setting up an HTTP Source](https://help.sumologic.com/Send-Data/Sources/02Sources-for-Hosted-Collectors/HTTP-Source/zGenerate-a-new-URL-for-an-HTTP-Source) in Sumo Logic.  Be sure to obtain the URL endpoint after creating an HTTP Source.

## Usage
Once installed, the plugin can be used as any other Docker logging driver.
To run a specific container with the logging driver, you can use the `--log-driver` flag:
```bash
$ docker run --log-driver=sumologic/docker-logging-driver --log-opt sumo-url=https://<deployment>.sumologic.com/receiver/v1/http/<source_token>
```

### Sumo Logic Options
To specify additional logging driver options, you can use the `--log-opt NAME=VALUE` flag.

| Option                    | Required? | Default Value        | Description
| ------------------------- | :-------: | :------------------: | -------------------------------------- |
| sumo-url                  | Yes       |                      | HTTP Source URL
| sumo-source-category      | No        | HTTP source category | Source category to appear when searching in Sumo Logic by `_sourceCategory`. Within the source category, the token `{{Tag}}` will be replaced with the value of the Docker tag option. If not specified, the default source category configured for the HTTP source will be used.
| sumo-source-name          | No        | container's name     | Source name to appear when searching in Sumo Logic by `_sourceName`. Within the source name, the token `{{Tag}}` will be replaced with the value of the Docker tag option. If not specified, the container's name will be used.
| sumo-source-host          | No        | host name            | Source host to appear when searching in Sumo Logic by `_sourceHost`. Within the source host, the token `{{Tag}}` will be replaced with the value of the Docker tag option. If not specified, the machine host name will be used.
| sumo-compress             | No        | `true`               | Enable/disable gzip compression. Boolean.
| sumo-compress-level       | No        | `-1`                 | Set the gzip compression level. Valid values are -1 (default), 0 (no compression), 1 (best speed) ... 9 (best compression).
| sumo-batch-size           | No        | `1000000`            | The number of bytes of logs the driver should wait for before sending them in bulk. If the number of bytes never reaches `sumo-batch-size`, the driver will send the logs in smaller batches at predefined intervals; see `sumo-sending-interval`.
| sumo-sending-interval     | No        | `2s`                 | The maximum time the driver waits for number of logs to reach `sumo-batch-size` before sending the logs, even if the number of logs is less than the batch size. In the format 72h3m5s, valid time units are `ns`, `us` (or `µs`), `ms`, `s`, `m`, `h`.
| sumo-proxy-url            | No        |                      | Set a proxy URL.
| sumo-insecure-skip-verify | No        | `false`              | Ignore server certificate validation. Boolean.
| sumo-root-ca-path         | No        |                      | Set the path to a custom root certificate.
| sumo-server-name          | No        |                      | Name used to validate the server certificate. By default, uses hostname of the `sumo-url`.
| sumo-queue-size           | No        | `100`                | The maximum number of log batches of size `sumo-batch-size` we can store in memory in the event of network failure, before we begin dropping batches. Thus in the worst case, the plugin will use `sumo-batch-size` * `sumo-queue-size` bytes of memory per container (default 100 MB).
| tag                       | No        | `{{.ID}}`            | Specifies a tag for messages, which can be used in the "source category", "source name", and "source host" fields. Certain tokens of the form {{X}} are supported. Default value is `{{.ID}}`, the first 12 characters of the container ID. Refer to the [tag log-opt documentation] for more information and a list of supported tokens.

[tag log-opt documentation]: https://docs.docker.com/engine/admin/logging/log_tags/

### Example

```bash
$ docker run --log-driver=sumologic/docker-logging-driver \
    --log-opt sumo-url=https://<deployment>.sumologic.com/receiver/v1/http/<source_token> \
    --log-opt sumo-batch-size=2000000 \
    --log-opt sumo-queue-size=400 \
    --log-opt sumo-sending-frequency=500ms \
    --log-opt sumo-compress=false \
    --log-opt ... \
    your/container
```

### Setting Default Options
To set the `sumologic/docker-logging-driver` logging driver as the default, find the `daemon.json` file located in `/etc/docker` on Linux hosts.
Set the `log-driver` and `log-opts` keys to the desired values and restart Docker for the changes to take effect. For more information about configuring Docker using `daemon.json`, see [daemon.json].

[daemon.json]: https://docs.docker.com/engine/reference/commandline/dockerd/#daemon-configuration-file

```json
{
  "log-driver": "sumologic/docker-logging-driver",
  "log-opts": {
    "sumo-url": "https://<deployment>.sumologic.com/receiver/v1/http/<source_token>"
  }
}
```

Now all containers started with `docker run your/container` will send logs to Sumo Logic.

## Uninstall
To cleanly disable and remove the plugin, run `plugin_uninstall.sh`.
