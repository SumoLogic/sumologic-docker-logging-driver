#!/bin/bash

source ./plugin_build.sh

docker plugin disable sumologic/docker-logging-driver:${VERSION}
docker plugin remove sumologic/docker-logging-driver:${VERSION}
rm -rf rootfs
