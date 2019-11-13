## Install logging services with helm

We use [Fluent-Bit](https://fluentbit.io/) to collect logs and store them in an [ElasticSearch](https://www.elastic.co/products/elasticsearch) cluster, a [Kibana](https://www.elastic.co/products/kibana) instance is also setup that can used to show collected logs. These systems are dedicated from TiDB instances and not likely to be changed often, and we use helm to configure and deploy them.

Configurations are from the official [helm/charts](https://github.com/helm/charts/) repo, and custom values are pre-defined in `charts/{compoment-name}/values.yaml`, where you can adjust to fit your own need if necessary.

The default release names prefix is `infra`, if you would like to change it, just run this script:

```
./mkcharts.sh <your-prefered-prefix>
```

Suggest you already have helm installed and working (if you don't, see [this doc](https://helm.sh/docs/using_helm/#installing-helm)), and the official repo configured as `stable`, deploying these compoments are as easy as:

```
./install-logging.sh
```

Then wait all the containers to be up and you're all done.
