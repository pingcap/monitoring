# Reloader container identity and migration

Both reloader image recipes run the service as the existing `pingcap` user,
UID 1000 and GID 2000 by default (build arguments can select different numeric
IDs). The initializer remains a separate root init container so it can prepare
the shared rule volumes before the non-root service starts.

**Upgrade the initializer and reloader images together.** Merely replacing the
reloader image leaves root-owned files from the old initializer unwritable,
including on fresh volumes. In the same `TidbMonitor` update, select images built
from these changes in both `spec.initializer` and `spec.reloader`. For DM, also
update `spec.dm.initializer`. Retain the
authentication/TLS configuration required by the secured reloader.

After generating rules and restoring saved rules, both the TiDB and DM
initializer paths prepare the active rules directory and the current version's
`/data/latest-rules/<version>` directory and files for the reloader UID/GID.
This runs on every Pod initialization, so a rebuilt rules `emptyDir` remains
editable and existing root-owned persisted rules are migrated. Only the rules
directories and their regular `.yml` files are changed, not `/data/prometheus`,
`/data/grafana` or the entire data volume. Symbolic-link rule files and dedicated
rules directories are rejected; ownership or permission errors fail initialization.
Existing group/other read bits are retained, while group/other write and file
execute bits are removed. Back up custom rules before upgrading.

For TiDB Operator v1, the existing `spec.podSecurityContext` and
`spec.reloader.securityContext` can configure this without replacing generated
commands:

```yaml
spec:
  initializer:
    baseImage: <registry>/tidb-monitor-initializer
    version: <new-initializer-tag>
    envs:
      RELOADER_UID: "1000"
      RELOADER_GID: "2000"
  reloader:
    baseImage: <registry>/tidb-monitor-reloader
    version: <new-reloader-tag>
    securityContext:
      runAsNonRoot: true
      runAsUser: 1000
      runAsGroup: 2000
      allowPrivilegeEscalation: false
      capabilities:
        drop: ["ALL"]
```

Merge this fragment with the complete existing CR. The initializer environment
defaults to UID 1000/GID 2000 (or its build arguments); set these two variables
to match any custom reloader `runAsUser`/`runAsGroup`. DM's initializer image and
`envs` belong under `spec.dm.initializer`, not `spec.initializer`; when monitoring
both TiDB and DM, configure both initializers to match the shared reloader.
An `fsGroup` alone does
not prepare files created later by the initializer and is not a substitute.

Storage must allow the initializer to set ownership of these dedicated paths.
Storage that denies these operations (for example, root-squashed NFS) requires
a storage-specific initialization strategy; this initializer fails rather than
starting an unwritable reloader. Verify initialization before upgrading.
Do not make files world writable or recursively change unrelated data as a
workaround. Verify a real save/reload and persisted content after deleting and
recreating a monitor Pod, in addition to checking readiness. This image change
does not modify already-running workloads or override an explicit runtime identity.

Current k8s datasource provisioning is also non-editable in Grafana, consistent
with the sibling datasource definitions. Change the reviewed provisioning source
and redeploy to update its URL or credentials.
