#!/bin/sh

# Values are sed replacement data, never pieces of a sed program.
replace_template() {
    replacement=$(printf '%s' "$2" | sed 's/[\\&|]/\\&/g') || return 1
    sed -i "s|$1|$replacement|g" -- "$3"
}

validate_rule_file() {
    if [ -L "$1" ] || [ ! -f "$1" ] || [ -z "$(find "$1" -prune -type f -links 1 -print)" ]; then
        echo "Rule must be a regular file with one link: $1" >&2
        return 1
    fi
}

# Run after copying/restoring rules on every Pod initialization. Do not chown /data.
prepare_reloader_rules() {
    reloader_uid=${RELOADER_UID:-1000}
    reloader_gid=${RELOADER_GID:-2000}
    for reloader_id in "$reloader_uid" "$reloader_gid"; do
        case "$reloader_id" in
            ''|*[!0-9]*) echo "Reloader UID/GID must be numeric" >&2; return 1;;
        esac
    done
    reloader_version=$1
    reloader_store=
    set -- "$PROM_CONFIG_PATH/rules"
    if [ -n "$PROM_PERSISTENT_DIR" ]; then
        case "$reloader_version" in
            ''|.|..|*/*) echo "Invalid persistent rules version" >&2; return 1;;
        esac
        reloader_store="$PROM_PERSISTENT_DIR/latest-rules/$reloader_version"
        set -- "$@" "$PROM_PERSISTENT_DIR/latest-rules" "$reloader_store"
    fi
    for reloader_dir do
        if [ -L "$reloader_dir" ] || { [ -e "$reloader_dir" ] && [ ! -d "$reloader_dir" ]; }; then
            echo "Rules path must be a directory, not a symlink: $reloader_dir" >&2
            return 1
        fi
        mkdir -p -- "$reloader_dir" &&
            chown "$reloader_uid:$reloader_gid" "$reloader_dir" &&
            chmod u+rwx,go-w "$reloader_dir" || return 1
    done
    for reloader_dir in "$PROM_CONFIG_PATH/rules" "$reloader_store"; do
        [ -n "$reloader_dir" ] || continue
        for reloader_file in "$reloader_dir"/*.yml; do
            [ -e "$reloader_file" ] || [ -L "$reloader_file" ] || continue
            validate_rule_file "$reloader_file" || return 1
            chown "$reloader_uid:$reloader_gid" "$reloader_file" &&
                chmod u+rw,go-w,a-x "$reloader_file" || return 1
        done
    done
}

# Restore current-version customizations automatically, as before. Validate the
# filesystem entries rather than treating all historical user edits as untrusted.
ensure_rules_directory() {
    rules_dir="$PROM_CONFIG_PATH/rules"
    if [ -L "$rules_dir" ] || { [ -e "$rules_dir" ] && [ ! -d "$rules_dir" ]; }; then
        echo "Rules path must be a directory" >&2; return 1
    fi
    mkdir -p -- "$rules_dir" || return 1
}

reset_rules() {
    ensure_rules_directory || return 1
    for entry in "$rules_dir"/* "$rules_dir"/.[!.]* "$rules_dir"/..?*; do
        [ -e "$entry" ] || [ -L "$entry" ] || continue
        rm -rf -- "$entry" || return 1
    done
}

restore_rules() {
    [ -n "$PROM_PERSISTENT_DIR" ] || return 0
    case "$1" in
        ''|.|..|*/*) echo "Invalid persisted rules version" >&2; return 1;;
    esac
    saved_root="$PROM_PERSISTENT_DIR/latest-rules"
    saved_version="$saved_root/$1"
    if [ -L "$PROM_PERSISTENT_DIR" ] || [ -L "$saved_root" ] || [ -L "$saved_version" ]; then
        echo "Persisted rule directories must not be symbolic links" >&2; return 1
    fi
    if [ -e "$saved_version" ] && [ ! -d "$saved_version" ]; then
        echo "Persisted rules path must be a directory" >&2; return 1
    fi
    [ -d "$saved_version" ] || return 0
    for rule in "$saved_version"/*.rules.yml; do
        [ -e "$rule" ] || [ -L "$rule" ] || continue
        validate_rule_file "$rule" || return 1
        destination="$PROM_CONFIG_PATH/rules/${rule##*/}"
        if [ -e "$destination" ] || [ -L "$destination" ]; then
            validate_rule_file "$destination" || return 1
        fi
        cp -f -- "$rule" "$PROM_CONFIG_PATH/rules/${rule##*/}" || return 1
    done
}

# Multiline replacements would become new sed commands or malformed configuration.
for value in "$DM_CLUSTER_NAME" "$TIDB_CLUSTER_NAME" "$TIDB_CLUSTER_NAMESPACE" \
    "$GF_DM_PROMETHEUS_URL" "$GF_K8S_PROMETHEUS_URL" "$GF_TIDB_PROMETHEUS_URL"; do
    case "$value" in
        *'
'*|*"$(printf '\r')"*) echo "Initializer values must be single-line" >&2; exit 1;;
    esac
done
if [ -z "$GF_PROVISIONING_PATH" ] || [ -z "$PROM_CONFIG_PATH" ]; then
    echo "Grafana and Prometheus output paths are required" >&2
    exit 1
fi

if [ -n "$DM_CLUSTER_NAME" ]; then
    echo "running for DM"
    if [ ! -d "$GF_PROVISIONING_PATH/dashboards" ]; then
        mkdir -p -- "$GF_PROVISIONING_PATH/dashboards"
    else
        rm -rf -- "$GF_PROVISIONING_PATH/dashboards"/*
    fi

    # DM dashboard
    cp -f -- /tmp/DM-Monitor-Standard.json "$GF_PROVISIONING_PATH/dashboards"
    replace_template 'Test-Cluster-DM' "${DM_CLUSTER_NAME}-DM-Standard" "$GF_PROVISIONING_PATH/dashboards"/DM-Monitor-Standard.json
    cp -f -- /tmp/DM-Monitor-Professional.json "$GF_PROVISIONING_PATH/dashboards"
    replace_template 'Test-Cluster-DM' "${DM_CLUSTER_NAME}-DM-Professional" "$GF_PROVISIONING_PATH/dashboards"/DM-Monitor-Professional.json

    # Operator runs DM after TiDB against the same directory. Keep TiDB rules.
    ensure_rules_directory || exit 1
    dm_rule="$PROM_CONFIG_PATH/rules/dm_worker.rules.yml"
    if [ -e "$dm_rule" ] || [ -L "$dm_rule" ]; then
        validate_rule_file "$dm_rule" || exit 1
    fi

    echo $META_TYPE
    echo $META_INSTANCE
    echo $META_VALUE
    if ! cp -f -- /tmp/dm_worker.rules.yml "$PROM_CONFIG_PATH/rules"; then
        echo "Failed to install DM alert rules" >&2
        exit 1
    fi
    replace_template 'ENV_LABELS_ENV' "${DM_CLUSTER_NAME}" "$PROM_CONFIG_PATH/rules/dm_worker.rules.yml" || exit 1

    restore_rules "${DM_VERSION##*/}" || exit 1

    # Data sources
    if [ -n "$GF_DATASOURCE_PATH" ]; then
        if [ -n "$GF_DM_PROMETHEUS_URL" ]; then
            replace_template 'http://127.0.0.1:9090' "${GF_DM_PROMETHEUS_URL}" /tmp/dm-cluster-datasource.yaml
        fi
        cp -f -- /tmp/dm-cluster-datasource.yaml "$GF_DATASOURCE_PATH/"
    fi
    prepare_reloader_rules "${DM_VERSION##*/}" || exit 1
    echo "running for DM done"
    exit
fi

if [ ! -d "$GF_PROVISIONING_PATH/dashboards" ]; then
    mkdir -p -- "$GF_PROVISIONING_PATH/dashboards"
else
    rm -rf -- "$GF_PROVISIONING_PATH/dashboards"/*
fi

# TiDB dashboard
cp -- /tmp/tidb*.json "$GF_PROVISIONING_PATH/dashboards"
sed -i 's/Test-Cluster-TiDB/Cluster-TiDB/g' "$GF_PROVISIONING_PATH/dashboards"/tidb.json
sed -i 's/Test-Cluster-TiDB/Cluster-TiDB/g' "$GF_PROVISIONING_PATH/dashboards"/tidb_runtime.json
sed -i 's/Test-Cluster-TiDB/Cluster-TiDB/g' "$GF_PROVISIONING_PATH/dashboards"/tidb_resource_control.json
sed -i 's/label_values(pd_cluster_status, tidb_cluster)/label_values(tidb_server_connections, tidb_cluster)/g' "$GF_PROVISIONING_PATH/dashboards"/tidb.json

# Overview dashboard
cp -- /tmp/overview.json "$GF_PROVISIONING_PATH/dashboards"
cp -- /tmp/performance_overview.json "$GF_PROVISIONING_PATH/dashboards"
sed -i 's/Test-Cluster-Overview/Cluster-Overview/g' "$GF_PROVISIONING_PATH/dashboards"/overview.json
sed -i 's/Test-Cluster-Performance-Overview/Cluster-Performance-Overview/g' "$GF_PROVISIONING_PATH/dashboards"/performance_overview.json
sed -i 's/label_values(pd_cluster_status, tidb_cluster)/label_values(process_start_time_seconds, tidb_cluster)/g' "$GF_PROVISIONING_PATH/dashboards"/overview.json

# PD dashboard
cp -- /tmp/pd.json "$GF_PROVISIONING_PATH/dashboards"
sed -i 's/Test-Cluster-PD/Cluster-PD/g' "$GF_PROVISIONING_PATH/dashboards"/pd.json

# TiKV dashboard
cp -- /tmp/tikv*.json "$GF_PROVISIONING_PATH/dashboards"
if [ ! -f /tmp/tikv_pull.json ]; then
    sed -i 's/Test-Cluster-TiKV-Details/Cluster-TiKV-Details/g' "$GF_PROVISIONING_PATH/dashboards"/tikv_details.json
    sed -i 's/Test-Cluster-TiKV-Summary/Cluster-TiKV-Summary/g' "$GF_PROVISIONING_PATH/dashboards"/tikv_summary.json
    sed -i 's/Test-Cluster-TiKV-Trouble-Shooting/Cluster-TiKV-Trouble-Shooting/g' "$GF_PROVISIONING_PATH/dashboards"/tikv_trouble_shooting.json
else
    sed -i 's/Test-Cluster-TiKV/Cluster-TiKV/g' "$GF_PROVISIONING_PATH/dashboards"/tikv_pull.json
fi

# Binlog dashboard
cp -- /tmp/binlog.json "$GF_PROVISIONING_PATH/dashboards"
sed -i 's/Test-Cluster-Binlog/Cluster-Binlog/g' "$GF_PROVISIONING_PATH/dashboards"/binlog.json

# Lighting
cp -- /tmp/lightning.json "$GF_PROVISIONING_PATH/dashboards"
sed -i 's/Test-Cluster-Lightning/Cluster-Lightning/g' "$GF_PROVISIONING_PATH/dashboards"/lightning.json

# TiFlash
cp -- /tmp/tiflash_summary.json "$GF_PROVISIONING_PATH/dashboards"
sed -i 's/Test-Cluster-TiFlash-Summary/Cluster-TiFlash-Summary/g' "$GF_PROVISIONING_PATH/dashboards"/tiflash_summary.json
cp -- /tmp/tiflash_proxy_summary.json "$GF_PROVISIONING_PATH/dashboards"
sed -i 's/Test-Cluster-TiFlash-Proxy-Summary/Cluster-TiFlash-Proxy-Summary/g' "$GF_PROVISIONING_PATH/dashboards"/tiflash_proxy_summary.json
cp -- /tmp/tiflash_proxy_details.json "$GF_PROVISIONING_PATH/dashboards"
sed -i 's/Test-Cluster-TiFlash-Proxy-Details/Cluster-TiFlash-Proxy-Details/g' "$GF_PROVISIONING_PATH/dashboards"/tiflash_proxy_details.json

# TiCDC dashboard
cp -- /tmp/ticdc.json "$GF_PROVISIONING_PATH/dashboards"
sed -i 's/Test-Cluster-TiCDC/Cluster-TiCDC/g' "$GF_PROVISIONING_PATH/dashboards"/ticdc.json
sed -i 's/label_values(go_goroutines, tidb_cluster)/label_values(ticdc_kvclient_event_feed_count, tidb_cluster)/g' "$GF_PROVISIONING_PATH/dashboards"/ticdc.json
cp -- /tmp/TiCDC-Monitor-Summary.json "$GF_PROVISIONING_PATH/dashboards"
sed -i 's/Test-Cluster-TiCDC-Summary/Cluster-TiCDC-Summary/g' "$GF_PROVISIONING_PATH/dashboards"/TiCDC-Monitor-Summary.json
sed -i 's/label_values(go_goroutines, tidb_cluster)/label_values(ticdc_kvclient_event_feed_count, tidb_cluster)/g' "$GF_PROVISIONING_PATH/dashboards"/TiCDC-Monitor-Summary.json
cp -- /tmp/ticdc_new_arch.json "$GF_PROVISIONING_PATH/dashboards/"
sed -i 's/Test-Cluster-TiCDC-New-Arch/Cluster-TiCDC-New-Arch/g' "$GF_PROVISIONING_PATH/dashboards/ticdc_new_arch.json"
sed -i 's/label_values(go_goroutines, tidb_cluster)/label_values(ticdc_kvclient_event_feed_count, tidb_cluster)/g' "$GF_PROVISIONING_PATH/dashboards"/ticdc_new_arch.json

# TiCI dashboard
cp -- /tmp/tici_meta.json "$GF_PROVISIONING_PATH/dashboards"
sed -i 's/Test-Cluster-TiCI-Meta/Cluster-TiCI-Meta/g' "$GF_PROVISIONING_PATH/dashboards"/tici_meta.json
cp -- /tmp/tici_worker.json "$GF_PROVISIONING_PATH/dashboards"
sed -i 's/Test-Cluster-TiCI-Worker/Cluster-TiCI-Worker/g' "$GF_PROVISIONING_PATH/dashboards"/tici_worker.json
cp -- /tmp/tici_reader.json "$GF_PROVISIONING_PATH/dashboards"
sed -i 's/Test-Cluster-TiCI-Reader/Cluster-TiCI-Reader/g' "$GF_PROVISIONING_PATH/dashboards"/tici_reader.json

# TiKV-CDC dashboard
cp -- /tmp/tikv-cdc.json "$GF_PROVISIONING_PATH/dashboards"
sed -i 's/Test-Cluster-TiKV-CDC/Cluster-TiKV-CDC/g' "$GF_PROVISIONING_PATH/dashboards"/tikv-cdc.json
sed -i 's/label_values(go_goroutines, tidb_cluster)/label_values(tikv_cdc_kvclient_event_feed_count, tidb_cluster)/g' "$GF_PROVISIONING_PATH/dashboards"/tikv_cdc.json

# TiProxy dashboard
cp -- /tmp/tiproxy_summary.json "$GF_PROVISIONING_PATH/dashboards"
sed -i 's/Test-Cluster-TiProxy-Summary/Cluster-TiProxy-Summary/g' "$GF_PROVISIONING_PATH/dashboards"/tiproxy_summary.json

# To support monitoring multiple clusters with one TidbMonitor, change the job label to component
sed -i 's%job=\\\"tiflash\\\"%component=\\"tiflash\\"%g' "$GF_PROVISIONING_PATH/dashboards"/*.json
sed -i 's%job=\\\"tikv-importer\\\"%component=\\"importer\\"%g' "$GF_PROVISIONING_PATH/dashboards"/*.json
sed -i 's%job=\\\"lightning\\\"%component=\\"tidb-lightning\\"%g' "$GF_PROVISIONING_PATH/dashboards"/*.json
sed -i 's/\"hide\":\s2/"hide": 0/g' "$GF_PROVISIONING_PATH/dashboards"/*.json

for f in "$GF_PROVISIONING_PATH/dashboards"/*.json; do
    [ -f "$f" ] || continue
    if [ "${f}" != "$GF_PROVISIONING_PATH/dashboards/nodes.json" ] &&
        [ "${f}" != "$GF_PROVISIONING_PATH/dashboards/pods.json" ]; then
        sed -i 's%job=%component=%g' "${f}"
        sed -i 's%{{job}}%{{component}}%g' "${f}"
        sed -i -e 's%\(by\s(\)job\(,.*)\)%\1component\2%g' -e 's%\(by\s(.*\),job,\(.*)\)%\1,component,\2%g' -e 's%\(by\s(.*,\)job)%\1component)%g' -e 's%\(by\s(\)job)%\1component)%g' "${f}"
        sed -i 's/tidb_cluster=\\\"$tidb_cluster\\\"/tidb_cluster=~\\\"$tidb_cluster.*\\\"/g' "${f}"
    fi
done

# Rules
reset_rules || exit 1
echo $META_TYPE
echo $META_INSTANCE
echo $META_VALUE
cp -- /tmp/*.rules.yml "$PROM_CONFIG_PATH/rules" || exit 1
for file in "$PROM_CONFIG_PATH/rules"/*; do
    sed -i 's/ENV_LABELS_ENV/Cluster/g' "$file"
    sed -i 's%job=%component=%g' "$file"
    sed -i -e 's%\(by\s(\)job\(,.*)\)%\1component\2%g' -e 's%\(by\s(.*\),job,\(.*)\)%\1,component,\2%g' -e 's%\(by\s(.*,\)job)%\1component)%g' -e 's%\(by\s(\)job)%\1component)%g' "$file"
done

restore_rules "${TIDB_VERSION##*/}" || exit 1

# Datasources
prepare_reloader_rules "${TIDB_VERSION##*/}" || exit 1

if [ -n "$GF_DATASOURCE_PATH" ]; then
    if [ -n "$GF_K8S_PROMETHEUS_URL" ]; then
        replace_template 'http://prometheus-k8s.monitoring.svc:9090' "${GF_K8S_PROMETHEUS_URL}" /tmp/k8s-datasource.yaml
    fi

    if [ -n "$GF_TIDB_PROMETHEUS_URL" ]; then
        replace_template 'http://127.0.0.1:9090' "${GF_TIDB_PROMETHEUS_URL}" /tmp/tidb-cluster-datasource.yaml
    fi

    cp -- /tmp/k8s-datasource.yaml "$GF_DATASOURCE_PATH/"
    cp -- /tmp/tidb-cluster-datasource.yaml "$GF_DATASOURCE_PATH/"

    # pods
    if [ -n "$TIDB_CLUSTER_NAMESPACE" ]; then
        replace_template '$namespace' "${TIDB_CLUSTER_NAMESPACE}" /tmp/pods.json
    else
        sed -i 's/$namespace/default/g' /tmp/pods.json
    fi
    sed -i 's/Test-Cluster-Pods-Info/Cluster-Pods-Info/g' /tmp/pods.json
    cp -- /tmp/pods.json "$GF_PROVISIONING_PATH/dashboards"

    # nodes
    cp -- /tmp/nodes.json "$GF_PROVISIONING_PATH/dashboards"
fi
