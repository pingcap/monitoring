#!/bin/bash
set -e
SCRIPTDIR=$(cd -- "$(dirname -- "$0")" && pwd)
cd "$SCRIPTDIR/.."
set +x
if [ "$NOPULL" != "1" ]; then
    echo ./pull-monitoring --config=monitoring.yaml --tag=${TARGET}
    ./pull-monitoring --config=monitoring.yaml --tag=${TARGET} --token=$TOKEN
fi
set -x
mkdir -p output && cd output
bash "$SCRIPTDIR/extract_dashboard_archive.sh" "../monitor-snapshot/${TARGET}/ansible-monitor.tar.gz" tidb-monitor
mkdir -p dashboards_swp/tiup
mkdir -p dashboards_swp/operator
cp tidb-monitor/*.json dashboards_swp/tiup/
cp ../monitor-snapshot/${TARGET}/operator/dashboards/*.json dashboards_swp/operator/
mv dashboards_swp dashboards
