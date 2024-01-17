#!/bin/bash
set -e
SCRIPTDIR=$(dirname -- "$0")
cd $SCRIPTDIR/..
set +x
if [ -z $NOPULL ]; then
echo ./pull-monitoring --config=monitoring.yaml --tag=${TARGET}
./pull-monitoring --config=monitoring.yaml --tag=${TARGET} --token=$TOKEN
fi
set -x
mkdir -p output && cd output
case "$(uname -s)" in
    Darwin*)    tar_comp=2;;
    *)          tar_comp=1
esac
tar --strip-components=$tar_comp -xzf ../monitor-snapshot/${TARGET}/ansible-monitor.tar.gz
mkdir -p dashboards_swp/tiup
mkdir -p dashboards_swp/operator
cp tidb-monitor/*.json dashboards_swp/tiup/
cp ../monitor-snapshot/${TARGET}/operator/dashboards/*.json dashboards_swp/operator/
mv dashboards_swp dashboards
