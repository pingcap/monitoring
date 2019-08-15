#!/bin/bash

# Install ElasticSearch and ElasticSearch-Exporter
helm install --name infra-es stable/elasticsearch --namespace monitoring -f charts/elasticsearch/values.yaml
helm install --name infra-es-exporter stable/elasticsearch-exporter --namespace monitoring -f charts/elasticsearch-exporter/values.yaml

# Install Elasticsearch Configuration Job
kubectl create configmap es-config --from-file=manifests/elasticsearch/esconfig.json --from-literal=es.url=http://infra-es-elasticsearch-client:9200 -n monitoring
kubectl apply -n monitoring -f manifests/elasticsearch/job.yaml

# Install Fluent-Bit and Fluentd
kubectl apply -n monitoring -f manifests/fluent-bit/
kubectl apply -n monitoring -f manifests/fluentd

# Install Kibana
helm install --name infra-kibana stable/kibana --namespace monitoring -f charts/kibana/values.yaml
