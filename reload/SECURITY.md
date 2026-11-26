# Securing the reloader

New images keep the file watcher running but disable the administrative API until
an authentication file is configured. The default listener is loopback
(`127.0.0.1:9089`). This is a deliberate change from the anonymous, pod-wide API;
do not upgrade an externally accessed deployment without configuring access.

Mount a JSON authentication file from a Kubernetes Secret:

```json
{
  "users": {
    "oncall": {"password_hash": "<bcrypt hash>", "role": "admin"},
    "observer": {"password_hash": "<bcrypt hash>", "role": "viewer"}
  }
}
```

Use unique passwords and bcrypt cost 10–14 (for example, generate a hash with
`htpasswd -nBC 12 oncall`). Store only the hash in the file. `viewer` may read;
only `admin` may change rules. Restart the process after rotating the mounted
configuration. Credentials, authorization headers and rule text are not logged;
audit records include authenticated actor, role, operation, target, status and,
for accepted updates, the content SHA-256, byte count and whether it was saved.
A failed reload can occur after saving; the API then explicitly returns 502.

For direct network access, mount a valid certificate and private key and set:

```sh
/bin/reload --watch-path=/prometheus-rules/rules \
  --web.listen-address=0.0.0.0:9089 \
  --web.auth-file=/etc/reloader-web/auth.json \
  --web.tls-cert-file=/etc/reloader-web/tls.crt \
  --web.tls-key-file=/etc/reloader-web/tls.key
```

TLS requires at least 1.2. The browser's Basic-auth prompt also protects the
console. When TLS terminates at a trusted proxy, `--web.allow-insecure` permits
an authenticated plaintext backend explicitly; restrict that backend's network
reachability. It never disables authentication. No forwarded identity header is
trusted.

## Required network admission controls

The eight authentication slots bound concurrent bcrypt work, **not** password
attempts over time. Every remotely reachable deployment, including the direct
TLS listener above, must use an external HTTP gateway that rate-limits requests
before they reach the reloader. TLS and bcrypt alone do not prevent sustained
password guessing or slot exhaustion.

Require per-source limits and an aggregate limit shared across all clients and
gateway replicas. Set and record finite sustained request rates and burst
budgets; unlimited defaults are not sufficient. Choose the aggregate sustained
rate using measured bcrypt verification time at the configured cost and CPU,
leaving capacity for legitimate administrators. The bounded burst budget must
accommodate a cold console load (HTML, CSS, JavaScript, Ace modes and initial API
requests); verify this with an empty browser cache and normal rule reads/saves.
Count all requests, including missing or invalid credentials and unknown
usernames; do not key admission solely on the supplied username. Reject excess
requests with 429 before forwarding or running bcrypt. During a sustained
invalid-credential flood, verify both the configured rate/burst bounds and
availability of the restricted administrative recovery path below.

Only the gateway and explicitly authorized administrative clients may reach the
backend: enforce source allowlists and Kubernetes NetworkPolicy (or equivalent
firewall rules) for the Service and Pod IPs, including other monitor shards.
Do not expose a bypass through a NodePort, load balancer or alternate Service.
Use TLS from gateway to reloader, or the explicitly trusted plaintext backend
option described above. Restrict `kubectl port-forward` RBAC to trusted operators;
it bypasses gateway admission. Before exposing access, verify that a request
burst receives 429 at the gateway, direct backend access from an unauthorized
Pod fails, and a trusted operator can still authenticate. Keep an authorized
local port-forward recovery path for administrative access during a flood.

## TiDB Operator v1 deployments

For the complete migration procedure, see [Upgrading with TiDB Operator](UPGRADING.md),
including authentication and TLS Secret creation, CR configuration, verification,
rotation and rollback. No Operator code change is required for supported versions.

The existing `TidbMonitor.spec.additionalContainers` merge can append `args` and
mounts to the generated `reloader` container while retaining its generated
command, watch path, Prometheus URL and persistence version. For example:

```yaml
spec:
  additionalVolumes:
    - name: reloader-web
      secret:
        secretName: monitoring-reloader-web
  additionalContainers:
    - name: reloader
      args:
        - --web.listen-address=0.0.0.0:9089
        - --web.auth-file=/etc/reloader-web/auth.json
        - --web.tls-cert-file=/etc/reloader-web/tls.crt
        - --web.tls-key-file=/etc/reloader-web/tls.key
      volumeMounts:
        - name: reloader-web
          mountPath: /etc/reloader-web
          readOnly: true
```

The Secret supplies `auth.json`, `tls.crt` and `tls.key`. Use HTTPS when accessing
the reloader Service or proxy. This configuration is for a newly built image
containing the security changes, not existing published images that lack these
flags. Root-container and volume-ownership migration remains a separate concern;
authentication does not grant access to Kubernetes Secrets or change RBAC.
