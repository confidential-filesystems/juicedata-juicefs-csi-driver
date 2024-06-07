#!/bin/bash

tmpdir=${1:-./certs}
mkdir -p ${tmpdir}

K8S_SERVICE=${K8S_SERVICE:-"cfs-juicefs-csi-webhook"}
K8S_NAMESPACE=${K8S_NAMESPACE:-"confidential-filesystems"}

function gen_webhook_certs() {
  need_cmd mktemp
  need_cmd openssl
  need_cmd curl

  ensure openssl genrsa -out ${tmpdir}/ca.key 2048 >/dev/null 2>&1
  ensure openssl req -x509 -new -nodes -key ${tmpdir}/ca.key -subj "/CN=${K8S_SERVICE}.${K8S_NAMESPACE}.svc" -days 3650 -out ${tmpdir}/ca.crt >/dev/null 2>&1
  ensure openssl genrsa -out ${tmpdir}/server.key 2048 >/dev/null 2>&1

  cat <<EOF >${tmpdir}/csr.conf
[req]
prompt = no
req_extensions = v3_req
distinguished_name = dn
[dn]
CN = ${K8S_SERVICE}.${K8S_NAMESPACE}.svc
[v3_req]
basicConstraints = CA:FALSE
keyUsage = nonRepudiation, digitalSignature, keyEncipherment
extendedKeyUsage = serverAuth
subjectAltName = @alt_names
[alt_names]
DNS.1 = ${K8S_SERVICE}
DNS.2 = ${K8S_SERVICE}.${K8S_NAMESPACE}
DNS.3 = ${K8S_SERVICE}.${K8S_NAMESPACE}.svc
EOF

  ensure openssl req -new -key ${tmpdir}/server.key -out ${tmpdir}/server.csr -config ${tmpdir}/csr.conf >/dev/null 2>&1
  ensure openssl x509 -req -in ${tmpdir}/server.csr -CA ${tmpdir}/ca.crt -CAkey ${tmpdir}/ca.key -CAcreateserial -out ${tmpdir}/server.crt -days 3650 -extensions v3_req -extfile ${tmpdir}/csr.conf >/dev/null 2>&1

  TLS_KEY=$(openssl base64 -A -in ${tmpdir}/server.key)
  TLS_CRT=$(openssl base64 -A -in ${tmpdir}/server.crt)
  CA_BUNDLE=$(openssl base64 -A -in ${tmpdir}/ca.crt)

  rm ${tmpdir}/ca.crt ${tmpdir}/ca.key ${tmpdir}/ca.srl ${tmpdir}/csr.conf ${tmpdir}/server.csr
  mv ${tmpdir}/server.crt ${tmpdir}/tls.crt
  mv ${tmpdir}/server.key ${tmpdir}/tls.key
  echo $CA_BUNDLE > ${tmpdir}/CA_BUNDLE
}

need_cmd() {
  if ! check_cmd "$1"; then
    err "need '$1' (command not found)"
  fi
}

check_cmd() {
  command -v "$1" >/dev/null 2>&1
}

ensure() {
  if ! "$@"; then err "command failed: $*"; fi
}

gen_webhook_certs