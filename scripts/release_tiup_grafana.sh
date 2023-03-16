set -ex
rm -f grafana.tar.gz
wget -O grafana.tar.gz -qnc https://download.pingcap.org/grafana-7.5.11.$OS-$ARCH.tar.gz
mkdir -p grafana
tar -C grafana --strip-components=1 -xzf grafana.tar.gz
cp tiup_dashboards/* grafana/
tar -C grafana -czf grafana.tar.gz .
tiup mirror publish grafana ${VERSION} grafana.tar.gz "bin/grafana-server" --arch $ARCH --os $OS --desc="Grafana is the open source analytics & monitoring solution for every database"
