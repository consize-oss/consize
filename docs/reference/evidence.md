# Evidence and provenance (CON-6)

`pkg/evidence.Record` is the provider-neutral measurement contract. `MetricsSnapshot.records` contains typed observations; `signals` remains a compatibility projection. The built-in headroom algorithm checks the configured resource, plugin, canonical unit, content identity, quality, freshness and projected value before generating a recommendation. Verification requires matching provenance and evaluates baseline freshness at collection time, so historical baselines do not become unusable solely because a verification window elapsed.

## Record contract

Each record includes schema version, content ID, resource ID, source plugin, signal, rendered query or selector, kind, aggregation, numeric value or null, canonical unit, original unit and normalization scale, sample count, closed observation window, collection time, freshness deadline and quality. Timestamps serialize in UTC. Window end must follow start and cannot exceed collection time. Freshness is evaluated without rewriting the historical record.

Kinds are `raw` (unaggregated observations), `aggregated` (for example p95 over a window) and `derived` (computed from other evidence; parent content IDs are required). The Prometheus collector emits aggregates, not fabricated raw samples. Its query text retains the server-side aggregation; the separate aggregation field describes the percentile/max across returned samples.

Quality is explicit:

- `valid`: sampled numeric observation, including measured zero; eligible only until `fresh_until`.
- `partial`: a numeric summary exists but coverage is insufficient; not eligible for new recommendations or successful verification.
- `stale`: a numeric observation exists but is already too old; not eligible.
- `missing`: no samples; value is null.
- `invalid`: unusable input; value is null.
- `unavailable`: source could not establish a measurement; value is null.

Missing/invalid/unavailable numeric values are rejected. A collection error still returns an error instead of manufacturing a successful snapshot. Historical reference-only records remain readable and are not backfilled with invented queries, units or quality. New built-in recommendations and verification cannot use reference-only metrics.

`Normalize` supports bytes/KiB/MiB, cores/millicores, seconds/milliseconds, count and ratio/percent, rejects unknown units and nonfinite values, and returns canonical value/unit plus scale. Producers preserve the original unit. Other plugins can explicitly define other units in their own records. Consumers must request the exact unit they understand. Prometheus uses bytes, cores, ratio and count for built-in signals; custom query keys require an explicit `prometheus.units` entry. Custom verification thresholds can declare `unit` on their check; built-in signals use their known canonical units. Credentials must never be placed in query text or extensions.

## Identity, ordering and persistence

`Seal` validates and detaches input maps/slices/pointers, normalizes timestamps, serializes the record with an empty ID using Go's deterministic JSON map ordering, and assigns `evidence:v1:` plus SHA-256. Changing resource, plugin, value, query, quality or metadata changes the ID. `Validate` checks the content digest. This detects accidental mutation, not malicious rewriting of both content and digest; it is not a signature. Producers sort records by signal/statistic; snapshot reference order is preserved exactly. Extensions allow at most 32 string entries, 128 bytes per key, 1,024 bytes per value and 8 KiB total. Derived records allow at most 64 parent IDs. Snapshots allow at most 256 records.

Recommendations contain an explicit algorithm version, ordered `evidence_refs`, and an embedded `evidence_snapshot`. The built-in algorithm copies the full typed input set, including coverage extensions, before returning it. Both stores validate snapshot IDs/resource ownership/reference correspondence before writing; reads detach mutable data. JSON-file state validates embedded snapshots on reopen. PostgreSQL stores the same record JSON in the recommendation payload, with no separate provider-specific table.

A later collection creates a new content ID and a new recommendation. Superseding or expiring a recommendation updates its lifecycle only; it never replaces its historical evidence. There is no mutable “latest evidence” row and no automatic evidence deletion. Embedded snapshots remain for the lifetime of their recommendation and its action/audit references, including restart, backup and restore. This deliberately favors explainability over long-term analytics retention; a future retention policy must account for all durable references before deleting data.

## Producer and consumer audit

- `pkg/plugins/prometheus`: emits rendered query provenance for p50/p95/p99/max for each configured signal; no sample summaries are emitted as zero when samples are missing. Coverage statistics are retained in bounded extensions. Below 90% coverage is partial; samples older than two collection steps are stale.
- `pkg/plugin`: transports records in built-in and packaged-plugin metrics snapshots and validates them at consumption boundaries. External plugins must implement this additive contract to supply inputs for new recommendations/verification.
- `internal/recommender`: consumes the selected memory signal, rejects unusable provenance and freezes all records plus algorithm version in the recommendation.
- `internal/verification`: validates provenance for every required/configured check; missing or mismatched records produce an inconclusive result, never success. Before/after snapshots retain provenance in durable results and jobs.
- `internal/store`: stores immutable recommendation inputs using the existing memory/JSON-file and PostgreSQL persistence paths. Existing reference-only recommendation creation remains supported for legacy integrations/demo paths; absence of a snapshot does not establish verified provenance.
- `internal/api`: generation responses expose source `records`, and dashboard recommendation responses expose `evidence_snapshot` and `evidence_refs`; OpenAPI and generated TypeScript describe the additive fields. No provider-specific field is added to the universal resource model.

## Validation

Run `go test -race ./pkg/evidence ./pkg/plugin ./pkg/plugins/prometheus ./internal/store ./internal/recommender ./internal/verification ./internal/api ./internal/safety`, then the full Go suite and API contract checks. Unit tests cover normalization, quality/null versus zero, canonical identities, cross-plugin provenance, mismatched projections, input isolation and stale refusal. Store tests cover replacement, concurrent writes, tampering and restart. The PostgreSQL integration test uses the existing disposable database setup in canonical CI. Existing Prometheus HTTP tests exercise real serialization and query execution against a local test server; they do not use production telemetry.
