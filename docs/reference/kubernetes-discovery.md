# Kubernetes Deployment discovery

CON-12 adds scoped, bounded full scans to the existing Kubernetes discovery plugin.
It collects Deployment observations; core code validates and persists them. It does
not start a scheduler, reconcile missing resources, query metrics or execute changes.

## Configure the source

Merge this object into your normal bootstrap JSON:

```json
{
  "kubernetes": {
    "enabled": true,
    "kubeconfig": "/run/secrets/consize-kubeconfig",
    "context": "approved-observation-context",
    "cluster_id": "cluster-one",
    "location": "local",
    "discovery": {
      "namespaces": ["payments", "orders"],
      "label_selector": "consize.io/observe=true",
      "page_size": 200,
      "max_objects": 10000,
      "max_bytes": 33554432,
      "timeout": "30s"
    }
  }
}
```

Use a kubeconfig whose credentials can read only the intended namespaces. Select
`context` explicitly when using a shared kubeconfig. If it is omitted, the current
context **inside that explicitly supplied file** is used. Consize never selects the
global kubectl configuration automatically. With no `kubeconfig`, it uses in-cluster
service-account configuration; a context override then is invalid. Credentials and
configuration contents are excluded from setup and discovery errors.

`cluster_id` and `location` are your stable registry identity coordinates. Keep them
consistent with the selected cluster. Changing a discovery selector does not change
canonical resource IDs. Namespace allowlists and Deployment label selectors filter
collection, not Kubernetes permissions. Selectors apply to `metadata.labels` on
Deployments, not the Pod template. No Pod/ReplicaSet queries or identity guessing
are added here.

Empty/omitted `namespaces` retains the existing all-namespace behavior and requires
cluster-wide Deployment-list permission. Set an explicit allowlist for a restricted
identity. Each namespace must be a valid DNS label; blanks, wildcards and duplicates
are rejected. The allowlist is sorted so order does not change scope identity.
Selectors use Kubernetes equality/set syntax and are parsed before collection.

The standalone Kubernetes executable preserves its existing configuration names
(`Kubeconfig`, `ClusterID`, `Location`), adding `Context` and `Discovery`. Its discovery
settings use the same snake-case properties shown above. Compiled discovery plugins
support the snapshot interface described below; the existing executable loader's
action/metrics registration is not extended into a new discovery lifecycle here.

## Bounds and failure behavior

Defaults and supported bounds:

- Namespace allowlist: at most 128 entries.
- Label selector: at most 4 KiB.
- `page_size`: 200, configurable from 1–1,000.
- `max_objects`: 10,000, configurable from 1–100,000.
- `max_bytes`: 32 MiB of serialized native observations retained during a scan,
  configurable from 64 KiB–256 MiB.
- `timeout`: 30 seconds, configurable from 1 millisecond–5 minutes.
- Total pages: at most 1,000 across the scope. Each production Deployment-list
  response is capped at 16 MiB, including decompressed bodies. This wrapper applies
  only to Deployment lists; get/patch/action transport behavior is unchanged.
- Captured metadata: at most 64 KiB per Deployment; bounded label, annotation,
  container and owner-reference counts. Limits fail the scan rather than truncate it.

The provider follows each opaque continuation token without widening selectors or
namespace scope. Resource versions must remain consistent across pages within a
namespace. Duplicate identities/UIDs, out-of-scope objects, selector mismatches,
continuation cycles, invalid pages and expired tokens fail explicitly. An expired
continuation does not silently restart against a different snapshot.

Multi-namespace scans have separate Kubernetes resource versions per namespace;
they are not a globally atomic view of the cluster. One active scan is allowed per
plugin instance; a concurrent request gets `scan_busy`. Parent cancellation and scan
timeouts propagate through listing, paging and transformation. A canceled empty
response cannot become a completed empty scan.

## What is captured

Canonical logical identity remains unchanged. [Deployment mapping](kubernetes-mapping.md)
defines metadata precedence, explicit unknown environment, per-container quantities
and the native UID replacement guard.
`resource.metadata.discovery` adds the versioned native input contract
`kubernetes-deployment-discovery-v1`:

- Cluster identity, namespace, name, UID, resource version and creation time.
- Deployment labels, Pod-template labels, Deployment and template annotation names.
- Owner references, selectors, requested replica count, actual/ready/updated/available
  replica counts, generation and observed generation.
- Regular and init container names, restart policy and original Kubernetes resource
  request/limit quantities as strings. Canonical unit conversion and aggregation
  changes are not introduced here.

All extension values are plain JSON; Kubernetes API types do not cross into core.
No environment values, command arguments, volumes, condition messages or full
manifests are stored. Annotation values are omitted by default and the omitted count
is explicit. To capture known public annotations, configure `annotation_allowlist`,
for example `["consize.io/team"]`. At most 16 qualified keys are allowed, with values
at most 4 KiB. Credential-bearing keys and last-applied manifests cannot be opted in.
The operator must classify allowed values as public metadata; arbitrary annotations
are not a secret store. Existing labels remain infrastructure metadata and must not
contain credentials. Missing optional native facts remain null/absent, not invented.

Each resource also carries `metadata.discovery_scan.id` and `scope_digest` to tie it
to the provider attempt. A per-resource marker never authorizes absence handling.

## Complete scans and the API

The additive SDK `SnapshotDiscoveryPlugin.DiscoverSnapshot` returns a
`DiscoverySnapshot` containing scan metadata and observations. `Discover` remains
available for existing callers. `POST /api/discovery` retains administrator
authorization and adds an optional `providers[].scan` response containing:

- Version `discovery-scan-v1`, unique attempt ID and source plugin ID.
- Canonical scope and SHA-256 scope digest.
- Start/finish times, duration, count, page count and per-namespace snapshot versions.
- Explicit completeness and a safe error code where applicable.

A successful empty scan has `complete: true` and `count: 0`. A failed or partial
scan has `complete: false` and emits no observations to the registry, even if some
pages/namespaces were read. The core rejects malformed/incomplete snapshots and
validates the entire observation batch before starting resource writes. A later
storage failure still is a storage failure: provider enumeration is not an atomic
registry transaction, and no completion response is returned for failed ingestion.

Legacy Discover-only plugins remain supported, but have no authoritative scan
metadata. An empty legacy list must never be treated as proof of absence. No
absence reconciliation or resource deletion is wired to this endpoint; that
lifecycle work remains separate.

## Inspect health and fix failures

`GET /api/plugins` adds no new permissions. Existing generic health now includes
`health.details.discovery`: `in_progress`, `last_attempt` and
`last_successful_scan`. A failed attempt does not erase a previous successful scan.
These diagnostics are runtime state; after restart, unknown history is null, not a
fabricated empty scan. Resource scan IDs already persisted with observations remain.

API reachability and discovery success are separate: a successful version probe
cannot hide an incomplete/denied scan. A running/incomplete scan is degraded, a
failed API probe is unhealthy, and a completed scan may be healthy even when empty.
A provider with no scan yet says so. Health probes honor cancellation and are
bounded by the configured scan timeout or 15 seconds, whichever is smaller.

Example before the first scan (illustrative health fields):

```json
{
  "status": "healthy",
  "message": "Kubernetes API reachable; discovery has not run.",
  "details": {
    "discovery": {
      "in_progress": false,
      "last_attempt": null,
      "last_successful_scan": null
    }
  }
}
```

After a scan, the attempt/success objects contain the scan ID, declared scope,
completeness, timestamps, duration, count and safe error code described above.

Safe failure codes:

- `configuration`: check namespace/selector syntax, bounds, context and kubeconfig.
- `unauthorized`, `forbidden`: correct credentials/RBAC and selected namespaces.
- `timeout`, `canceled`: check API availability and request deadlines.
- `unavailable`: the client/API could not establish a trustworthy result.
- `expired_continue`: retry a new full scan explicitly; never treat the partial scan
  as absence evidence.
- `invalid_page`, `invalid_observation`: investigate inconsistent versions,
  duplicate identities, missing UID or out-of-scope responses.
- `limit_exceeded`: narrow the scope or review a bounded limit; no partial set was
  converted into a complete scan.
- `scan_busy`: another full scan is active; retry after it completes.
- `unsupported_client`: the supplied provider client cannot perform paged discovery.

Errors/diagnostics omit API bodies, URLs, auth-plugin output and continuation tokens.

## Least-privilege access

[The example Role and RoleBinding](https://github.com/consize-oss/consize/blob/main/examples/rbac/kubernetes-discovery.yaml)
grant only `list` on `apps/deployments` in one namespace. Adapt the namespace and
dedicated service-account subject before applying. Repeat the binding only for
approved namespaces. No Pod, Secret, mutation, watch or cluster-admin permissions
are required by the scan. Kubernetes's normal authenticated API-discovery grant
provides the existing `/version` health probe; hardened clusters may need a separate
`get` permission for that non-resource URL. Do not grant unrelated resources to fix it.

Use a separate read-only discovery identity. A Role adds permissions and cannot
remove other grants held by an administrative kubeconfig. The shared existing
Kubernetes action capability does not acquire mutation rights through this role.
Label selectors are not an authorization boundary.

## Verification

Run `go test -race ./pkg/plugins/kubernetes ./internal/discovery ./internal/api`.
Unit/HTTP tests cover namespace/label scope, paging, empty success, access denial,
continuation expiry, cancellation, duplicate identities, response/retention bounds,
concurrent attempts, safe native golden output and registry refusal of partial data.

CI creates a fresh checksum-pinned kind 0.33.0 cluster with a digest-pinned Kubernetes
1.37.0 node. Its private kubeconfig is separate from user configuration. Tests create
zero-replica fixtures in unique owned namespaces (no workload-image pulls), verify
same-name identities and selectors/paging, and exercise list-only service-account
access with a forbidden second namespace and no patch/Secret access. Cleanup deletes
only owned namespace UIDs and the exact CI-created cluster, including after failure.
This is a test control plane, not a deployable Consize image or release asset.

The live test requires explicit isolated-target configuration and fixture opt-in;
it refuses non-loopback targets and does not use your current context. Missing CI
prerequisites fail the check. Workstation runs without them skip this live stage;
a skipped stage is not live-cluster validation. The older mutation/rollback tests
retain their independent opt-ins and are not invoked by this discovery test.

References: Kubernetes [API list/pagination semantics](https://kubernetes.io/docs/reference/using-api/api-concepts/),
[label selectors](https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/),
and [the pinned kind release](https://github.com/kubernetes-sigs/kind/releases/tag/v0.33.0).
