# Kubernetes resource-to-Prometheus mapping

CON-18 replaces pod-name prefix attribution with `kubernetes-series-mapping-v1`.
The plugin reads dimension-preserving series, validates ownership, then performs
the existing workload reductions. Missing identity, ambiguity or membership data
produces unavailable evidence with a null value, not measured zero.

## Required source contract

Every source must carry an explicit cluster identity matching resource `account`
and namespace. A shared Prometheus must label each cluster distinctly. Consize
never treats a missing cluster label as the local cluster.

There are two supported modes:

- **direct** (default): metrics and `kube_pod_container_info` carry cluster,
  namespace, pod, pod UID, container, Deployment name and Deployment UID. A trusted
  exporter or recording/enrichment pipeline must obtain ownership from Kubernetes
  owner references. Do not derive it by splitting pod names.
- **owner_chain**: query `kube_pod_owner` and `kube_replicaset_owner` over the same
  interval and step. Each measurement must have exactly one active controller
  chain at its timestamp: Pod UID → ReplicaSet → Deployment name/UID. Metrics may
  omit pod UID; when present it must match the owner fact. Raw pod/container
  dimensions stay intact until mapping. Each owner fact must have value 1 and
  `owner_is_controller="true"`; conflicting/duplicate owners fail closed.

Standard kube-state-metrics supplies Pod UID and controller names, but its
ReplicaSet owner metric **does not supply Deployment UID**. `owner_chain` therefore
requires that additional `deployment_uid` label from a trusted owner-reference
exporter/enrichment pipeline. Consize does not manufacture it from names,
creation times or metric values. Without the extra identity data this mode returns
`deployment_uid_missing`; stock cAdvisor/KSM data alone is not claimed sufficient.
See the official [Pod](https://github.com/kubernetes/kube-state-metrics/blob/main/docs/metrics/workload/pod-metrics.md)
and [ReplicaSet](https://github.com/kubernetes/kube-state-metrics/blob/main/docs/metrics/workload/replicaset-metrics.md)
metric contracts.

The target resource must come from validated Deployment discovery/mapping: it
needs resource ID, Kubernetes provider/type, cluster, namespace/name, native UID,
and `metadata.mapping.containers`. Namespace and workload names alone are not
identity. Same-name Deployments in different clusters/namespaces cannot match.
A replaced Deployment UID is rejected, even if all names are unchanged.

## Configure label aliases

Add `mapping` inside your existing bootstrap `prometheus` object:

```json
{
  "prometheus": {
    "mapping": {
      "mode": "owner_chain",
      "cluster": "cluster",
      "namespace": "namespace",
      "pod": "pod",
      "pod_uid": "uid",
      "container": "container",
      "deployment": "deployment",
      "deployment_uid": "deployment_uid",
      "replicaset": "replicaset",
      "owner_name": "owner_name",
      "owner_kind": "owner_kind",
      "controller": "owner_is_controller"
    }
  }
}
```

These are the defaults; specify only overrides. Names must be distinct valid
Prometheus label names and cannot use reserved `__` prefixes. Invalid configuration
fails startup. The standalone plugin's `Mapping` object uses the same fields.
Configuration is copied at construction; no permissive compatibility switch exists.

## Collection and membership

All requests are scoped by exact cluster and namespace matchers with escaped values.
The plugin validates returned labels again; it does not trust query filtering alone.
The default queries preserve pod/container labels through rate/increase operations.
They no longer aggregate away identity on the Prometheus side.

`kube_pod_container_info` plus `kube_pod_init_container_info` form the membership inventory at each evaluation timestamp.
Metrics must match its pod UID/container set. Missing or extra members invalidate
the signal; duplicate scrape replicas are ambiguous and are not silently summed
or deduplicated. Configure upstream deduplication explicitly if needed. The
inventory must contain every expected container per pod. This is a mapping check,
not a guarantee that the upstream exporter observed every real Pod.

Empty containers, `POD` infrastructure containers and containers outside the
validated workload inventory are excluded. Regular sidecars and restartable init
sidecars listed by Deployment mapping are included. Ordinary init containers are
excluded. Rollout generations can coexist when their explicit owner facts identify
the same target Deployment UID; ownership is checked at every timestamp, not just
at the end of the window.

CPU/memory retain maximum per-pod container sums; restart/OOM and eviction signals
retain sums; throttling retains the maximum container ratio. Missing contributors
do not become zero. Container reductions happen only after mapping/membership
validation. This ticket does not redefine metric normalization or recommendation
eligibility. Custom queries must preserve these identity dimensions; `{scope}`
expands to validated cluster/namespace matchers. Old `{pod_regex}` templates and
already-aggregated unlabeled custom results are unsupported. Custom units still
must be explicit, and existing custom sample statistics remain unchanged.

Pure mapping is bounded to 10,000 input/owner series and 100,000 measurement/owner
points per invocation. Ownership indexes avoid per-point network requests and
quadratic joins. Existing HTTP body, query timeout and sample limits still apply.

## Diagnose and retain provenance

Each signal has `signals.mapping[SIGNAL]` containing version, status, source,
membership count, excluded count and digest. Every sealed evidence record carries
these mapping fields plus cluster, namespace and target Deployment UID. The digest
binds the mapping version, aliases, target identity, timestamped members, owner
facts and membership inventory; it is not a replacement for preserving exporter
configuration or a full source-series archive. Existing immutable evidence and
recommendation persistence retain these extensions across restart.

Examples of unavailable reasons:

- `identity_labels_missing`, `ownership_labels_missing`: required source labels absent.
- `cross_scope_series`, `deployment_instance_mismatch`: wrong cluster/namespace or UID.
- `pod_owner_missing`, `replicaset_owner_missing`, `deployment_uid_missing`: no proven chain.
- `ambiguous_series`, `ambiguous_pod_owner`, `ambiguous_replicaset_owner`: multiple matches.
- `incomplete_membership`, `inventory_timestamp_missing`, `inventory_incomplete_container_inventory`:
  measurement and inventory do not establish the same members.
- `no_matching_series`: no owned series; absence is not zero usage.

Mapping failures contain safe codes, not returned metric payloads. Transport/query
errors retain the existing safe error contract. Plugin health continues to report
connection readiness separately from per-resource evidence availability.

## Migrate and verify

1. Re-discover resources using CON-12/CON-14 mapping to obtain UID/container facts.
2. Provide exact cluster labels and the chosen UID-bearing source contract above.
3. Replace aggregated/prefix-based overrides with dimension-preserving queries.
4. Inspect mapping status and evidence quality before relying on recommendations.
5. Test cross-cluster/namespace collisions, replacement and rollout cases for your source.

The optional live lab test now requires `CONSIZE_LAB_CLUSTER` and
`CONSIZE_LAB_DEPLOYMENT_UID`, in addition to its original explicit opt-in. It never
infers them. Unit/golden/HTTP tests cover identity, aliases, ownership, ambiguity,
sidecars, unavailable evidence and recommendation persistence. CI's isolated
Prometheus server validates the raw PromQL syntax; this is not validation of a
particular production exporter/enrichment installation.
