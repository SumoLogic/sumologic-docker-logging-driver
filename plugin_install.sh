#!/bin/bash

docker build -t rootfsimage .
id=$(docker create rootfsimage true)
mkdir rootfs
sudo docker export "$id" | sudo tar -x -C rootfs

docker rm -vf "$id"
docker rmi rootfsimage

docker plugin create sumo-log-driver .
docker plugin enable sumo-log-driver
