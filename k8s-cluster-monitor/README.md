# kubernetes cluster monitoring


kubernetes cluster level monitoring, including api server and etcd cluster monitoring.


## Features

Many of the useful settings come from the [kube-prometheus project](https://github.com/coreos/prometheus-operator/tree/master/contrib/kube-prometheus),and we added some new features.

- __Enable pv for grafana__

- __Enable pv for prometheus__

- __Add kubernetes cluster monitoring dashboard__

- __Add kubernetes node overview dashboard__

- __Enable systemd service monitoring__

- __Add API server dashboard__

- __Enable etcd cluster monitoring__

- __Log collection with ElasticSearch and Fluent-Bit__

## Install

There are two ways to customize and install, choose one you prefer.

-  __[Install from the generated YAML](#Install-from-the-generated-YAML-files-in-manifests-directory)__

-  __[Install from jsonnet](#Install-from-jsonnet)__

And then install logging services with helm:

- __[Install logging services with helm](#Install-logging-services-with-helm)__



## Install from the generated YAML files in manifests directory

We have generated all YAML files in manifests directory, if you have no jsonnet env, you can modify these files to customize your configuration.

### Step 1: Customizing 
- Overwrite etcd cluster certificate base64 data in [prometheus-secretEtcdCerts.yaml](manifests/prometheus/prometheus-secretEtcdCerts.yaml)
- Set etcd server ips in  [prometheus-endpointsEtcd.yaml](manifests/prometheus/prometheus-endpointsEtcd.yaml)
- Put your alert manager configuration in [alertmanager-secret.yaml](manifests/prometheus/alertmanager-secret.yaml)


### Step 2: Apply to kubernetes

```bash
kubectl apply -f manifest/
```
*Note*: If some resources created failed, wait for prometheus operator up and then apply again.



## Install from jsonnet


### Step 0: Install jsonnet env

- __Install jsonnet__
```bash
go get github.com/google/go-jsonnet/cmd/jsonnet
```

- __Install jb__
```bash
go get github.com/jsonnet-bundler/jsonnet-bundler/cmd/jb
```

- __Install gojsontoyaml__
```bash
go get github.com/brancz/gojsontoyaml
```

- __Download jsonnet dependencies__
```bash
#run in monitoring directory
jb install
```

### Step 1: Customizing 
- Put etcd cluster certificate path and IPs in [config.libsonnet](config.libsonnet)
```jsonnet
{
   etcd+:: {
      // Configure this to be the IP(s) to scrape - i.e. your etcd node(s) (use commas to separate multiple values).
      ips: ['172.16.4.155', '172.16.4.156', '172.16.4.157'],

      // Set these three variables to the fully qualified directory path on your work machine to the certificate files that are valid to scrape etcd metrics with (check the apiserver container).
      // All the sensitive information on the certificates will end up in a Kubernetes Secret.
      clientCA: importstr 'etcd/etcd-client-ca.crt',
      clientKey: importstr 'etcd/etcd-client.key',
      clientCert: importstr 'etcd/etcd-client.crt',
      insecureSkipVerify: true,
   }
}
```

- Put your additional dashboard name and path in [config.libsonnet](config.libsonnet)
```jsonnet
{
  grafanaDashboards+:: {
          //Configure this to be the dashboard definitions, keep the name unique
          'k8s-cluster-monitoring.json': (import 'dashboards/k8s-cluster-monitoring-dashboard.json'),
          'k8s-node-dashboard.json': (import 'dashboards/k8s-node-dashboard.json'),
          'api-server.json': (import 'dashboards/api-server-dashboard.json'),
  }
}
```

- Put your alert manager configuration in [alertmanager-secret.yaml](manifests/prometheus/alertmanager-secret.yaml) after step2

### Step 2: Generate YAML files
```bash
./build.sh true|false // the variables means that whether include etcd monitor and service of kube-scheduler and kube-controller-manager
```

### Step 3: Apply to kubernetes

```bash
kubectl apply -f manifest/prometheus-operator
```
```$xslt
kubectl apply -f manifest/prometheus
```
*Note*: If any resource failed to create, wait for prometheus operator to be up and then apply again. 
