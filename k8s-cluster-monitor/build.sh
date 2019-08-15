#!/usr/bin/env bash

# This script uses arg $1 (name of *.jsonnet file to use) to generate the manifests/*.yaml files.

set -e
set -x
# only exit with zero if all commands of the pipeline exit successfully
set -o pipefail

# Make sure to start with a clean 'manifests' dir
rm -rf manifests/prometheus
rm -rf manifests/prometheus-operator
rm -rf output/prometheus
rm -rf manifests/archive
mkdir manifests/prometheus
mkdir manifests/prometheus-operator
mkdir output/prometheus
mkdir manifests/archive

# optional, but we would like to generate yaml, not json
jsonnet --ext-str PrivateCloudEnv="$1" -J vendor -m output/prometheus kubernetes-cluster-monitoring.jsonnet | xargs -I{} sh -c 'cat {} | gojsontoyaml > {}.yaml; rm -f {}' -- {}

mv output/prometheus/0*.yaml manifests/prometheus-operator
mv output/prometheus/*.yaml  manifests/prometheus

tar -czvf manifests/archive/prometheus-operator.tar.gz manifests/prometheus-operator/
tar -czvf manifests/archive/prometheus.tar.gz manifests/prometheus/
