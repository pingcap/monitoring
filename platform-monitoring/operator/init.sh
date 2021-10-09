#!/bin/sh
if [ ! -d $GF_PROVISIONING_PATH/dashboards  ];then
    mkdir -p $GF_PROVISIONING_PATH/dashboards
else
    rm -rf  $GF_PROVISIONING_PATH/dashboards/*
fi

# TiDB dashboard
cp /tmp/tidb.json $GF_PROVISIONING_PATH/dashboards
sed -i 's/Test-Cluster-TiDB/Cluster-TiDB/g'  $GF_PROVISIONING_PATH/dashboards/tidb.json
sed -i 's/label_values(pd_cluster_status, tidb_cluster)/label_values(tidb_server_connections, tidb_cluster)/g' $GF_PROVISIONING_PATH/dashboards/tidb.json

# Overview dashboard
cp /tmp/overview.json $GF_PROVISIONING_PATH/dashboards
sed -i 's/Test-Cluster-Overview/Cluster-Overview/g' $GF_PROVISIONING_PATH/dashboards/overview.json
sed -i 's/label_values(pd_cluster_status, tidb_cluster)/label_values(process_start_time_seconds, tidb_cluster)/g' $GF_PROVISIONING_PATH/dashboards/overview.json

# PD dashboard
cp /tmp/pd.json $GF_PROVISIONING_PATH/dashboards
sed -i 's/Test-Cluster-PD/Cluster-PD/g' $GF_PROVISIONING_PATH/dashboards/pd.json

# TiKV dashboard
cp /tmp/tikv*.json $GF_PROVISIONING_PATH/dashboards
if [ ! -f /tmp/tikv_pull.json ];then
    sed -i 's/Test-Cluster-TiKV-Details/Cluster-TiKV-Details/g' $GF_PROVISIONING_PATH/dashboards/tikv_details.json
    sed -i 's/Test-Cluster-TiKV-Summary/Cluster-TiKV-Summary/g' $GF_PROVISIONING_PATH/dashboards/tikv_summary.json
    sed -i 's/Test-Cluster-TiKV-Trouble-Shooting/Cluster-TiKV-Trouble-Shooting/g' $GF_PROVISIONING_PATH/dashboards/tikv_trouble_shooting.json
else
    sed -i 's/Test-Cluster-TiKV/Cluster-TiKV/g'  $GF_PROVISIONING_PATH/dashboards/tikv_pull.json
fi

# Binlog dashboard
cp /tmp/binlog.json $GF_PROVISIONING_PATH/dashboards
sed -i 's/Test-Cluster-Binlog/Cluster-Binlog/g'  $GF_PROVISIONING_PATH/dashboards/binlog.json

# Lighting
cp /tmp/lightning.json $GF_PROVISIONING_PATH/dashboards
sed -i 's/Test-Cluster-Lightning/Cluster-Lightning/g'  $GF_PROVISIONING_PATH/dashboards/lightning.json

# TiFlash
cp /tmp/tiflash_summary.json $GF_PROVISIONING_PATH/dashboards
sed -i 's/Test-Cluster-TiFlash-Summary/Cluster-TiFlash-Summary/g'  $GF_PROVISIONING_PATH/dashboards/tiflash_summary.json
cp /tmp/tiflash_proxy_summary.json $GF_PROVISIONING_PATH/dashboards
sed -i 's/Test-Cluster-TiFlash-Proxy-Summary/Cluster-TiFlash-Proxy-Summary/g' $GF_PROVISIONING_PATH/dashboards/tiflash_proxy_summary.json
cp /tmp/tiflash_proxy_details.json $GF_PROVISIONING_PATH/dashboards
sed -i 's/Test-Cluster-TiFlash-Proxy-Details/Cluster-TiFlash-Proxy-Details/g' $GF_PROVISIONING_PATH/dashboards/tiflash_proxy_details.json

# TiCDC dashboard
cp /tmp/ticdc.json $GF_PROVISIONING_PATH/dashboards
sed -i 's/Test-Cluster-TiCDC/Cluster-TiCDC/g' $GF_PROVISIONING_PATH/dashboards/ticdc.json
sed -i 's/label_values(go_goroutines, tidb_cluster)/label_values(ticdc_kvclient_event_feed_count, tidb_cluster)/g' $GF_PROVISIONING_PATH/dashboards/ticdc.json

# To support monitoring multiple clusters with one TidbMonitor, change the job label to component
sed -i 's%job=\\\"tiflash\\\"%component=\\"tiflash\\"%g' $GF_PROVISIONING_PATH/dashboards/*.json
sed -i 's%job=\\\"tikv-importer\\\"%component=\\"importer\\"%g' $GF_PROVISIONING_PATH/dashboards/*.json
sed -i 's%job=\\\"lightning\\\"%component=\\"tidb-lightning\\"%g' $GF_PROVISIONING_PATH/dashboards/*.json
sed -i 's/\"hide\":\s2/"hide": 0/g' $GF_PROVISIONING_PATH/dashboards/*.json


fs=`ls $GF_PROVISIONING_PATH/dashboards/*.json`
for f in $fs
do
  if [ "${f}" != "$GF_PROVISIONING_PATH/dashboards/nodes.json" ] &&
     [ "${f}" != "$GF_PROVISIONING_PATH/dashboards/pods.json" ]; then
    sed -i 's%job=%component=%g' ${f}
    sed -i 's%{{job}}%{{component}}%g' ${f}
    sed -i -e 's%\(by\s(\)job\(,.*)\)%\1component\2%g' -e 's%\(by\s(.*\),job,\(.*)\)%\1,component,\2%g' -e 's%\(by\s(.*,\)job)%\1component)%g' -e 's%\(by\s(\)job)%\1component)%g' ${f}
    sed -i 's/tidb_cluster=\\\"$tidb_cluster\\\"/tidb_cluster=~\\\"$tidb_cluster.*\\\"/g' ${f}
    tidb_cluster="$tidb_cluster"
  fi
done

# Rules
if [ ! -d $PROM_CONFIG_PATH/rules  ];then
    mkdir -p $PROM_CONFIG_PATH/rules
else
    rm -rf  $PROM_CONFIG_PATH/rules/*
fi
echo $META_TYPE
echo $META_INSTANCE
echo $META_VALUE
cp /tmp/*.rules.yml $PROM_CONFIG_PATH/rules
for file in $PROM_CONFIG_PATH/rules/*
do
    sed -i 's/ENV_LABELS_ENV/Cluster/g' $file
    sed -i 's%job=%component=%g' $file
    sed -i -e 's%\(by\s(\)job\(,.*)\)%\1component\2%g' -e 's%\(by\s(.*\),job,\(.*)\)%\1,component,\2%g' -e 's%\(by\s(.*,\)job)%\1component)%g' -e 's%\(by\s(\)job)%\1component)%g' $file
done

# Copy Persistent rules to override raw files
if [ ! -z $PROM_PERSISTENT_DIR ];
then
    if [ -d $PROM_PERSISTENT_DIR/latest-rules/${TIDB_VERSION##*/} ];then
        cp -f $PROM_PERSISTENT_DIR/latest-rules/${TIDB_VERSION##*/}/*.rules.yml $PROM_CONFIG_PATH/rules
    fi
fi


# Datasources
if [ ! -z $GF_DATASOURCE_PATH ];
then
    if [ ! -z $GF_K8S_PROMETHEUS_URL ];
    then
        sed -i 's,http://prometheus-k8s.monitoring.svc:9090,'$GF_K8S_PROMETHEUS_URL',g' /tmp/k8s-datasource.yaml
    fi

    if [ ! -z $GF_TIDB_PROMETHEUS_URL ];
    then
        sed -i 's,http://127.0.0.1:9090,'$GF_TIDB_PROMETHEUS_URL',g' /tmp/tidb-cluster-datasource.yaml
    fi

    cp /tmp/k8s-datasource.yaml $GF_DATASOURCE_PATH/
    cp /tmp/tidb-cluster-datasource.yaml $GF_DATASOURCE_PATH/

    # pods
    if [ ! -z $TIDB_CLUSTER_NAMESPACE ];
    then
         sed -i 's/$namespace/'$TIDB_CLUSTER_NAMESPACE'/g' /tmp/pods.json
    else
         sed -i 's/$namespace/default/g' /tmp/pods.json
    fi
    sed -i 's/Test-Cluster-Pods-Info/Cluster-Pods-Info/g' /tmp/pods.json
    cp /tmp/pods.json $GF_PROVISIONING_PATH/dashboards

    # nodes
     cp /tmp/nodes.json $GF_PROVISIONING_PATH/dashboards
fi

