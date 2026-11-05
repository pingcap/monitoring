#!/bin/sh

# Values are sed replacement data, never pieces of a sed program.
replace_template() {
    replacement=$(printf '%s' "$2" | sed 's/[\\&|]/\\&/g') || return 1
    sed -i "s|$1|$replacement|g" -- "$3"
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
            if [ -L "$reloader_file" ] || [ ! -f "$reloader_file" ] || [ -z "$(find "$reloader_file" -prune -type f -links 1 -print)" ]; then
                echo "Rule must be a regular file: $reloader_file" >&2
                return 1
            fi
            chown "$reloader_uid:$reloader_gid" "$reloader_file" &&
                chmod u+rw,go-w,a-x "$reloader_file" || return 1
        done
    done
}

# Restore current-version customizations automatically, as before. Validate the
# filesystem entries rather than treating all historical user edits as untrusted.
reset_rules() {
    rules_dir="$PROM_CONFIG_PATH/rules"
    if [ -L "$rules_dir" ] || { [ -e "$rules_dir" ] && [ ! -d "$rules_dir" ]; }; then
        echo "Rules path must be a directory" >&2; return 1
    fi
    mkdir -p -- "$rules_dir" || return 1
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
        if [ -L "$rule" ] || [ ! -f "$rule" ] || [ -z "$(find "$rule" -prune -type f -links 1 -print)" ]; then
            echo "Persisted rule must be a regular file with one link: $rule" >&2; return 1
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

    # Rules
    reset_rules || exit 1

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
replace_template 'Test-Cluster-TiDB' "${TIDB_CLUSTER_NAME}-TiDB" "$GF_PROVISIONING_PATH/dashboards"/tidb.json
replace_template 'Test-Cluster-TiDB' "${TIDB_CLUSTER_NAME}-TiDB" "$GF_PROVISIONING_PATH/dashboards"/tidb_runtime.json
replace_template 'Test-Cluster-TiDB' "${TIDB_CLUSTER_NAME}-TiDB" "$GF_PROVISIONING_PATH/dashboards"/tidb_resource_control.json

# Overview dashboard
cp -- /tmp/overview.json "$GF_PROVISIONING_PATH/dashboards"
cp -- /tmp/performance_overview.json "$GF_PROVISIONING_PATH/dashboards"
replace_template 'Test-Cluster-Overview' "${TIDB_CLUSTER_NAME}-Overview" "$GF_PROVISIONING_PATH/dashboards"/overview.json
replace_template 'Test-Cluster-Performance-Overview' "${TIDB_CLUSTER_NAME}-Performance-Overview" "$GF_PROVISIONING_PATH/dashboards"/performance_overview.json

# PD dashboard
cp -- /tmp/pd.json "$GF_PROVISIONING_PATH/dashboards"
replace_template 'Test-Cluster-PD' "${TIDB_CLUSTER_NAME}-PD" "$GF_PROVISIONING_PATH/dashboards"/pd.json

# TiKV dashboard
cp -- /tmp/tikv*.json "$GF_PROVISIONING_PATH/dashboards"
if [ ! -f /tmp/tikv_pull.json ]; then
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

# TiFlash
cp -- /tmp/tiflash_summary.json "$GF_PROVISIONING_PATH/dashboards"
replace_template 'Test-Cluster-TiFlash-Summary' "${TIDB_CLUSTER_NAME}-TiFlash-Summary" "$GF_PROVISIONING_PATH/dashboards"/tiflash_summary.json
cp -- /tmp/tiflash_proxy_summary.json "$GF_PROVISIONING_PATH/dashboards"
replace_template 'Test-Cluster-TiFlash-Proxy-Summary' "${TIDB_CLUSTER_NAME}-TiFlash-Proxy-Summary" "$GF_PROVISIONING_PATH/dashboards"/tiflash_proxy_summary.json
cp -- /tmp/tiflash_proxy_details.json "$GF_PROVISIONING_PATH/dashboards"
replace_template 'Test-Cluster-TiFlash-Proxy-Details' "${TIDB_CLUSTER_NAME}-TiFlash-Proxy-Details" "$GF_PROVISIONING_PATH/dashboards"/tiflash_proxy_details.json

# TiCDC dashboard
cp -- /tmp/ticdc.json "$GF_PROVISIONING_PATH/dashboards"
replace_template 'Test-Cluster-TiCDC' "${TIDB_CLUSTER_NAME}-TiCDC" "$GF_PROVISIONING_PATH/dashboards"/ticdc.json
cp -- /tmp/TiCDC-Monitor-Summary.json "$GF_PROVISIONING_PATH/dashboards"
replace_template 'Test-Cluster-TiCDC-Summary' "${TIDB_CLUSTER_NAME}-TiCDC" "$GF_PROVISIONING_PATH/dashboards"/TiCDC-Monitor-Summary.json
# TiCDC New Arch
cp -- /tmp/ticdc_new_arch.json "$GF_PROVISIONING_PATH/dashboards"
replace_template 'Test-Cluster-TiCDC-New-Arch' "${TIDB_CLUSTER_NAME}-TiCDC" "$GF_PROVISIONING_PATH/dashboards"/ticdc_new_arch.json

# TiCI dashboard
cp -- /tmp/tici_meta.json "$GF_PROVISIONING_PATH/dashboards"
sed -i 's/Test-Cluster-TiCI-Meta/Cluster-TiCI-Meta/g' "$GF_PROVISIONING_PATH/dashboards"/tici_meta.json
cp -- /tmp/tici_worker.json "$GF_PROVISIONING_PATH/dashboards"
sed -i 's/Test-Cluster-TiCI-Worker/Cluster-TiCI-Worker/g' "$GF_PROVISIONING_PATH/dashboards"/tici_worker.json
cp -- /tmp/tici_reader.json "$GF_PROVISIONING_PATH/dashboards"
sed -i 's/Test-Cluster-TiCI-Reader/Cluster-TiCI-Reader/g' "$GF_PROVISIONING_PATH/dashboards"/tici_reader.json

# TiKV-CDC dashboard
cp -- /tmp/tikv-cdc.json "$GF_PROVISIONING_PATH/dashboards"
replace_template 'Test-Cluster-TiKV-CDC' "${TIDB_CLUSTER_NAME}-TiKV-CDC" "$GF_PROVISIONING_PATH/dashboards"/tikv-cdc.json

# TiProxy dashboard
cp -- /tmp/tiproxy_summary.json "$GF_PROVISIONING_PATH/dashboards"
replace_template 'Test-Cluster-TiProxy-Summary' "${TIDB_CLUSTER_NAME}-TiProxy-Summary" "$GF_PROVISIONING_PATH/dashboards"/tiproxy_summary.json

# To support monitoring multiple clusters with one TidbMonitor, change the job label to component
sed -i 's%job=\\\"tiflash\\\"%component=\\"tiflash\\"%g' "$GF_PROVISIONING_PATH/dashboards"/*.json
sed -i 's%job=\\\"tikv-importer\\\"%component=\\"importer\\"%g' "$GF_PROVISIONING_PATH/dashboards"/*.json
sed -i 's%job=\\\"lightning\\\"%component=\\"tidb-lightning\\"%g' "$GF_PROVISIONING_PATH/dashboards"/*.json
for f in "$GF_PROVISIONING_PATH/dashboards"/*.json; do
    [ -f "$f" ] || continue
    if [ "${f}" != "$GF_PROVISIONING_PATH/dashboards/nodes.json" ] &&
        [ "${f}" != "$GF_PROVISIONING_PATH/dashboards/pods.json" ]; then
        sed -i 's%job=%component=%g' "${f}"
        sed -i 's%{{job}}%{{component}}%g' "${f}"
        sed -i -e 's%\(by\s(\)job\(,.*)\)%\1component\2%g' -e 's%\(by\s(.*\),job,\(.*)\)%\1,component,\2%g' -e 's%\(by\s(.*,\)job)%\1component)%g' -e 's%\(by\s(\)job)%\1component)%g' "${f}"
    fi
done

# Rules
reset_rules || exit 1
echo $META_TYPE
echo $META_INSTANCE
echo $META_VALUE
cp -- /tmp/*.rules.yml "$PROM_CONFIG_PATH/rules" || exit 1
for file in "$PROM_CONFIG_PATH/rules"/*; do
    replace_template 'ENV_LABELS_ENV' "${TIDB_CLUSTER_NAME}" "$file"
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
    replace_template 'Test-Cluster-Pods-Info' "${TIDB_CLUSTER_NAME}-Pods-Info" /tmp/pods.json
    cp -- /tmp/pods.json "$GF_PROVISIONING_PATH/dashboards"

    # nodes
    cp -- /tmp/nodes.json "$GF_PROVISIONING_PATH/dashboards"
fi
