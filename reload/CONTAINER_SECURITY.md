# Rule persistence and reloader identity

Updating the reloader does not require a new initializer, new CR fields, a
restore opt-in, or a change to `alertManagerRulesVersion`. Keep the deployed
initializer and its version. Rules continue to be restored from the existing
current-version directory on the PVC. Authentication/TLS settings follow
[UPGRADING.md](UPGRADING.md); this change adds no web configuration requirements.

## Existing initializer images

The Linux reloader image starts `/bin/reload` with root privileges for local
filesystem preparation only. Before loading web credentials, starting watchers
or listening, it prepares the active rules directory and the current
`latest-rules/<version>` directory for the image's existing `pingcap` account
(default UID 1000/GID 2000). It does not recursively change `/data`, Prometheus,
Grafana or other rule versions. Directories and regular `.yml` rule files lose
unnecessary write/execute permissions; existing read permissions are retained.
Symlink paths and symlink, hard-linked or non-regular rule files are rejected.

The process clears supplementary groups, switches GID/UID, clears capabilities,
sets `no_new_privs`, and replaces itself before starting the service. There is no
root supervisor. A preparation or privilege-drop failure stops startup rather
than serving as root. The image intentionally retains root as its startup user:
this is **not** an image that is non-root for its entire lifetime. A scanner
checking only the Docker `USER` will still report root.

Pod rebuilds rerun the old initializer and may recreate root-owned active files.
The reloader therefore repeats preparation each time, without changing rule
contents or choosing a different version. Upgrading the reloader alone does not
make a future fully non-root image compatible with every old initializer.

## New initializer images and fully non-root deployments

Current TiDB and DM initializer sources prepare the same rule permissions after
generating and restoring rules on every startup. Once deployed, they also allow
the reloader to start directly as UID 1000/GID 2000, including after Pod rebuilds.
An explicit non-root runtime user bypasses reloader ownership preparation;
volume permissions must already permit editing. No identity override is needed
for the compatible default startup path.

Custom builds retain the existing `PINGCAP_UID`/`PINGCAP_GID` build arguments.
The reloader looks up the image's `pingcap` account; initializer permission
preparation uses matching `RELOADER_UID`/`RELOADER_GID` defaults or existing env
configuration (`spec.initializer.envs`, and `spec.dm.initializer.envs` for DM).
Explicit custom runtime identities must match the prepared ownership. Mounted
web credentials must be readable by the serving identity, as with any non-root
service; root-only custom Secret modes may require an access adjustment.

The compatibility startup needs permission to chown/chmod rule files and change
UID/GID. Storage that prohibits ownership changes (for example root-squashed
NFS), or a security context dropping the required startup capabilities, needs
pre-provisioned permissions and an explicit non-root runtime identity. Existing
root-only configurations cannot be made universally non-root without those
filesystem permissions.

## Rule restore behavior

Current initializers continue to restore persisted `*.rules.yml` files for the
selected version automatically, including names not present in the image's
bundle. Both TiDB and DM first clear only the dedicated active rules directory,
including hidden leftovers, and then install bundled rules and restore saved
ones. Invalid paths, symlink/hard-linked/non-regular saved rules, and copy errors
stop initialization. The PVC copies are not deleted. There is no
`PROM_RESTORE_RULES` option and no cross-version directory discovery or migration.

The existing version-selection semantics are unchanged: without an explicit
`alertManagerRulesVersion`, changing the initializer tag changes the rule store
version. Retaining customizations across that change uses the existing field;
this PR does not silently overlay rules from another version on a new bundle.

Filesystem validation and authenticated editing do not establish the provenance
of rules saved before authentication was enabled. Existing customizations remain
in use; if compromise is suspected, review/replace them as part of incident
response. This change does not claim automatic detection of historical tampering.

Provisioned k8s Grafana datasources are non-editable, consistent with their
sibling definitions. Change the provisioning source to update their settings.
