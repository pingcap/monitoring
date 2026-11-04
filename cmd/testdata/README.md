`dm_worker.rules.yml` is a regression fixture from PingCAP TiFlow's
`dm/metrics/alertmanager/dm_worker.rules.yml` (Apache-2.0), retrieved 2026-11-04.
Upstream Git blob SHA: `1480e28019e607a8291482de82ca98b8a3de2db2`.
Source: https://github.com/pingcap/tiflow/tree/master/dm/metrics/alertmanager

It verifies that the configured DM source produces rules accepted by the
operator transformer and that current initializer scripts install them.
Tests use only this fixture and local mock transports, without credentials.
