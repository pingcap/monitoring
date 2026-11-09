#!/bin/sh

# Values are sed replacement data, never pieces of a sed program.
replace_template() {
    replacement=$(printf '%s' "$2" | sed 's/[\\&|]/\\&/g') || return 1
    sed -i "s|$1|$replacement|g" -- "$3"
}

# Validate before deleting or copying any existing dashboards or rules.
for value in "$TIDB_CLUSTER_NAME" "$TIDB_CLUSTER_NAMESPACE" \
    "$GF_K8S_PROMETHEUS_URL" "$GF_TIDB_PROMETHEUS_URL"; do
    case "$value" in
        *'
'*|*"$(printf '\r')"*) echo "Initializer values must be single-line" >&2; exit 1;;
    esac
done
if [ -z "$GF_PROVISIONING_PATH" ] || [ -z "$PROM_CONFIG_PATH" ]; then
    echo "Grafana and Prometheus output paths are required" >&2
    exit 1
fi

if [ ! -d "$GF_PROVISIONING_PATH/dashboards"  ];then
    mkdir -p -- "$GF_PROVISIONING_PATH/dashboards"
else
    rm -rf --  "$GF_PROVISIONING_PATH/dashboards"/*
fi

# TiDB dashboard
cp -- /tmp/tidb.json "$GF_PROVISIONING_PATH/dashboards"
replace_template 'Test-Cluster-TiDB' "${TIDB_CLUSTER_NAME}-TiDB" "$GF_PROVISIONING_PATH/dashboards"/tidb.json

# Overview dashboard
cp -- /tmp/overview.json "$GF_PROVISIONING_PATH/dashboards"
replace_template 'Test-Cluster-Overview' "${TIDB_CLUSTER_NAME}-Overview" "$GF_PROVISIONING_PATH/dashboards"/overview.json

# PD dashboard
cp -- /tmp/pd.json "$GF_PROVISIONING_PATH/dashboards"
replace_template 'Test-Cluster-PD' "${TIDB_CLUSTER_NAME}-PD" "$GF_PROVISIONING_PATH/dashboards"/pd.json

# TiKV dashboard
cp -- /tmp/tikv*.json "$GF_PROVISIONING_PATH/dashboards"
if [ ! -f /tmp/tikv_pull.json ];then
    replace_template 'Test-Cluster-TiKV-Details' "${TIDB_CLUSTER_NAME}-TiKV-Details" "$GF_PROVISIONING_PATH/dashboards"/tikv_details.json
    replace_template 'Test-Cluster-TiKV-Summary' "${TIDB_CLUSTER_NAME}-TiKV-Summary" "$GF_PROVISIONING_PATH/dashboards"/tikv_summary.json
    replace_template 'Test-Cluster-TiKV-Trouble-Shooting' "${TIDB_CLUSTER_NAME}-TiKV-Trouble-Shooting" "$GF_PROVISIONING_PATH/dashboards"/tikv_trouble_shooting.json
else
    replace_template 'Test-Cluster-TiKV' "${TIDB_CLUSTER_NAME}-TiKV" "$GF_PROVISIONING_PATH/dashboards"/tikv_pull.json
fi

# Binlog dashboard
cp -- /tmp/binlog.json "$GF_PROVISIONING_PATH/dashboards"
replace_template 'Test-Cluster-Binlog' "${TIDB_CLUSTER_NAME}-Binlog" "$GF_PROVISIONING_PATH/dashboards"/binlog.json

# Lighting
cp -- /tmp/lightning.json "$GF_PROVISIONING_PATH/dashboards"
replace_template 'Test-Cluster-Lightning' "${TIDB_CLUSTER_NAME}-Lightning" "$GF_PROVISIONING_PATH/dashboards"/lightning.json


# Rules
if [ ! -d "$PROM_CONFIG_PATH/rules"  ];then
    mkdir -p -- "$PROM_CONFIG_PATH/rules"
else
    rm -rf --  "$PROM_CONFIG_PATH/rules"/*
fi
echo "$META_TYPE"
echo "$META_INSTANCE"
echo "$META_VALUE"
cp -- /tmp/*.rules.yml "$PROM_CONFIG_PATH/rules"
for file in "$PROM_CONFIG_PATH/rules"/*
do
    replace_template 'ENV_LABELS_ENV' "${TIDB_CLUSTER_NAME}" "$file"
done

# Copy Persistent rules to override raw files
if [ -n "$PROM_PERSISTENT_DIR" ];
then
    if [ -d "$PROM_PERSISTENT_DIR/latest-rules/${TIDB_VERSION##*/}" ];then
        cp -f -- "$PROM_PERSISTENT_DIR/latest-rules/${TIDB_VERSION##*/}"/*.rules.yml "$PROM_CONFIG_PATH/rules"
    fi
fi


# Datasources
if [ -n "$GF_DATASOURCE_PATH" ];
then
    if [ -n "$GF_K8S_PROMETHEUS_URL" ];
    then
        replace_template 'http://prometheus-k8s.monitoring.svc:9090' "${GF_K8S_PROMETHEUS_URL}" /tmp/k8s-datasource.yaml
    fi

    if [ -n "$GF_TIDB_PROMETHEUS_URL" ];
    then
        replace_template 'http://127.0.0.1:9090' "${GF_TIDB_PROMETHEUS_URL}" /tmp/tidb-cluster-datasource.yaml
    fi

    cp -- /tmp/k8s-datasource.yaml "$GF_DATASOURCE_PATH/"
    cp -- /tmp/tidb-cluster-datasource.yaml "$GF_DATASOURCE_PATH/"

    # pods
    if [ -n "$TIDB_CLUSTER_NAMESPACE" ];
    then
         replace_template '$namespace' "${TIDB_CLUSTER_NAMESPACE}" /tmp/pods.json
    else
         sed -i 's/$namespace/default/g' /tmp/pods.json
    fi
    replace_template 'Test-Cluster-Pods-Info' "${TIDB_CLUSTER_NAME}-Pods-Info" /tmp/pods.json
    cp -- /tmp/pods.json "$GF_PROVISIONING_PATH/dashboards"

    # nodes
     cp -- /tmp/nodes.json "$GF_PROVISIONING_PATH/dashboards"
fi

