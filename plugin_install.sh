#!/bin/bash

docker build -t rootfsimage .
id=$(docker create rootfsimage true)
rm -rf rootfs
mkdir rootfs
docker export "$id" | tar -x -C rootfs

docker rm -vf "$id"

docker plugin create sumologic/docker-logging-driver:1.0.2 .
docker plugin enable sumologic/docker-logging-driver:1.0.2
