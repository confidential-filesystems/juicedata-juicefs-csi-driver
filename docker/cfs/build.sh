#!/bin/bash

set -e

git pull

# tmp build TODO

rm -rf build
mkdir -p build
rsync -a --delete --exclude '.github' ../../../juicedata-juicefs-csi-driver build/
rsync -a --delete --exclude '.github' ../../../filesystem-toolchain build/
rsync -a --delete --exclude '.github' ../../../csi-driver-common build/
cp build_docker.sh build/
cp Dockerfile build/
cp copy.go_ build/copy.go

cd build/
./build_docker.sh