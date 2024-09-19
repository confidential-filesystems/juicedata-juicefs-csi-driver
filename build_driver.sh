#!/bin/bash

set -e
SERVICE_NAME=juicefs-csi-driver
HUB=hub.confidentialfilesystems.com:30443
VERSION=${1:-v0.23.4-filesystem-d10}
SSH_KEY=${2:-$HOME/.ssh/id_rsa}

docker build --ssh default=${SSH_KEY} -f ./juicedata-juicefs-csi-driver.dockerfile -t ${HUB}/cc/${SERVICE_NAME}:${VERSION} .
docker push ${HUB}/cc/${SERVICE_NAME}:${VERSION}

echo "build time: $(date)"
