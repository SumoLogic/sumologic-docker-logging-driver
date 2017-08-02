#!/bin/bash

docker plugin disable sumo-log-driver
docker plugin rm sumo-log-driver
sudo rm -rf rootfs
