#!/bin/bash

set -e
SERVICE_NAME=juicefs-csi-driver
TAG_NAME=juicedata-juicefs-csi-driver
VERSION=v0.23.4-filesystem-d5
HUB=hub.confidentialfilesystems.com:30443

git pull

time=$(date "+%F %T")
id=$(git rev-parse HEAD)
GOOS=linux GOARCH=amd64 go build -o ${SERVICE_NAME} ../../cmd

docker build -f ./Dockerfile -t ${HUB}/cc/${TAG_NAME}:${VERSION} .
docker push ${HUB}/cc/${TAG_NAME}:${VERSION}

rm -rf ${SERVICE_NAME}

echo "build time: $(date)"
