#!/bin/bash

set -e
SERVICE_NAME=juicefs-csi-driver
VERSION=v0.0.1-d2
HUB=hub.confidentialfilesystems.com:4443

git pull

time=$(date "+%F %T")
id=$(git rev-parse HEAD)
GOOS=linux GOARCH=amd64 go build -o ${SERVICE_NAME} ../../cmd

docker build -f ./Dockerfile -t ${HUB}/cc/${SERVICE_NAME}:${VERSION} .
docker push ${HUB}/cc/${SERVICE_NAME}:${VERSION}

rm -rf ${SERVICE_NAME}

echo "build time: $(date)"
