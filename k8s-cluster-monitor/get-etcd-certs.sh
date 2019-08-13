#!/usr/bin/env bash

ip=$(hostname -i)
ca=$(cat /etc/ssl/etcd/ssl/ca.pem | base64 -w0)
cert=$(cat /etc/ssl/etcd/ssl/member-${ip}.pem | base64 -w0)
key=$(cat /etc/ssl/etcd/ssl/member-${ip}-key.pem | base64 -w0)

cat <<EOF
apiVersion: v1
data:
  etcd-client-ca.crt: "${ca}"
  etcd-client.crt: "${cert}"
  etcd-client.key: "${key}"
kind: Secret
metadata:
  name: kube-etcd-client-certs
  namespace: monitoring
type: Opaque
EOF
