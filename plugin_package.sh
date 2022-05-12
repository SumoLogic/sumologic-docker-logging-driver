#!/bin/bash

source ./plugin_build.sh

for arch in "" "amd64" "arm64"; do
    build_plugin "push" $arch
done
