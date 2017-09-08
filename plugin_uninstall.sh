#!/bin/bash

docker plugin disable sumologic/docker-logging-driver
docker plugin remove sumologic/docker-logging-driver
sudo rm -rf rootfs
