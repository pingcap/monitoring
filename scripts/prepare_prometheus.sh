#!/usr/bin/env bash

set -e

get_ref_path() {
    current_git_desc=$(git describe --tags)
    if [[ $current_git_desc =~ -[0-9]+-g[0-9a-f]{7,10}$ ]]; then
        # Not checked out on a tag revision, or no tag added on this revision.
        git branch --contains | grep -vE 'detached (at|from)' | sed 's/^ *//' | sed 's/^* //'
    elif [[ $current_git_desc =~ v[0-9]+.[0-9]+.[0-9]+$ ]]; then
        # Remove the leading 'v'
        version=${current_git_desc#"v"}

        # Split the version string using '.' as delimiter
        IFS='.' read -r major minor _patch <<<"$version"

        # Construct the desired release branch
        echo "release-$major.$minor"
    else
        echo "master"
    fi
}

main() {
    prometheus_ver="$(grep -E "^prometheus: " dependencies.yaml | awk -F': ' '{ print $2 }')"
    prometheus_ver="${prometheus_ver#v}"
    prometheus_os="${TARGET_OS:-linux}"
    prometheus_arch="${TARGET_ARCH:-amd64}"

    archive_dir="output"
    ref_dir="$(get_ref_path)"
    echo "Ref directory is: $ref_dir"

    mkdir -p "$archive_dir"

    ## Compose prometheus files from the community repo.
    rm -rf ${archive_dir}/prometheus
    prometheus_dirname="prometheus-${prometheus_ver}.${prometheus_os}-${prometheus_arch}"
    prometheus_download_url="https://github.com/prometheus/prometheus/releases/download/v${prometheus_ver}/${prometheus_dirname}.tar.gz"
    wget "$prometheus_download_url" -O - | tar -zxvf - --strip-components=0 -C "$archive_dir" "$prometheus_dirname"
    mv "$archive_dir/${prometheus_dirname}" ${archive_dir}/prometheus

    ## Add component rules
    cp -v monitor-snapshot/${ref_dir}/operator/rules/* "${archive_dir}/prometheus"
    ## Add basic rules
    cp -v platform-monitoring/ansible/rule/*.rules.* "${archive_dir}/prometheus"

    echo "Done: $(realpath $archive_dir/prometheus)"
}

# Main execution
main
