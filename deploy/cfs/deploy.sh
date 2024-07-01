#!/bin/bash

set -e

export K8S_SERVICE=${1:-"cfs-juicefs-csi-webhook"}
export K8S_SECRET=${2:-"cfs-juicefs-csi-webhook-tls-secret"}
export K8S_NAMESPACE=${3:-"confidential-filesystems"}

kubectl create ns $K8S_NAMESPACE 2>/dev/null || true

echo "Generating tls certs:"
CERT_DIR=certs
../../scripts/certs.sh $CERT_DIR

echo "Creating $K8S_SECRET secret:"
kubectl delete secret $K8S_SECRET --ignore-not-found -n $K8S_NAMESPACE
kubectl create secret generic $K8S_SECRET \
  --from-file=tls.crt=$CERT_DIR/tls.crt \
  --from-file=tls.key=$CERT_DIR/tls.key -n $K8S_NAMESPACE

echo "Applying rbac:"
sed "s/NAMESPACE/$K8S_NAMESPACE/" ./artifact/cfs-juicefs-csi-rbac.yaml  > ./artifact/tmp-cfs-juicefs-csi-rbac.yaml
kubectl apply -f ./artifact/tmp-cfs-juicefs-csi-rbac.yaml -n $K8S_NAMESPACE
echo "Applying controller:"
kubectl delete -f ./artifact/cfs-juicefs-csi-controller.yaml --ignore-not-found -n $K8S_NAMESPACE
kubectl apply -f ./artifact/cfs-juicefs-csi-controller.yaml -n $K8S_NAMESPACE

echo "Applying webhook:"
CA_BUNDLE=$(cat $CERT_DIR/CA_BUNDLE)
sed -e "s/CA_BUNDLE/$CA_BUNDLE/" -e "s/NAMESPACE/$K8S_NAMESPACE/" ./artifact/cfs-juicefs-csi-webhook.yaml  > ./artifact/tmp-cfs-juicefs-csi-webhook.yaml

kubectl apply -f ./artifact/tmp-cfs-juicefs-csi-webhook.yaml -n $K8S_NAMESPACE

rm -rf ./artifact/tmp-cfs-juicefs-csi-rbac.yaml
rm -rf ./artifact/tmp-cfs-juicefs-csi-webhook.yaml
rm -rf $CERT_DIR