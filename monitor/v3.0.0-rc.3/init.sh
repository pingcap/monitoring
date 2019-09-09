#!/bin/sh
if [ ! -d $GF_PROVISIONING_PATH/dashboards  ];then
    mkdir -p $GF_PROVISIONING_PATH/dashboards
else
    rm -rf  $GF_PROVISIONING_PATH/dashboards/*
fi

# TiDB dashboard
cp /tmp/tidb.json $GF_PROVISIONING_PATH/dashboards
sed -i 's/Test-Cluster-TiDB/'$TIDB_CLUSTER_NAME'-TiDB/g'  $GF_PROVISIONING_PATH/dashboards/tidb.json

# Overview dashboard
cp /tmp/overview.json $GF_PROVISIONING_PATH/dashboards
sed -i 's/Test-Cluster-Overview/'$TIDB_CLUSTER_NAME'-Overview/g' $GF_PROVISIONING_PATH/dashboards/overview.json

# PD dashboard
cp /tmp/pd.json $GF_PROVISIONING_PATH/dashboards
sed -i 's/Test-Cluster-PD/'$TIDB_CLUSTER_NAME'-PD/g' $GF_PROVISIONING_PATH/dashboards/pd.json

# TiKV dashboard
cp /tmp/tikv*.json $GF_PROVISIONING_PATH/dashboards
if [ ! -f /tmp/tikv_pull.json ];then
    sed -i 's/Test-Cluster-TiKV-Details/'$TIDB_CLUSTER_NAME'-TiKV-Details/g' $GF_PROVISIONING_PATH/dashboards/tikv_details.json
    sed -i 's/Test-Cluster-TiKV-Summary/'$TIDB_CLUSTER_NAME'-TiKV-Summary/g' $GF_PROVISIONING_PATH/dashboards/tikv_summary.json
    sed -i 's/Test-Cluster-TiKV-Trouble-Shooting/'$TIDB_CLUSTER_NAME'-TiKV-Trouble-Shooting/g' $GF_PROVISIONING_PATH/dashboards/tikv_trouble_shooting.json
else
    sed -i 's/Test-Cluster-TiKV/'$TIDB_CLUSTER_NAME'-TiKV/g'  $GF_PROVISIONING_PATH/dashboards/tikv_pull.json
fi

# Binlog dashboard
cp /tmp/binlog.json $GF_PROVISIONING_PATH/dashboards
sed -i 's/Test-Cluster-Binlog/'$TIDB_CLUSTER_NAME'-Binlog/g'  $GF_PROVISIONING_PATH/dashboards/binlog.json

# Lighting
cp /tmp/lightning.json $GF_PROVISIONING_PATH/dashboards
sed -i 's/Test-Cluster-Lightning/'$TIDB_CLUSTER_NAME'-Lightning/g'  $GF_PROVISIONING_PATH/dashboards/lightning.json


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
    sed -i 's/ENV_LABELS_ENV/'$TIDB_CLUSTER_NAME'/g' $file
done

# Datasources
if [ $GF_DATASOURCE_PATH ];
then
    if [ $GF_K8S_PROMETHEUS_URL ];
    then
        sed -i 's,http://prometheus-k8s.monitoring.svc:9090,'$GF_K8S_PROMETHEUS_URL',g' /tmp/k8s-datasource.yaml
    fi

    if [ $GF_TIDB_PROMETHEUS_URL ];
    then
        sed -i 's,http://127.0.0.1:9090,'$GF_TIDB_PROMETHEUS_URL',g' /tmp/tidb-cluster-datasource.yaml
    fi

    cp /tmp/k8s-datasource.yaml $GF_DATASOURCE_PATH/
    cp /tmp/tidb-cluster-datasource.yaml $GF_DATASOURCE_PATH/

    # pods
    if [ $TIDB_CLUSTER_NAMESPACE ];
    then
         sed -i 's/$namespace/'$TIDB_CLUSTER_NAMESPACE'/g' /tmp/pods.json
    fi
    sed -i 's/Test-Cluster-Pods-Info/'$TIDB_CLUSTER_NAME'-Pods-Info/g' /tmp/pods.json
    cp /tmp/pods.json $GF_PROVISIONING_PATH/dashboards

    # nodes
     cp /tmp/nodes.json $GF_PROVISIONING_PATH/dashboards
fi

