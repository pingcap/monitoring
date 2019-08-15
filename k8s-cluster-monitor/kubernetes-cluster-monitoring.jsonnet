local k = import 'ksonnet/ksonnet.beta.3/k.libsonnet';
local p = import 'kube-prometheus/kube-prometheus.libsonnet';
local config = import 'config.libsonnet';
//local para = import 'template.libsonnet';
local serviceMonitorKubeletEndpoints = p.prometheus.serviceMonitorKubelet.spec.endpoints;

local privateCloud = (import 'kube-prometheus/kube-prometheus.libsonnet') + (import 'kube-prometheus/kube-prometheus-static-etcd.libsonnet');
local privateCloudConfig = {
    namespace: 'monitoring',
    grafanaPVCName: "graph-pvc",
    etcd+:: config._config.etcd,
    cpuThrottlingPercent: 90,
    notKubeDnsSelector: 'job="kube-test"',
};

local cloud = (import 'kube-prometheus/kube-prometheus.libsonnet') + (import 'public-cloud-config.libsonnet');
local cloudConfig = {
 namespace: 'monitoring',
    grafanaPVCName: "graph-pvc",
    cpuThrottlingPercent: 90,
    notKubeDnsSelector: 'job="kube-test"',
};
local pvc = k.core.v1.persistentVolumeClaim;
local kp =  (if std.extVar("PrivateCloudEnv") == "true" then privateCloud else cloud) + (import 'kube-prometheus/kube-prometheus-node-ports.libsonnet')   + {
  _config+:: (if std.extVar("PrivateCloudEnv") == "true" then privateCloudConfig else cloudConfig),

  grafanaDashboards+:: config._config.grafanaDashboards,
  prometheus+:: {
      rules: {
                 apiVersion: 'monitoring.coreos.com/v1',
                 kind: 'PrometheusRule',
                 metadata: {
                   labels: {
                     prometheus: $._config.prometheus.name,
                     role: 'alert-rules',
                   },
                   name: 'prometheus-' + $._config.prometheus.name + '-rules',
                   namespace: $._config.namespace,
                 },
                 spec: {
                   groups: (if std.extVar("PrivateCloudEnv") == "true" then $._config.prometheus.rules.groups else local rules = $._config.prometheus.rules.groups;{groups: std.filter(function(x) x.name != 'kube-scheduler.rules' && x.name != 'node-network', rules)}.groups),
                 },
               },
      prometheus+: {
        spec+: {
          retention: '30d',
          storage: {
            volumeClaimTemplate:
              pvc.new() +
              pvc.mixin.spec.withAccessModes('ReadWriteOnce') +
              pvc.mixin.spec.resources.withRequests({ storage: '1Gi' }) +
              pvc.mixin.spec.withStorageClassName('local-storage'),
          },
        },
      },

      serviceMonitorKubelet+: {
          spec+: {
              endpoints: [
              if std.objectHas(endpoint, "path") then
               endpoint + {
                 metricRelabelings: [
                   {
                   action: 'replace',
                   sourceLabels: ['id'],
                   regex: '^/machine\\.slice/machine-rkt\\\\x2d([^\\\\]+)\\\\.+/([^/]+)\\.service$',
                   targetLabel: 'rkt_container_name',
                   replacement: "${2}-${1}"
                   },
                   {
                   action: 'replace',
                   sourceLabels: ['id'],
                   regex: '^/system\\.slice/(.+)\\.service$',
                   targetLabel: 'systemd_service_name',
                   replacement: '${1}'
                   }
                 ]
               }
               else endpoint for endpoint in serviceMonitorKubeletEndpoints ],
          },
      },

      serviceMonitorElasticSearchExporter+: {
        apiVersion: "monitoring.coreos.com/v1",
        kind: "ServiceMonitor",
        metadata: {
          labels: {app: "elasticsearch"},
          name: "es-exporter",
          namespace: "monitoring",
        },
        spec+: {
          endpoints: [
            {interval: "10s"},
            {port: "http"},
            {scheme: "http"},
          ],
          jobLabel: "app",
          selector: {
            matchLabels: {app: "elasticsearch-exporter"},
          },
        },
      },
    },

  grafana+:: {
     pvc:
       pvc.new() +
       pvc.mixin.metadata.withName($._config.grafanaPVCName) +
       pvc.mixin.metadata.withNamespace($._config.namespace) +
       pvc.mixin.spec.withAccessModes('ReadWriteOnce') +
       pvc.mixin.spec.resources.withRequests({ storage: '1Gi' }) +
       pvc.mixin.spec.withStorageClassName('local-storage'),

     deployment:
       local deployment = k.apps.v1beta2.deployment;
       local container = k.apps.v1beta2.deployment.mixin.spec.template.spec.containersType;
       local volume = k.apps.v1beta2.deployment.mixin.spec.template.spec.volumesType;
       local containerPort = container.portsType;
       local containerVolumeMount = container.volumeMountsType;
       local podSelector = deployment.mixin.spec.template.spec.selectorType;
       local env = container.envType;

       local targetPort = 3000;
       local portName = 'http';
       local podLabels = { app: 'grafana' };

       local configVolumeName = 'grafana-config';
       local configSecretName = 'grafana-config';
       local configVolume = volume.withName(configVolumeName) + volume.mixin.secret.withSecretName(configSecretName);
       local configVolumeMount = containerVolumeMount.new(configVolumeName, '/etc/grafana');

       local storageVolumeName = 'grafana-storage';
       local storageVolume = volume.fromPersistentVolumeClaim(storageVolumeName, $._config.grafanaPVCName);
       local storageVolumeMount = containerVolumeMount.new(storageVolumeName, '/var/lib/grafana');

       local datasourcesVolumeName = 'grafana-datasources';
       local datasourcesSecretName = 'grafana-datasources';
       local datasourcesVolume = volume.withName(datasourcesVolumeName) + volume.mixin.secret.withSecretName(datasourcesSecretName);
       local datasourcesVolumeMount = containerVolumeMount.new(datasourcesVolumeName, '/etc/grafana/provisioning/datasources');

       local dashboardsVolumeName = 'grafana-dashboards';
       local dashboardsConfigMapName = 'grafana-dashboards';
       local dashboardsVolume = volume.withName(dashboardsVolumeName) + volume.mixin.configMap.withName(dashboardsConfigMapName);
       local dashboardsVolumeMount = containerVolumeMount.new(dashboardsVolumeName, '/etc/grafana/provisioning/dashboards');

       local volumeMounts =
         [
        storageVolumeMount,
        datasourcesVolumeMount,
         dashboardsVolumeMount,
         ] +
         [
           local dashboardName = std.strReplace(name, '.json', '');
           containerVolumeMount.new('grafana-dashboard-' + dashboardName, '/grafana-dashboard-definitions/0/' + dashboardName)
           for name in std.objectFields($._config.grafana.dashboards)
         ] +
         if std.length($._config.grafana.config) > 0 then [configVolumeMount] else [];

       local volumes =
         [
           storageVolume,
           datasourcesVolume,
           dashboardsVolume,
         ] +
         [
           local dashboardName = 'grafana-dashboard-' + std.strReplace(name, '.json', '');
           volume.withName(dashboardName) +
           volume.mixin.configMap.withName(dashboardName)
           for name in std.objectFields($._config.grafana.dashboards)
         ] +
         if std.length($._config.grafana.config) > 0 then [configVolume] else [];

       local c =
         container.new('grafana', $._config.imageRepos.grafana + ':' + $._config.versions.grafana) +
         (if std.length($._config.grafana.plugins) == 0 then {} else container.withEnv([env.new('GF_INSTALL_PLUGINS', std.join(',', $._config.grafana.plugins))])) +
         container.withVolumeMounts(volumeMounts) +
         container.withPorts(containerPort.newNamed(portName, targetPort)) +
         container.mixin.readinessProbe.httpGet.withPath('/api/health') +
         container.mixin.readinessProbe.httpGet.withPort(portName) +
         container.mixin.resources.withRequests($._config.grafana.container.requests) +
         container.mixin.resources.withLimits($._config.grafana.container.limits);

       local initContainersType = k.apps.v1beta2.deployment.mixin.spec.template.spec.initContainersType;
       local initContainerMount = [storageVolumeMount];
       local initContainer =
         initContainersType.new('init-data', $._config.imageRepos.grafana + ':' + $._config.versions.grafana) +
         initContainersType.mixin.securityContext.withRunAsUser(0) +
         initContainersType.withCommand(['/bin/sh', '-c', 'chmod 777 /var/lib/grafana']) +
         initContainersType.withVolumeMounts(initContainerMount);

       deployment.new('grafana', 1, c, podLabels) +
       deployment.mixin.metadata.withNamespace($._config.namespace) +
       deployment.mixin.metadata.withLabels(podLabels) +
       deployment.mixin.spec.selector.withMatchLabels(podLabels) +
       deployment.mixin.spec.template.spec.withNodeSelector({ 'beta.kubernetes.io/os': 'linux' }) +
       deployment.mixin.spec.template.spec.withVolumes(volumes) +
       deployment.mixin.spec.template.spec.withServiceAccountName('grafana') +
       deployment.mixin.spec.template.spec.withInitContainers(initContainer)
  },
};

{ ['00namespace-' + name]: kp.kubePrometheus[name] for name in std.objectFields(kp.kubePrometheus) } +
{ ['0prometheus-operator-' + name]: kp.prometheusOperator[name] for name in std.objectFields(kp.prometheusOperator) } +
{ ['node-exporter-' + name]: kp.nodeExporter[name] for name in std.objectFields(kp.nodeExporter) } +
{ ['kube-state-metrics-' + name]: kp.kubeStateMetrics[name] for name in std.objectFields(kp.kubeStateMetrics) } +
{ ['alertmanager-' + name]: kp.alertmanager[name] for name in std.objectFields(kp.alertmanager) } +
{ ['prometheus-' + name]: kp.prometheus[name] for name in std.objectFields(kp.prometheus) } +
{ ['prometheus-adapter-' + name]: kp.prometheusAdapter[name] for name in std.objectFields(kp.prometheusAdapter) } +
{ ['grafana-' + name]: kp.grafana[name] for name in std.objectFields(kp.grafana) }
