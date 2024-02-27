#!/usr/bin/env bash

set -e

get_ref_path() {
    current_git_desc=$(git describe --tags)
    if [[ $current_git_desc =~ -[0-9]+-g[0-9a-f]{7,10}$ ]]; then
        # not checkouted on a tag revision, or none tag added on this revision.
        git branch --contains | grep -v 'HEAD detached' | sed 's/^ *//' | sed 's/^* //'
    elif [[ $current_git_desc =~ v[0-9]+.[0-9]+.[0-9]+$ ]]; then
        echo "$current_git_desc"
    else
        echo "master"
    fi
}

main() {
    prometheus_ver="2.49.1"
    prometheus_os="${TARGET_OS:linux}"
    prometheus_arch="${TARGET_ARCH:amd64}"

    archive_dir="output"
    ref_path="$(get_ref_path)"
    echo "ref path is: $ref_path"

    rm -rf "$archive_dir"
    mkdir -p "$archive_dir"

    ## compose prometheus files from community repo.
    prometheus_dirname="prometheus-${prometheus_ver}.${prometheus_os}-${prometheus_arch}"
    prometheus_download_url="https://github.com/prometheus/prometheus/releases/download/v${prometheus_ver}/${prometheus_dirname}.tar.gz"
    wget "$prometheus_download_url" -O - | tar -zxvf - --strip-components=0 -C $archive_dir ${prometheus_dirname}

    ## add rules
    # mv "$archive_dir/${prometheus_dirname}" ${archive_dir}/prometheus
    mkdir ${archive_dir}/prometheus
    for rf in tidb.rules.yml pd.rules.yml tikv.rules.yml tikv.accelerate.rules.yml binlog.rules.yml ticdc.rules.yml tiflash.rules.yml lightning.rules.yml; do cp -v "monitor-snapshot/${ref_path}/operator/rules/$rf" "$archive_dir/prometheus"; done
    for rf in blacker.rules.yml bypass.rules.yml kafka.rules.yml node.rules.yml; do cp -v "platform-monitoring/ansible/rule/$rf" "$archive_dir/prometheus"; done

    echo "Done: $(realpath $archive_dir/prometheus)"
}

# main
ref_path="$(get_ref_path)"
echo "ref path is: $ref_path"
