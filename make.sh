#!/bin/sh

set -xe

name="registry-1-stage.docker.io/alfred/docker-metrics-plugin-poc"
docker build -f Dockerfile.pluginbuild -t "$name" .

id=$(docker create "$name")

rm -rf rootfs
mkdir -p rootfs
docker export "$id" | tar -xvf - -C rootfs
docker rm "$id"

docker plugin disable -f "$name" || true
docker plugin rm -f "$name" || true
docker plugin create "$name" .
docker plugin enable "$name"
