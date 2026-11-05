# Upgrade the reloader with TiDB Operator

This guide keeps the reloader console reachable through its existing Service
while enabling authentication, role checks and HTTPS. It uses existing
`TidbMonitor` CR fields; no TiDB Operator code change is required. The container
generation and merge behavior below was checked against Operator v1.6.6. For
other versions, confirm that the installed CRD accepts `additionalVolumes` and
`additionalContainers` and that the controller merges an entry named `reloader`.

## What changes on upgrade

Images containing the access-control changes listen on `127.0.0.1:9089` by
default. Without `--web.auth-file`, both the console and administrative API
return 503. File watching and notifications to Prometheus remain enabled.
Merely replacing the image will therefore break access through the existing
reloader Service. Prepare the Secrets first, then apply the new image and web
arguments in the same CR update.

With this guide, the Service keeps port 9089 but clients must use **HTTPS and
Basic authentication**. Viewers can read rules; only administrators can save.
The separate `prometheus-config-reloader` container is not configured here.

## 1. Prepare and record the current deployment

Use Bash for the commands below. Install `kubectl`, `jq`, Apache `htpasswd`,
OpenSSL and `curl`. Use a Kubernetes context authorized to update the target
`TidbMonitor` and create Secrets in its namespace. Disable shell tracing so
credentials are not copied into terminal logs.

```bash
set -euo pipefail
set +x
umask 077

export MONITOR_NS=tidb-cluster
export MONITOR_NAME=basic
export RELOADER_SERVICE="${MONITOR_NAME}-monitor-reloader"
export RELOADER_DNS="${RELOADER_SERVICE}.${MONITOR_NS}.svc"
export AUTH_SECRET="${MONITOR_NAME}-reloader-auth-v1"
export TLS_SECRET="${MONITOR_NAME}-reloader-tls-v1"
# Set these to an image already built and published from the security changes.
export RELOADER_IMAGE='<registry>/tidb-monitor-reloader'
export RELOADER_VERSION='<new-tag>'

export UPGRADE_DIR
UPGRADE_DIR=$(mktemp -d "${TMPDIR:-/tmp}/reloader-upgrade.XXXXXX")
cd "$UPGRADE_DIR"

kubectl config current-context
kubectl -n "$MONITOR_NS" get tidbmonitor "$MONITOR_NAME" -o json \
  > tidbmonitor-before.json
kubectl -n "$MONITOR_NS" get service "$RELOADER_SERVICE" -o yaml \
  > reloader-service-before.yaml
kubectl explain tidbmonitor.spec.additionalContainers
kubectl explain tidbmonitor.spec.additionalVolumes
```

Replace the example namespace, CR name, image and tag before proceeding. Verify
the Service name with `kubectl -n "$MONITOR_NS" get services`; sharded monitors
have additional Services that must also be checked after upgrade. Use a unique,
unused pair of Secret names for this upgrade, and retain the previous Secrets
for rollback. Do not commit the working directory or generated credentials.

Record the monitor StatefulSets owned by this CR (not unrelated monitors):

```bash
kubectl -n "$MONITOR_NS" get statefulsets -o json | \
  jq -r --arg name "$MONITOR_NAME" \
  '.items[] | select(any(.metadata.ownerReferences[]?;
    .kind == "TidbMonitor" and .name == $name)) | .metadata.name' \
  > monitor-statefulsets.txt
test -s monitor-statefulsets.txt
```

Plan for the monitor Pod restart caused by changing its template. With a single
monitor replica, scraping and alert evaluation can be interrupted while the Pod
restarts. Existing images are not updated automatically by this PR.

## 2. Create the authentication Secret

Generate two unique passwords interactively and store them in your password
manager. `htpasswd -nBC 12` prompts for a password and emits a bcrypt entry; do
not use its `-b` option, which puts the password on the command line. The service
accepts bcrypt costs 10 through 14.

```bash
htpasswd -nBC 12 oncall > oncall.htpasswd
htpasswd -nBC 12 observer > observer.htpasswd

jq -n \
  --rawfile admin oncall.htpasswd \
  --rawfile viewer observer.htpasswd \
  '{users: {
    oncall: {password_hash: ($admin | split(":")[1] | gsub("[\\r\\n]"; "")), role: "admin"},
    observer: {password_hash: ($viewer | split(":")[1] | gsub("[\\r\\n]"; "")), role: "viewer"}
  }}' > auth.json

jq -e '.users.oncall.role == "admin" and .users.observer.role == "viewer"' \
  auth.json > /dev/null
kubectl -n "$MONITOR_NS" create secret generic "$AUTH_SECRET" \
  --from-file=auth.json=auth.json
```

The file contains hashes, not plaintext passwords. Kubernetes Secrets still
need appropriate RBAC and encryption-at-rest controls. Do not dump their data
to logs. A Secret from an unrelated Grafana or Prometheus authentication setup
does not automatically configure reloader authentication.

## 3. Obtain a server certificate and create the TLS Secret

For production, obtain a server certificate from your organization's CA or
certificate management system. Its DNS SANs must cover the hostnames clients
actually use, including `$RELOADER_DNS` for the verification below. Add your
external DNS name if users access a load balancer or proxy directly. A Service
IP or Pod IP requires an IP SAN if clients connect using that IP. The examples
assume cluster DNS names under `cluster.local`; adjust for your cluster.

Generate a private key and CSR locally:

```bash
cat > server.cnf <<EOF_CERT
[req]
prompt = no
distinguished_name = dn
req_extensions = server_ext
[dn]
CN = ${RELOADER_DNS}
[server_ext]
basicConstraints = critical,CA:FALSE
keyUsage = critical,digitalSignature,keyEncipherment
extendedKeyUsage = serverAuth
subjectAltName = @names
[names]
DNS.1 = ${RELOADER_SERVICE}
DNS.2 = ${RELOADER_SERVICE}.${MONITOR_NS}
DNS.3 = ${RELOADER_DNS}
DNS.4 = ${RELOADER_DNS}.cluster.local
EOF_CERT

openssl req -new -newkey rsa:3072 -nodes \
  -keyout tls.key -out server.csr -config server.cnf
```

Have your CA issue the CSR as a TLS server certificate with the requested SANs.
Save the returned leaf certificate and any intermediate certificates in
`tls.crt` (leaf first), and the trusted CA bundle in `ca.crt`. Keep `tls.key`
private; never place a CA private key in the Pod. For a self-contained laboratory
exercise, use the short-lived test CA recipe at the end of this guide instead
of submitting the CSR to a production CA.

Check trust, expiry, hostname and key matching before creating the Secret:

```bash
openssl x509 -in tls.crt -noout -subject -issuer -dates -ext subjectAltName
openssl x509 -in tls.crt -noout -checkend 86400
openssl verify -CAfile ca.crt -untrusted tls.crt \
  -verify_hostname "$RELOADER_DNS" tls.crt
openssl pkey -in tls.key -pubout -out key.pub
openssl x509 -in tls.crt -pubkey -noout > cert.pub
cmp key.pub cert.pub

kubectl -n "$MONITOR_NS" create secret tls "$TLS_SECRET" \
  --cert=tls.crt --key=tls.key
kubectl -n "$MONITOR_NS" get secret "$AUTH_SECRET" "$TLS_SECRET"
```

The last command only prints Secret metadata. The CA bundle stays with clients;
it is not necessary to mount it in the reloader, which serves TLS but does not
request client certificates.

## 4. Update the complete TidbMonitor CR

Prepare an editable copy of the current CR, without server-managed metadata:

```bash
jq 'del(.status, .metadata.uid, .metadata.resourceVersion,
        .metadata.generation, .metadata.creationTimestamp,
        .metadata.managedFields,
        .metadata.annotations["kubectl.kubernetes.io/last-applied-configuration"])' \
  tidbmonitor-before.json > tidbmonitor-upgrade.json
```

Edit `tidbmonitor-upgrade.json` (or the complete YAML manifest maintained in your
configuration repository). Merge the following **spec fragment** into it,
substituting the image/tag and Secret names set above. It is not a standalone
CR to apply. Preserve all existing spec fields, additional containers, volumes,
mounts and unrelated arguments. If a `reloader` entry already exists, edit that
entry rather than adding a duplicate. Use unused volume names or reconcile
existing entries with these names.

```yaml
spec:
  reloader:
    baseImage: <registry>/tidb-monitor-reloader
    version: <new-tag>
  additionalVolumes:
    - name: reloader-web-auth
      secret:
        secretName: basic-reloader-auth-v1
    - name: reloader-web-tls
      secret:
        secretName: basic-reloader-tls-v1
  additionalContainers:
    - name: reloader
      args:
        - --web.listen-address=0.0.0.0:9089
        - --web.auth-file=/etc/reloader-auth/auth.json
        - --web.tls-cert-file=/etc/reloader-tls/tls.crt
        - --web.tls-key-file=/etc/reloader-tls/tls.key
      volumeMounts:
        - name: reloader-web-auth
          mountPath: /etc/reloader-auth
          readOnly: true
        - name: reloader-web-tls
          mountPath: /etc/reloader-tls
          readOnly: true
```

Add the web options as **args**, not a replacement `command`. Operator's generated
command includes `/bin/reload`, `--watch-path`, `--prometheus-url`,
`--root-store-path` and `--sub-store-path`; replacing it can break watching or
rule persistence. Resolve existing overrides of these web flags rather than
leaving conflicting duplicates. If an existing customization overrides the
container image or command, reconcile it with the intended upgrade as well.

Apply the complete CR once, with both the image and security configuration:

```bash
kubectl apply --dry-run=server -f tidbmonitor-upgrade.json
if kubectl diff -f tidbmonitor-upgrade.json; then
  :
else
  DIFF_EXIT=$?
  test "$DIFF_EXIT" -eq 1
fi
# Inspect the diff before running the actual apply:
kubectl apply -f tidbmonitor-upgrade.json
```

Do not use a JSON merge patch containing only the example arrays: it can replace
existing `additionalContainers` and `additionalVolumes` lists. If GitOps manages
the CR, commit the complete change to its source and let that controller apply
it instead of making a competing live edit.

## 5. Verify reconciliation and the actual Pod configuration

Allow Operator to reconcile the CR. For each recorded StatefulSet, first inspect
that its Pod template contains the new image, four web arguments and both
Secret mounts; an immediate rollout check alone might still observe the old
revision before Operator reconciles.

```bash
while IFS= read -r sts; do
  kubectl -n "$MONITOR_NS" get statefulset "$sts" -o json | \
    jq '.spec.template.spec.containers[] | select(.name == "reloader") |
        {image, command, args, volumeMounts}'
done < monitor-statefulsets.txt
```

Once the new template appears, wait for each rollout:

```bash
while IFS= read -r sts; do
  kubectl -n "$MONITOR_NS" rollout status "statefulset/$sts" --timeout=10m
done < monitor-statefulsets.txt

kubectl -n "$MONITOR_NS" get pods -o wide
export MONITOR_POD='<upgraded-monitor-pod-name>'
kubectl -n "$MONITOR_NS" get pod "$MONITOR_POD" -o json | \
  jq '.spec.containers[] | select(.name == "reloader") |
      {image, command, args, volumeMounts}'
kubectl -n "$MONITOR_NS" logs "$MONITOR_POD" -c reloader --tail=50
```

Confirm the actual Pod uses the intended image and preserved persistence/watch
arguments, has no startup error and is Ready. Repeat for all replicas/shards.
If reconciliation does not advance, inspect `kubectl describe tidbmonitor` and
Pod events. Missing Secrets, unreadable mounts, invalid bcrypt configuration
and mismatched certificates prevent startup.

## 6. Verify TLS, authentication, roles and the console

In a separate terminal, forward the existing Service port:

```bash
kubectl -n "$MONITOR_NS" port-forward "service/$RELOADER_SERVICE" 19089:9089
```

Run the following in the original terminal and working directory. `--connect-to`
preserves certificate hostname checking while connecting through localhost.
`--noproxy '*'` avoids sending this test through a workstation proxy. Never use
`curl -k`: that would hide a broken trust chain or hostname.

```bash
export RELOADER_URL="https://${RELOADER_DNS}:9089"
CURL_TLS=(--silent --show-error --connect-timeout 5 --max-time 30
  --noproxy '*' --cacert "$UPGRADE_DIR/ca.crt"
  --connect-to "${RELOADER_DNS}:9089:127.0.0.1:19089")

# TLS validation succeeds, but anonymous API access must be 401.
curl "${CURL_TLS[@]}" -D anonymous.headers -o anonymous.json \
  -w '%{http_code}\n' "$RELOADER_URL/monitoring/configs"
# Expected: 401, plus a WWW-Authenticate: Basic header.
cat anonymous.headers

# TLS 1.2 must be accepted (authentication is still required).
curl "${CURL_TLS[@]}" --tlsv1.2 --tls-max 1.2 -o /dev/null \
  -w '%{http_code}\n' "$RELOADER_URL/monitoring/configs"
# Expected: 401 with a successful TLS handshake.

# Anonymous access to the console must also be 401.
curl "${CURL_TLS[@]}" -o /dev/null -w '%{http_code}\n' "$RELOADER_URL/"

# curl prompts for passwords; no plaintext password appears in arguments/history.
curl "${CURL_TLS[@]}" --user observer --fail \
  "$RELOADER_URL/monitoring/configs" > configs.json
jq . configs.json
# Expected: 200 and a JSON array of rule filenames.

curl "${CURL_TLS[@]}" --user oncall -o /dev/null \
  -w '%{http_code}\n' "$RELOADER_URL/"
# Expected: 200 (console HTML).

curl "${CURL_TLS[@]}" --user observer --fail \
  "$RELOADER_URL/monitoring/rules" | jq -e '.status == "success"'
# Confirms the authenticated API can query the running Prometheus rule set.

# Enter an intentionally incorrect password at this prompt.
curl "${CURL_TLS[@]}" --user oncall -o /dev/null \
  -w '%{http_code}\n' "$RELOADER_URL/monitoring/configs"
# Expected: 401.

# Authorization is checked before body parsing; this must not change any file.
curl "${CURL_TLS[@]}" --user observer -X PUT \
  -H 'Content-Type: application/json' --data '{}' \
  -o viewer-denied.json -w '%{http_code}\n' \
  "$RELOADER_URL/monitoring/configs/authorization-check.yml"
# Expected: 403, administrator role required.
```

For browser access, use the actual HTTPS hostname covered by the certificate,
trust the issuing CA, and log in as `oncall` or `observer`. Update existing
clients and reverse proxies from HTTP to HTTPS and configure certificate trust
and Basic authentication. The current UI still displays Save to viewers; the
server enforces the 403 permission check.

Port forwarding verifies the selected backend but does not prove Pod-IP Service
reachability. Also open the HTTPS Service hostname from an authorized in-cluster
client (or the configured external endpoint) with the same CA and credentials.
For example, from that existing client with the CA bundle installed:

```bash
curl --cacert /path/to/ca.crt --user observer --fail \
  "https://${RELOADER_DNS}:9089/monitoring/configs"
```

Check each shard's endpoint separately; a single Service request does not test
every replica. Existing HTTP health checks against 9089 must be updated for
HTTPS/authentication. Prometheus's own health endpoint is separate.

### Optional: verify an administrator save and reload

This sends a real PUT and triggers reload and persistence. Run on a staging
monitor or in an approved maintenance window, with concurrent rule editors
paused. Choose an existing test rule file from `configs.json`; do not invent a
new filename, because the API only updates existing files. Even writing the
same content is a real operation.

```bash
export CONFIG_NAME='<existing-test-file.yml>'
CONFIG_ENCODED=$(jq -rn --arg name "$CONFIG_NAME" '$name | @uri')
curl "${CURL_TLS[@]}" --user oncall --fail \
  "$RELOADER_URL/monitoring/configs/$CONFIG_ENCODED" > config-before.json
jq -e 'type == "string"' config-before.json > /dev/null
jq -c '{content: .}' config-before.json > save-request.json

curl "${CURL_TLS[@]}" --user oncall -X PUT \
  -H 'Content-Type: application/json' --data-binary @save-request.json \
  -o save-response.json -w '%{http_code}\n' \
  "$RELOADER_URL/monitoring/configs/$CONFIG_ENCODED"
# Expected: 200 means save, configured persistence and reload completed.
# 502 means the file was saved but reload failed; do not assume no change occurred.

curl "${CURL_TLS[@]}" --user oncall --fail \
  "$RELOADER_URL/monitoring/configs/$CONFIG_ENCODED" > config-after.json
jq -rj . config-before.json > config-before.yml
jq -rj . config-after.json > config-after.yml
cmp config-before.yml config-after.yml

kubectl -n "$MONITOR_NS" logs "$MONITOR_POD" -c reloader --since=10m | \
  jq -R 'fromjson? | select(.event == "rule_api") |
    {actor, role, method, target, status, config_sha256, config_bytes, config_saved}'
```

Find the record for this file with `actor: oncall`, `role: admin`, `method: PUT`,
`status: 200`, `config_saved: true`, and a content fingerprint. Read the logs of
the Pod selected by the Service port-forward (or every monitor Pod), because it
may differ from `$MONITOR_POD`. Do not expect rejected viewer requests to have
this same event: the current rejection log is emitted by authentication
middleware. A failed persistence write can also follow a successful primary
file write, so inspect error responses rather than assuming failed requests
leave files unchanged.

## 7. Rotation and rollback

Authentication configuration is loaded at process startup, and TLS certificates
are not dynamically reloaded. For rotation, create new versioned Secrets, update
their names in the CR's volumes and apply the CR. The template change triggers a
Pod replacement. Merely updating Secret data does not reload the in-memory
configuration. Retain old Secrets until all Pods use the new configuration and
verification passes; repeat trust, login and role checks after rotation.

Prefer repairing a bad Secret or certificate while retaining the secured image.
If rollback to the previous image is unavoidable, first restrict or block access
to its 9089 endpoint: old images may restore anonymous rule-editing access and
ignore the new web flags. Update the managed CR, not only the StatefulSet; the
Operator otherwise reconciles your change away. Review the saved spec against
any intervening changes, restore the previous image and previous web-related
container/volume settings together, and restore clients to their previous
protocol as needed. Do not restore unrelated stale configuration blindly.
The following creates a rollback spec for review; it does not apply it:

```bash
jq '{spec: .spec}' tidbmonitor-before.json > rollback-spec.json
# After reviewing concurrent changes and securing network access, apply via your
# usual CR/GitOps workflow. A reviewed full-spec merge patch is also possible:
# kubectl -n "$MONITOR_NS" patch tidbmonitor "$MONITOR_NAME" \
#   --type=merge --patch-file=rollback-spec.json
```

The full-spec merge patch replaces array fields and does not automatically
remove newly added object keys; reconcile any such keys explicitly. Check the
rendered template and repeat rollout/health checks after rollback. Do not delete
PVCs or persisted rule data as part of this procedure. Securely remove local
private keys and test rule copies according to your workstation policy after
putting the required credentials and CA material in approved storage.

## Laboratory-only CA recipe

After generating `tls.key` and `server.csr` in step 3, this recipe produces a
short-lived private test CA and server certificate. Do not use this disposable
CA in production or commit its keys. Clients must explicitly trust `ca.crt`.

```bash
openssl req -x509 -newkey rsa:3072 -nodes -days 7 \
  -keyout test-ca.key -out ca.crt -subj '/CN=Reloader upgrade test CA' \
  -addext 'basicConstraints=critical,CA:TRUE' \
  -addext 'keyUsage=critical,keyCertSign,cRLSign'
openssl x509 -req -in server.csr -CA ca.crt -CAkey test-ca.key \
  -CAcreateserial -out tls.crt -days 7 -sha256 \
  -extfile server.cnf -extensions server_ext
```

Then return to step 3 to verify the certificate and create the TLS Secret.

## References

- [Reloader security options](SECURITY.md)
- [Operator v1.6.6 generated reloader command](https://github.com/pingcap/tidb-operator/blob/v1.6.6/pkg/monitor/monitor/util.go#L798-L840)
- [Operator v1.6.6 additional-container merge](https://github.com/pingcap/tidb-operator/blob/v1.6.6/pkg/monitor/monitor/util.go#L1238-L1253)
- [Kubernetes: managing Secrets using kubectl](https://kubernetes.io/docs/tasks/configmap-secret/managing-secret-using-kubectl/)
- [Apache: htpasswd options](https://httpd.apache.org/docs/2.4/programs/htpasswd.html)
