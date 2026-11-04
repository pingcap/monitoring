#!/bin/bash
set -euo pipefail
archive=$1
output_dir=$2
mkdir -p "$output_dir"
# Read members to stdout so legacy leading slashes never become extraction paths.
members=$(tar -tPzf "$archive")
found=0
while IFS= read -r member; do
    normalized=${member#./}
    normalized=${normalized#/}
    case "$normalized" in
        ansible/tidb-monitor/*.json|ansible-monitor/tidb-monitor/*.json)
            name=${normalized#*/tidb-monitor/}
            case "$name" in ""|*/*) continue;; esac
            tar -xOPzf "$archive" -- "$member" > "$output_dir/$name"
            found=1
            ;;
    esac
done <<< "$members"
if [ "$found" = 0 ]; then
    echo "No dashboard JSON files found in $archive" >&2
    exit 1
fi
