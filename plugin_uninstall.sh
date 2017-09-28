#!/bin/bash

docker plugin disable sumologic/docker-logging-driver:1.0.2
docker plugin remove sumologic/docker-logging-driver:1.0.2
rm -rf rootfs
