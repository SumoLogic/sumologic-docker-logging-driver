#!/bin/bash

docker build -t rootfsimage .
id=$(docker create rootfsimage true)
mkdir rootfs
sudo docker export "$id" | sudo tar -x -C rootfs

docker rm -vf "$id"
docker rmi rootfsimage

docker plugin create sumologic/docker-logging-driver .
docker plugin enable sumologic/docker-logging-driver
