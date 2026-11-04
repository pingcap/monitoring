#!/bin/bash
set -euo pipefail

script_dir=$(cd "$(dirname "$0")" && pwd)

grafanaVer="$(grep -E "^grafana: " dependencies.yaml | awk -F': ' '{ print $2 }')"
grafanaVer="${grafanaVer#v}"
cd "$script_dir/../output"
GrafanaPath=grafana-$TARGET_OS-$TARGET_ARCH
grafanaFile="grafana-${grafanaVer}.${TARGET_OS}-${TARGET_ARCH}.tar.gz"
downloadUrl="https://dl.grafana.com/oss/release/$grafanaFile"
if [ "$TARGET_OS/$TARGET_ARCH" = "darwin/arm64" ]; then
    downloadUrl="https://download.pingcap.com/$grafanaFile"
fi
mkdir -p "$GrafanaPath" && cd "$GrafanaPath"
mkdir -p grafana
archive=$(mktemp)
trap 'rm -f "$archive"' EXIT
bash "$script_dir/download_verified.sh" "$downloadUrl" "$archive"
tar -C grafana --strip-components=1 -xzf "$archive"
cp ../dashboards/tiup/* grafana/
rm -f grafana.tar.gz
tar -C grafana -czf grafana.tar.gz .
mv grafana.tar.gz ../$GrafanaPath.tar.gz
