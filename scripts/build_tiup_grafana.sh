#!/bin/bash
set -ex
cd "$(dirname -- "$0")/../output"
GrafanaPath=grafana-$TARGET_OS-$TARGET_ARCH
mkdir -p $GrafanaPath && cd $GrafanaPath 
rm -f grafana.tar.gz
if [ $TARGET_OS = 'darwin' ] && [ $TARGET_ARCH = 'arm64' ] ; then
	wget -O grafana.tar.gz -qnc  http://fileserver.pingcap.net/download/pingcap/grafana-7.5.10.darwin-arm64.tar.gz
else
	wget -O grafana.tar.gz -qnc https://download.pingcap.org/grafana-7.5.11.$TARGET_OS-$TARGET_ARCH.tar.gz
fi
mkdir -p grafana
tar -C grafana --strip-components=1 -xzf grafana.tar.gz
cp ../dashboards/tiup/* grafana/
tar -C grafana -czf grafana.tar.gz .
mv grafana.tar.gz ../$GrafanaPath.tar.gz 
