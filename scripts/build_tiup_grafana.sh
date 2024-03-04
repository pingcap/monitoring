#!/bin/bash
set -exo pipefail

grafanaVer="$(grep -E "^grafana: " dependencies.yaml | awk -F': ' '{ print $2 }')"
grafanaVer="${grafanaVer#v}"
cd "$(dirname -- "$0")/../output"
GrafanaPath=grafana-$TARGET_OS-$TARGET_ARCH
grafanaFile="grafana-${grafanaVer}.${TARGET_OS}-${TARGET_ARCH}.tar.gz"
downloadUrl="https://dl.grafana.com/oss/release/$grafanaFile"
if [ "$TARGET_OS/$TARGET_ARCH" = "darwin/arm64" ]; then
    downloadUrl="https://download.pingcap.org/$grafanaFile"
fi
mkdir -p $GrafanaPath && cd "$GrafanaPath"
mkdir -p grafana
wget -O - "$downloadUrl" | tar -C grafana --strip-components=1 -xzvf -
cp ../dashboards/tiup/* grafana/
rm -f grafana.tar.gz
tar -C grafana -czf grafana.tar.gz .
mv grafana.tar.gz ../$GrafanaPath.tar.gz
