#!/bin/sh

# Values are sed replacement data, never pieces of a sed program.
replace_template() {
    replacement=$(printf '%s' "$2" | sed 's/[\\&|]/\\&/g') || return 1
    sed -i "s|$1|$replacement|g" -- "$3"
}

# JSON string contents also work inside YAML double-quoted scalars.
json_string() {
    printf '%s' "$1" | LC_ALL=C awk '
        BEGIN {
            for (i = 1; i < 32; i++)
                escapes[sprintf("%c", i)] = sprintf("\\u%04x", i)
            escapes[sprintf("%c", 127)] = "\\u007f"
            for (i = 128; i < 160; i++)
                escapes[sprintf("%c%c", 194, i)] = sprintf("\\u%04x", i)
            escapes[sprintf("%c%c%c", 226, 128, 168)] = "\\u2028"
            escapes[sprintf("%c%c%c", 226, 128, 169)] = "\\u2029"
            escapes["\\"] = "\\\\"
            escapes["\""] = "\\\""
        }
        {
            for (i = 1; i <= length($0); i++) {
                c = substr($0, i, 1)
                if (substr($0, i, 2) in escapes) {
                    c = substr($0, i, 2)
                    i++
                } else if (substr($0, i, 3) in escapes) {
                    c = substr($0, i, 3)
                    i += 2
                }
                printf "%s", (c in escapes ? escapes[c] : c)
            }
        }
    '
}

replace_dashboard_title() {
    json_value=$(json_string "$2") || return 1
    replace_template "$1" "$json_value" "$3"
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
replace_dashboard_title 'Test-Cluster-TiDB' "${TIDB_CLUSTER_NAME}-TiDB" "$GF_PROVISIONING_PATH/dashboards"/tidb.json

# Overview dashboard
cp -- /tmp/overview.json "$GF_PROVISIONING_PATH/dashboards"
replace_dashboard_title 'Test-Cluster-Overview' "${TIDB_CLUSTER_NAME}-Overview" "$GF_PROVISIONING_PATH/dashboards"/overview.json

# PD dashboard
cp -- /tmp/pd.json "$GF_PROVISIONING_PATH/dashboards"
replace_dashboard_title 'Test-Cluster-PD' "${TIDB_CLUSTER_NAME}-PD" "$GF_PROVISIONING_PATH/dashboards"/pd.json

# TiKV dashboard
cp -- /tmp/tikv*.json "$GF_PROVISIONING_PATH/dashboards"
if [ ! -f /tmp/tikv_pull.json ];then
    replace_dashboard_title 'Test-Cluster-TiKV-Details' "${TIDB_CLUSTER_NAME}-TiKV-Details" "$GF_PROVISIONING_PATH/dashboards"/tikv_details.json
    replace_dashboard_title 'Test-Cluster-TiKV-Summary' "${TIDB_CLUSTER_NAME}-TiKV-Summary" "$GF_PROVISIONING_PATH/dashboards"/tikv_summary.json
    replace_dashboard_title 'Test-Cluster-TiKV-Trouble-Shooting' "${TIDB_CLUSTER_NAME}-TiKV-Trouble-Shooting" "$GF_PROVISIONING_PATH/dashboards"/tikv_trouble_shooting.json
else
    replace_dashboard_title 'Test-Cluster-TiKV' "${TIDB_CLUSTER_NAME}-TiKV" "$GF_PROVISIONING_PATH/dashboards"/tikv_pull.json
fi

# Binlog dashboard
cp -- /tmp/binlog.json "$GF_PROVISIONING_PATH/dashboards"
replace_dashboard_title 'Test-Cluster-Binlog' "${TIDB_CLUSTER_NAME}-Binlog" "$GF_PROVISIONING_PATH/dashboards"/binlog.json

# Lighting
cp -- /tmp/lightning.json "$GF_PROVISIONING_PATH/dashboards"
replace_dashboard_title 'Test-Cluster-Lightning' "${TIDB_CLUSTER_NAME}-Lightning" "$GF_PROVISIONING_PATH/dashboards"/lightning.json


# Rules
json_cluster_name=$(json_string "$TIDB_CLUSTER_NAME") || exit 1
rule_label=$(printf '%s' "$json_cluster_name" | sed 's/[\\&|]/\\&/g') || exit 1
# A constant template preserves the name without relying on sample labels.env.
rule_description=$(printf '%s' "$json_cluster_name" | sed "s/'/''/g" | sed 's/[\\&|]/\\&/g') || exit 1
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
    sed -i \
        -e "s|^\\([[:space:]]*env: \\)ENV_LABELS_ENV$|\\1\"$rule_label\"|" \
        -e "s|^\\([[:space:]]*description: 'cluster: \\)ENV_LABELS_ENV,|\\1{{ \"$rule_description\" }},|" \
        -- "$file"
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
    replace_dashboard_title 'Test-Cluster-Pods-Info' "${TIDB_CLUSTER_NAME}-Pods-Info" /tmp/pods.json
    cp -- /tmp/pods.json "$GF_PROVISIONING_PATH/dashboards"

    # nodes
     cp -- /tmp/nodes.json "$GF_PROVISIONING_PATH/dashboards"
fi

