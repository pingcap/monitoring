#!/usr/bin/env bash
set -euo pipefail

url=${1:?expected an HTTPS archive URL}
destination=${2:?expected a destination file}
script_dir=$(cd "$(dirname "$0")" && pwd)
filename=${url##*/}
expected=$(awk -v name="$filename" '$2 == name {print $1}' "$script_dir/artifact-checksums.sha256")
if [[ ! "$expected" =~ ^[0-9a-f]{64}$ ]]; then
    echo "No pinned SHA-256 for $filename; update artifact-checksums.sha256 with the dependency version" >&2
    exit 1
fi

archive=$(mktemp)
trap 'rm -f "$archive"' EXIT
curl --fail --silent --show-error --location --proto '=https' --proto-redir '=https' \
    --connect-timeout 30 --max-time 600 --output "$archive" "$url"
if command -v sha256sum >/dev/null 2>&1; then
    actual=$(sha256sum "$archive")
else
    actual=$(shasum -a 256 "$archive")
fi
if [[ "${actual%% *}" != "$expected" ]]; then
    echo "SHA-256 mismatch for $filename" >&2
    exit 1
fi
mv "$archive" "$destination"
