# Reloader container identity and migration

Both reloader image recipes run the service as the existing `pingcap` user,
UID 1000 and GID 2000 by default (build arguments can select different numeric
IDs). Root remains confined to image construction. The current initializer
images are separate and are not changed by this service-image default.

**Before upgrading**, ensure the runtime identity can read the active rules
and can write their directory and the optional persistent rules directory.
Existing root-owned volumes may need an administrator-controlled ownership/group
migration. Back up custom rules before changing ownership. Do not recursively
change unrelated data volumes or make files world writable as a workaround.

For TiDB Operator v1, the existing `spec.podSecurityContext` and
`spec.reloader.securityContext` can configure this without replacing generated
commands:

```yaml
spec:
  podSecurityContext:
    fsGroup: 2000
  reloader:
    securityContext:
      runAsNonRoot: true
      runAsUser: 1000
      runAsGroup: 2000
      allowPrivilegeEscalation: false
      capabilities:
        drop: ["ALL"]
```

Keep the existing reloader image configuration alongside these settings.
Check storage-driver support for fsGroup and test an existing PVC before rollout:
Kubernetes volume ownership handling depends on the driver. Where fsGroup does
not prepare permissions, migrate the dedicated directories explicitly using the
storage administrator's normal procedure. Permission errors must be resolved
before enabling rule editing/persistence. This image change does not modify
already-running workloads, install an admission policy, or override a deployer's
explicit root securityContext.

Current k8s datasource provisioning is also non-editable in Grafana, consistent
with the sibling datasource definitions. Change the reviewed provisioning source
and redeploy to update its URL or credentials.
