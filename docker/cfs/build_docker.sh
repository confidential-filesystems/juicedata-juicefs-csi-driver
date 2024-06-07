#!/bin/bash

set -e
SERVICE_NAME=juicefs-csi-driver
VERSION=v0.0.1
HUB=hub.confidentialfilesystems.com:4443

docker build -f ./Dockerfile -t ${HUB}/cc/${SERVICE_NAME}:${VERSION} .
docker push ${HUB}/cc/${SERVICE_NAME}:${VERSION}

echo "build time: $(date)"
