{
  _config+:: {
    etcd+:: {
      // Configure this to be the IP(s) to scrape - i.e. your etcd node(s) (use commas to separate multiple values).
      // ips: ['172.16.4.155', '172.16.4.156', '172.16.4.157'],
      ips: [],

      // Set these three variables to the fully qualified directory path on your work machine to the certificate files that are valid to scrape etcd metrics with (check the apiserver container).
      // All the sensitive information on the certificates will end up in a Kubernetes Secret.
      clientCA: '1',
      clientKey:  '2',
      clientCert:  '3',
      insecureSkipVerify: true,
    },

    grafanaDashboards+:: {
          //Configure this to be the dashboard definitions, keep the name unique
          'api-server.json': (import 'dashboards/api-server-dashboard.json'),
          'elasticsearch.json': (import 'dashboards/elasticsearch.json'),
          'k8s-cluster-monitoring.json': (import 'dashboards/k8s-cluster-monitoring-dashboard.json'),
          'k8s-nodes-overview.json': (import 'dashboards/k8s-nodes-overview.json'),
	  'k8s-node-disk.json': (import 'dashboards/k8s-node-disk.json'),
      },
      etcdDashboards+:: {
        'etcd.json': (import 'dashboards/etcd.json'),
      }
  },

}
