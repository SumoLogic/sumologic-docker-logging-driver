#!/bin/bash

VERSION="1.0.5"

for arch in "amd64" "arm64"; do
    imagename="rootfsimage-${arch}"
    docker build -t ${imagename} --build-arg=GOARCH=${arch} .
    id=$(docker create ${imagename} true)
    rm -rf rootfs
    mkdir rootfs
    docker export "$id" | tar -x -C rootfs
    docker rm -vf "$id"
    docker plugin create sumologic/docker-logging-driver:${VERSION}-${arch} .
    docker plugin push sumologic/docker-logging-driver:${VERSION}-${arch}
done