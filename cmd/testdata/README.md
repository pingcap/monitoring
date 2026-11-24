`dm_worker.rules.yml` is a regression fixture from PingCAP TiFlow's
`dm/metrics/alertmanager/dm_worker.rules.yml` (Apache-2.0), retrieved 2026-11-24.
Upstream Git blob SHA: `14f9bcf416eafbbce9f569da8679de8e2dbdc0d7`.
Source: https://github.com/pingcap/tiflow/tree/release-8.5/dm/metrics/alertmanager

It verifies that the configured DM source produces rules accepted by the
operator transformer and that current initializer scripts install them.
Tests use only this fixture and local mock transports, without credentials.
