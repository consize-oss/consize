# Kubernetes Deployment mapping

`MapDeployment` is the pure, network-free mapping entry point used by Deployment
discovery. Its contract version is `kubernetes-deployment-mapper-v1`. Kubernetes
objects stay inside the provider; returned extensions contain only JSON values.
Core validates observations and owns canonical IDs and registry timestamps.

## Identity and replacement

The existing canonical ID remains derived from provider, cluster, location, kind
and `namespace/name`. Mutable labels, replicas, generation and requests do not
change it. The mapper requires an explicit cluster/location, valid namespace/name
and nonempty UID. UID is preserved as both `metadata.uid` and
`metadata.provider_instance_id`, an incarnation guard separate from logical ID.

Registry upserts reject a changed or missing known instance with
`resource.ErrIdentityConflict`, including an existing record that only has legacy
Kubernetes `uid`. Conflicting UID/instance fields are invalid. A rejected update
leaves the old observation/history intact. This applies to memory, durable-file and
PostgreSQL stores. A first observation of an older record with no native instance
can establish it; subsequent observations must preserve it.

Replacing a Deployment under the same namespace/name is therefore an explicit
conflict, even after a tombstone. It does not silently inherit old references or
create a new canonical ID scheme. Stop actions for that resource, review its
recommendations/jobs and follow a deliberate registry migration/reset procedure
for the local alpha deployment. This mapper does not implement automatic
replacement reconciliation or delete history. Do not clear UID metadata to bypass
the guard. Kubernetes explains the distinction between [object names and
UIDs](https://kubernetes.io/docs/concepts/overview/working-with-objects/names/).

## Metadata precedence

For each field use the first nonblank Deployment label in this order:

- Environment: `consize.io/environment`, `environment`, then `unknown`.
- Owner: `consize.io/owner`, `owner`, then `unassigned`.
- Criticality: `consize.io/criticality`, `criticality`, then `medium`.

Environment and criticality values are trimmed and lowercased in core fields;
owner values are trimmed. Original label keys/values remain unchanged in `labels`
and native metadata. A nonblank invalid primary value fails mapping rather than
silently falling through to a weaker label. No namespace-name, Pod-template,
annotation or workload-name heuristics infer ownership or environment.

`metadata.mapping.provenance` records the selected `label:KEY` or `fallback` and
whether it is confirmed. `unknown` environment and `unassigned` owner are always
unconfirmed, including explicitly supplied sentinel values. Unknown environment is
accepted by the resource model and registry/inventory filters. It is never treated
as development. Foundation auto-apply requires a known staging/development value
and a real owner; explicit approval retains its existing governed path.

Native labels, safe annotations, owner references, selectors, replicas, generation
and provider creation time remain in `metadata.discovery`, as described in
[discovery](kubernetes-discovery.md). Annotation values still require the existing
public-key allowlist. Native creation time does not replace registry first-seen or
creation clocks. Unknown native creation time remains null.

## Containers and quantities

`metadata.mapping.containers` is ordered by kind and name. Each regular/init
container has its name, kind, optional restart policy, and separate request/limit
lists. Each entry records resource name, canonical unit, exact decimal `value` as a
string, and the original canonical Kubernetes `quantity` string:

```json
{"resource":"cpu","unit":"millicores","value":"0.1","quantity":"100u"}
```

CPU uses millicores. Memory, ephemeral storage and hugepages use bytes. Extended
resources retain their names and use units. Decimal strings preserve precision
through JSON and storage; clients must not convert them to floating point for
identity or safety decisions. Missing requests/limits have no entry; an explicitly
observed zero has value `"0"`. Invalid/negative quantities, aggregate overflow,
duplicate containers and requests above a supplied limit fail mapping.

Existing `current_state` CPU/memory integer totals remain compatible: sum regular
containers per replica, round individual fractional base units up as Kubernetes's
integer quantity helpers do, and do not multiply by replica count. Init containers,
including restartable init sidecars, remain separate. These compatibility totals
are not Kubernetes scheduler-effective Pod requests. Pod-level resources and
scheduler accounting are not implemented by this mapper.

`metadata.mapping.aggregate_complete` states whether each regular-container total
has every contributing request/limit. A partial total must not be interpreted as a
fully configured Pod budget. The container list distinguishes missing fields from
zero even when the older aggregate field is zero. Recommendation/metric aggregation
policy is outside CON-14; no new eligibility claim is made by these totals.

See Kubernetes [container resource units](https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/).

## Errors and verification

`MappingError` has stable safe codes and unwraps to `resource.ErrInvalidResource`.
Codes distinguish missing UID/cluster identity, invalid metadata, duplicate/missing
containers, negative/overflowing quantities, request-over-limit and bounded metadata
failures. Errors do not embed input values or native payloads. Discovery preserves
the typed cause under its safe `invalid_observation` scan failure and emits no
partial batch.

Tests cover the [versioned golden fixture](https://github.com/consize-oss/consize/blob/main/pkg/plugins/kubernetes/testdata/deployment-mapping.json),
metadata precedence, unlabeled workloads, sidecars, exact quantities, mutable identity
stability, cross-scope collisions, UID replacement rejection and extension persistence
across restart. Store contract tests run the instance guard against all backends;
CI supplies the isolated PostgreSQL and Kubernetes services. No Prometheus ownership
mapping, scheduler, UI redesign or action capability is added by this change.
