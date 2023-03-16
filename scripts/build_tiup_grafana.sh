set -ex
BASEDIR="$(dirname -- "$0")/.."
rm -f grafana.tar.gz
if [ $OS = 'darwin' ] && [ $ARCH = 'arm64' ] ; then
	wget -O grafana.tar.gz -qnc  http://fileserver.pingcap.net/download/pingcap/grafana-7.5.10.darwin-arm64.tar.gz
else
	wget -O grafana.tar.gz -qnc https://download.pingcap.org/grafana-7.5.11.$OS-$ARCH.tar.gz
fi
mkdir -p grafana
tar -C grafana --strip-components=1 -xzf grafana.tar.gz
cp $BASEDIR/output/tiup_dashboards/* grafana/
tar -C grafana -czf grafana.tar.gz .
