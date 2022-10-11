#!/bin/bash

VERSION="1.0.6"

build_plugin () {
    ACTION=$1
    ARCH=$2
    if [[ "$ARCH" == "" ]]; then
        ARCH="amd64"
        VERSION_ARCH="$VERSION"
    else
        VERSION_ARCH="$VERSION-$ARCH"
    fi

    imagename="rootfsimage-${VERSION_ARCH}"
    docker build -t ${imagename} --build-arg=GOARCH=${ARCH} .

    id=$(docker create ${imagename} true)
    rm -rf rootfs
    mkdir -p sumoplugin/rootfs
    docker export "$id" | tar -x -C sumoplugin/rootfs
    docker rm -vf "$id"

    cp config.json ./sumoplugin/
    docker plugin create ghcr.io/sumologic/docker-logging-driver:${VERSION_ARCH} ./sumoplugin/

    if [[ "$ACTION" == "install" ]]; then
        docker plugin enable ghcr.io/sumologic/docker-logging-driver:${VERSION_ARCH}
    elif [[ "$ACTION" == "push" ]]; then
        docker plugin push ghcr.io/sumologic/docker-logging-driver:${VERSION_ARCH}
    else
        echo "Invalid action ${ACTION}, must be 'install' or 'push'."
    fi

    docker rmi ${imagename}
    rm -rf sumoplugin
}
