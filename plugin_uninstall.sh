#!/bin/bash

docker plugin disable sumologic
docker plugin remove sumologic
sudo rm -rf rootfs
