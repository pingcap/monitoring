# Restoring persisted alert rules

Current initializer entrypoints (`cmd/init.sh`,
`platform-monitoring/operator/init.sh` and
`monitor-snapshot/master/operator/init.sh`) install the bundled rules on every
start. They clear the dedicated active `rules` directory first for both DM and
TiDB. Rules previously mirrored to `PROM_PERSISTENT_DIR/latest-rules/<version>`
are **not restored by default**, because those files may have been written before
the editing API was authenticated. The initializer does not delete the PVC copy.

This deliberately changes restart behavior for deployments that relied on
persisted custom rules. Before upgrading, back up the PVC, compare the stored
rules with the expected configuration and validate them with the matching
Prometheus `promtool check rules`. If an administrator trusts the volume and
wants to restore the reviewed customizations, set `PROM_RESTORE_RULES=true` in
the initializer environment. TiDB Operator v1 supports this through:

```yaml
spec:
  initializer:
    envs:
      PROM_RESTORE_RULES: "true"
```

Keep the initializer's existing image configuration alongside this setting.
Unset or `false` uses bundled defaults. Other values fail initialization.
Opt-in restoration accepts only regular `*.rules.yml` files with names already
installed from the bundle for the selected DM/TiDB version; unknown rule names
and symlink rule files/directories fail initialization. Custom additional files
must be packaged into a reviewed bundle instead. Copy errors also fail startup.

Opting in is an administrator trust decision, not cryptographic provenance.
Restrict writes to the PVC and active rules directory to trusted components;
any local process with write access remains able to change those files. These
changes apply to current initializer sources, not historical version snapshots
or already-built images. Deploy a rebuilt image to obtain the protection.
