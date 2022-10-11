#!/bin/bash

source ./plugin_build.sh

for arch in "" "amd64" "arm64" "aarch64"; do
    build_plugin "install" $arch
done
