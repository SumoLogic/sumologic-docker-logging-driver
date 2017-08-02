#!/bin/bash

docker plugin disable sumologic
docker plugin rm sumologic
sudo rm -rf rootfs
