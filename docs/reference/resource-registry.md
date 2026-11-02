# Durable resource registry (CON-8)

`pkg/resource.Registry` defines create, upsert, lookup, complete listing, filtered pages, lifecycle updates and scope-bound reconciliation without exposing a storage engine. The memory/JSON-file and PostgreSQL implementations use the same identity, lifecycle, cursor and reconciliation rules.

## Registration and identity

`CreateResource` succeeds only for a new resource. Repeated creation returns `ErrAlreadyExists`. `UpsertResource` creates or updates one immutable provider identity: provider, account, region, resource type and provider resource ID. Resource type is included in the PostgreSQL identity index by migration 004, matching the universal model.

Existing first-seen and creation timestamps survive updates. A successful new observation advances last-seen; older observations are rejected with `ErrStaleObservation`. The source plugin cannot silently change ownership of an existing resource. Metadata, labels, ownership and current state remain mutable. Support status is derived from the resource type, including model-only and unsupported types. All resources pass universal schema validation at the write boundary. Reads return detached data.

Legacy noncanonical IDs remain stable when the same resource is rediscovered under a canonical ID. This preserves all recommendation, evidence, action and audit references. Identity changes under an existing ID return `ErrIdentityConflict`.

## Lifecycle and complete scans

Resources are active, stale or deleted (a tombstone). No registry operation removes a resource or deletes anything in a provider account.

A lifecycle update and every reconciliation must declare the exact provider, account, region, resource type and source plugin. All five fields are required. Unknown/out-of-scope IDs, duplicate IDs or contradictory observation/deletion lists reject the whole reconciliation before any change is saved.

`ReconcileResources` represents one **complete successful scan of that exact scope**:

1. Persist current provider observations with `UpsertResource`.
2. Supply their IDs in `ObservedIDs`, any explicit deletion confirmations in `ConfirmedDeletedIDs`, and the scan observation time.
3. Active resources reported present advance their presence clock without rewriting configuration. Missing resources become stale. Explicit deletion confirmations create tombstones. Missing resources from another source/account/region/type/provider are unaffected.

An empty complete scan marks that scope's resources stale, never deleted. Failed or partial provider scans must not call reconciliation. The current discovery runner still only upserts observations; continuous discovery and complete-scan scheduling remain CON-15. This ticket supplies the registry boundary without guessing empty-scan scope from a plugin's returned resources.

`lifecycle_observed_at` records the latest accepted lifecycle/presence observation, separately from `observed_at` (configuration observation) and `last_seen_at` (confirmed presence). Reconciliation does not advance last-seen for absent resources. Older scans cannot overwrite newer presence or lifecycle decisions. Zero lifecycle clocks in existing state remain compatible; the next write establishes one. Lifecycle requests reject zero times and times more than one minute ahead of the registry clock.

A deleted resource cannot become stale. A fresh upsert can reactivate a stale/deleted resource with the same identity; a delayed pre-deletion observation cannot resurrect it. Historical records remain readable after either transition. There is no automatic tombstone retention/deletion policy in this MVP.

## Filtering and pagination

`ListResourcePage` orders IDs by byte order, with default limit 100 and maximum 200. Filters are exact matches for provider, account, region, type, source plugin, environment, lifecycle, support status and owner. Cursors bind the last ID to the complete filter set; malformed or mismatched cursors return `ErrInvalidQuery`. The limit may change between pages.

Pages are a live keyset view, not a frozen multi-page snapshot. Updates and new resources can appear between requests; new IDs before the continuation point are seen on a new traversal. No offset is used. PostgreSQL applies bound filters and the limit in SQL with C collation, matching Go's ID order.

`GET /api/resources` with no query parameters preserves the existing full `{resources: [...]}` response used by the Resources screen. Supplying a filter, cursor or limit returns `{resources, next_cursor, has_more}`. For example:

```text
/api/resources?provider=kubernetes&lifecycle_state=active&limit=50
```

Use the returned `next_cursor` with the same filters. Unknown, repeated or empty query parameters and invalid limits/cursors return HTTP 400. No reconciliation/lifecycle mutation endpoint is exposed by this change; existing administrator authorization for resource registration is retained.

## Concurrency, persistence and failure behavior

Memory/JSON-file mutations hold the registry mutex and publish an operation atomically. Failed persistence restores the affected records and identity entries. Existing durable-store health/error handling remains in force.

PostgreSQL registry writes acquire a schema-specific transaction advisory lock before inspecting records, including absent IDs. This serializes create/upsert/lifecycle/reconciliation decisions and prevents simultaneous first inserts from bypassing immutable identity, first-seen or stale-observation checks. Reconciliation commits all changed resources in one transaction. The lock favors correctness over parallel write throughput at MVP scale; reads remain independent.

Migration 004 replaces the older four-coordinate unique constraint with an identity index that also includes resource type. It runs in the existing transactional, checksummed migration framework. Previous migration files are unchanged. JSON state adds only an optional lifecycle timestamp and does not rewrite existing resource IDs or references.

Errors support `errors.Is`: `ErrNotFound`, `ErrAlreadyExists`, `ErrInvalidResource`, `ErrIdentityConflict`, `ErrStaleObservation`, `ErrInvalidTransition`, `ErrInvalidQuery` and `ErrScopeMismatch`. Driver, disk and context errors are returned rather than reported as success.

## Validation

The shared memory/PostgreSQL contract tests cover idempotence, first/last-seen, source isolation, tombstones, reappearance, delayed scans, pagination/filter binding, concurrent first inserts and legacy IDs. JSON restart tests retain historical recommendation/action/evidence references. API tests preserve the legacy response and verify pages and invalid inputs. Run:

```bash
go test -race ./internal/store ./internal/api ./internal/discovery ./pkg/resource
CI=1 go test -race ./internal/store -count=1
```

The second command uses the existing disposable PostgreSQL container harness and requires Docker. It creates only synthetic local test data and removes the container afterward.
