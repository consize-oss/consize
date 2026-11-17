# Kubernetes discovery access

Discovery needs only `list` on `apps/deployments` in the configured namespaces.
Health uses the same scoped list operation, with limit 1; it does not depend on
`/version`, create access reviews, watch resources or mutate the cluster.

## 1. Create the read-only identity

Start with `examples/rbac/kubernetes-discovery.yaml`. Change `example-workloads`
to an existing namespace that Consize should observe, then apply the file with an
administrative Kubernetes identity. It creates the `consize` namespace, a dedicated
ServiceAccount and a namespace-scoped Role/RoleBinding. Repeat the Role and binding
for each approved workload namespace. The identity has no Secret, Pod, get, watch
or write permission.

For intentionally all-namespace discovery, use the ServiceAccount from that file
and `examples/rbac/kubernetes-discovery-cluster.yaml` instead of namespaced grants.
The ClusterRole still grants only Deployment list. Choose an explicit namespace
allowlist unless all-namespace access is intended. Label selectors filter results;
they do not restrict Kubernetes authorization.

These grants add permissions; they cannot remove broader permissions already held
by an identity. Keep the discovery ServiceAccount separate from action privileges.

## 2. Configure authentication and cluster identity

For an external process, supply an explicit kubeconfig file and optional context.
Without a context override, the current context in that file is used. Consize does
not select the global kubectl config automatically. Credentials can use the
standard client-go kubeconfig mechanisms, including tokens, client certificates
and trusted credential executables. Protect credential/key files and review any
executable referenced by a kubeconfig before loading it.

For an in-cluster process, omit `kubeconfig` and `context`. Consize uses the mounted
ServiceAccount token and CA. The example ServiceAccount disables automatic token
mounting by default; explicitly enable it only on the Consize Pod:

```yaml
spec:
  serviceAccountName: consize-discovery
  automountServiceAccountToken: true
```

The basic bootstrap configuration uses one credential for the Kubernetes plugin:

```json
{
  "kubernetes": {
    "enabled": true,
    "kubeconfig": "/run/secrets/consize-reader-kubeconfig",
    "context": "reader-context",
    "cluster_id": "cluster-one",
    "location": "region-one",
    "discovery": {"namespaces": ["payments", "orders"]}
  }
}
```

If the same plugin also has a separately governed action identity, configure the
existing `kubeconfig`/`context` for that identity and add:

```json
{
  "discovery_auth": {
    "kubeconfig": "/run/secrets/consize-reader-kubeconfig",
    "context": "reader-context"
  }
}
```

Place `discovery_auth` inside `kubernetes`. Omission reuses the existing credential;
an explicit empty object selects in-cluster discovery authentication. Discovery
and action credentials must point to the exact same API server endpoint. Discovery
requests and health probes use only the discovery client. Existing action calls
continue using the action client; these RBAC examples do not enable actions.
The standalone executable accepts the same nested fields under `DiscoveryAuth`
and rejects unknown configuration keys instead of silently ignoring auth typos.

API server endpoints must use verified HTTPS. Insecure verification bypasses,
URL user information, query parameters and fragments are rejected. Supply the
correct CA and server name rather than disabling verification. Redirects are not
followed by discovery requests. Cluster ID and location are validated stable
registry coordinates, supplied by the operator; they are not an attestation of
an immutable server UID. Keep them unique and consistent with the selected context.

## 3. Verify access and run discovery

Run `consize health -config PATH` or inspect the configured plugin through
`GET /api/plugins`. Health lists one Deployment per configured namespace using
the configured selector. The combined probe obeys caller cancellation and a
maximum of 15 seconds or the configured scan timeout, whichever is smaller.

Read `health.details.discovery.access`:

- `status`: authorized, unauthorized, forbidden or failed.
- `api_reachable`: verified API response reached, failed connection, or absent when
  the available error cannot establish reachability.
- `verb`, `api_group`, `resource`, `namespace`: the required safe permission scope.
- `code`, `retryable`, `guidance`: fixed error classification and remediation.
- `needs_successful_scan`: access has failed or recovered without a complete scan.

Plugin scan/access history is process-local: a new CLI health process starts with
unknown scan history. Inspect durable discovery runs for persisted history.
A successful access probe before any complete scan is degraded. A healthy result
requires current list access and a successful complete scan. After access loss,
a restored permission probe remains degraded until another complete scan succeeds.
A complete empty scan is a valid success; denied access is never an empty success.

Run `consize discover -config PATH`, or queue a manual rescan through
`POST /api/discovery/runs` with an admin token and
`{"plugin_id":"kubernetes-action"}`. Inspect the returned operation ID and safe
run diagnostics. See [continuous discovery](continuous-discovery.md) for API usage,
scheduling, lifecycle thresholds and restart recovery.

## 4. Handle failures

Configuration errors fail during setup without exposing file contents or paths.
Request and scan errors use these safe classes:

- `configuration`: correct the explicit context, credentials source or verified
  HTTPS settings. No retry within the operation.
- `unauthorized`: renew the credential and check audience/expiry. Durable runs use
  `authentication`. No retry within the operation.
- `forbidden`: grant list on apps/deployments in the reported namespace, or narrow
  the configured namespace selection. Durable runs use `authorization`. No retry.
- `tls`: fix CA trust, certificate validity or server name. No retry.
- `network`, `timeout`, `server`: check connectivity, deadlines or API server
  availability. Retryable.
- `rate_limited`: retryable; honor Retry-After up to five minutes.
- `request`, `redirect_blocked`: correct the configured API endpoint or scope.
  Durable runs use `request`. No retry.
- `canceled`: caller cancellation; no automatic retry within that operation.
- Other malformed, oversized or incomplete scans stop safely. Unknown errors fail
  closed and retain no raw provider text.

Deployment-list calls disable client-go's per-request retries. The durable
scheduler owns the attempt budget: exponential backoff starts at five seconds,
caps at five minutes plus up to 20% jitter, and honors bounded Retry-After hints.
Configuration/access/TLS errors finish the operation instead of entering a retry
loop. A later scheduled interval or explicit manual rescan can test restored access.
Do not configure an unnecessarily short scan interval for a denied integration.

Consize does not retain raw API error bodies, HTTP warning text, server URLs,
continuation tokens, auth-plugin error output or credentials in discovery errors,
access diagnostics or audit failure codes. Credential executable behavior remains
under the operator's control. Existing annotation/label metadata rules still apply.
A failed, partial, denied or canceled scan cannot update absence history or mark
registered resources stale/deleted. Recovery requires a new complete matching scan.

## Verification and limits

Local unit/HTTP/registry tests cover credential separation, configuration errors,
401/403, TLS/network failures, throttling, timeout/cancellation, redirects, safe
errors, recovery, RBAC manifest contents and preservation of existing registry data.
CI additionally revokes and restores only a UID-guarded test RoleBinding in a newly
created random namespace inside its isolated loopback Kind cluster. Live fixture
execution requires an explicit test kubeconfig, approved Kind context and mutation
opt-in. No default kubectl context or production cluster is used.

No cloud credential provisioning, secret-manager integration, action RBAC,
enterprise credential rotation or managed-provider certification is introduced.
