set -e
SCRIPTDIR=$(dirname -- "$0")
cd $SCRIPTDIR/..
make pull-monitoring
set +x
echo ./pull-monitoring --config=monitoring.yaml --tag=${TARGET}
./pull-monitoring --config=monitoring.yaml --tag=${TARGET} --token=$TOKEN
set -x
mkdir -p output && cd output
case "$(uname -s)" in
    Darwin*)    tar_comp=2;;
    *)          tar_comp=1
esac
tar --strip-components=$tar_comp -xzf ../monitor-snapshot/${TARGET}/ansible-monitor.tar.gz
mkdir -p tiup_dashboards
cp tidb-monitor/*.json tiup_dashboards/
mkdir -p operator_dashboards
cp ../monitor-snapshot/${TARGET}/operator/dashboards/*.json operator_dashboards/
