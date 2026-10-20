#!/usr/bin/env bash

set -euo pipefail

get_ref_path() {
    # CI checks out the exact commit, so HEAD is detached. The generated build
    # script exports the target ref from the trigger, prefer it in that case.
    if [ -n "${TARGET:-}" ]; then
        echo "${TARGET}"
        return
    fi

    current_git_desc=$(git describe --tags 2>/dev/null || true)
    if [[ $current_git_desc =~ -[0-9]+-g[0-9a-f]{7,10}$ ]]; then
        # Not checked out on a tag revision, or no tag added on this revision.
        ref="$(git branch --contains | grep -vE 'detached (at|from)' | sed 's/^ *//' | sed 's/^* //')"
        if [ -z "$ref" ]; then
            # A detached HEAD may not be contained by any local branch, fall
            # back to the remote branches that contain it.
            ref="$(git for-each-ref --contains HEAD --format='%(refname:short)' refs/remotes/origin \
                | sed 's#^origin/##' | grep -vx 'HEAD' | head -n 1 || true)"
        fi
        echo "$ref"
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
    if [ -z "$ref_dir" ]; then
        echo "ERROR: cannot determine the monitor-snapshot ref, please set TARGET explicitly." >&2
        exit 1
    fi
    rules_dir="monitor-snapshot/${ref_dir}/operator/rules"
    if [ ! -d "$rules_dir" ]; then
        echo "ERROR: rules directory not found: $rules_dir" >&2
        exit 1
    fi

    mkdir -p "$archive_dir"

    ## Compose prometheus files from the community repo.
    rm -rf ${archive_dir}/prometheus
    prometheus_dirname="prometheus-${prometheus_ver}.${prometheus_os}-${prometheus_arch}"
    prometheus_download_url="https://github.com/prometheus/prometheus/releases/download/v${prometheus_ver}/${prometheus_dirname}.tar.gz"
    wget "$prometheus_download_url" -O - | tar -zxvf - --strip-components=0 -C "$archive_dir" "$prometheus_dirname"
    mv "$archive_dir/${prometheus_dirname}" ${archive_dir}/prometheus

    ## Add component rules
    cp -v "${rules_dir}"/* "${archive_dir}/prometheus"
    ## Add basic rules
    cp -v platform-monitoring/ansible/rule/*.rules.* "${archive_dir}/prometheus"

    echo "Done: $(realpath $archive_dir/prometheus)"
}

# Main execution
main
