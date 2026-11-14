# Resource inventory assessments

`GET /api/resources/inventory` is the backend read model for the Resources page.
It requires the existing viewer role. The existing `/api/resources` registry API
is unchanged. This endpoint does not call providers, execute actions, run
discovery, or calculate financial values.

## Inputs and assessment rules

- Registry records, recommendations and durable jobs are read together: under
  one read lock in the memory/file store, or one read-only `REPEATABLE READ`
  transaction in PostgreSQL. No new persistence schema is introduced.
- Telemetry comes from sealed evidence records already persisted with
  recommendations or job baseline/verification snapshots. Collection that has
  not persisted evidence is not visible here. CON-21/22 own collection and its
  future dedicated evidence read interface; this endpoint does not start a
  second collector. Legacy evidence strings and `metadata.inventory` assertions
  cannot establish reporting.
- Evidence must match the resource and a currently registered metrics provider,
  validate its content identity, and satisfy the existing verification signal
  contract (CPU, memory, restarts and configured checks). Each required signal
  must have the correct unit, valid samples and unexpired freshness. A measured
  zero remains usable. Sparse providers are never combined into a fictional
  complete observation. New partial or invalid readings do not inherit an older
  reading's usable status. Equal-time conflicting observations remain unknown.
- `usable` means the required measurements are usable, not application uptime
  or sufficient history/confidence to optimize. `stale` means trustworthy
  measurements have aged or are explicitly stale. `unmonitored` means no metrics
  capability is configured for this resource type, or the provider explicitly
  recorded missing required measurements. Missing provenance, provider
  unavailability, invalid or partial measurements remain `unknown`.
- `last_measurement_at` is the latest successful persisted measurement window
  end for the selected source. It is not discovery `last_seen_at`. No provider
  endpoint, query, credential or measurement payload is added to the response.
- The current foundation policy applies globally, so valid resources reference
  `oss-foundation-v1`. This is matching coverage, not approval. The coverage
  reader keeps `covered`, confirmed `uncovered` and `unknown` distinct so scoped
  policy resolution can replace it without rewriting inventory composition.
- Supported actions require a registered action plugin for the resource type
  with both planning and execution capabilities and at least one action type.
  This conveys support only: policy, approval, preflight and execution controls
  still apply. Provider health is a separate concept.
- Missing, blank or case-insensitive `unassigned` ownership is returned as a
  null assessed owner. The underlying registry resource remains unchanged.
- Active recommendation IDs include pending, planned, approved and executing
  records, excluding superseded or expired records. No recommendations is not
  proof that no optimization is needed.

## Counts, filtering and pagination

Filters: provider, account, region, type, source_plugin_id, environment,
lifecycle_state, owner, search, telemetry, policy and actions. Search matches ID,
name, account, assessed owner and labels. Deleted records are excluded by default;
an explicit lifecycle filter can select them. `owner=unassigned` selects missing
ownership. Unknown, duplicate, empty, oversized or invalid filters are rejected.

All those filters affect summary and tab counts. `tab`, `limit` and `cursor` do
not affect summary counts. `filtered_total` follows the selected tab before
pagination. All/Attention/Unmonitored counts are `summary.discovered`,
`summary.attention` and `summary.unmonitored` respectively. Attention counts each
resource once even when it has several reasons. Unmonitored overlaps Attention.
Per-reason counts can overlap. Unknown assessments have their own attention
reasons and are never counted as confirmed policy gaps. Supported action
percentage is null for an empty inventory or unavailable capability assessment,
rather than a fictitious measured 0%.

Rows and counts use one `assessed_at`, `assessment_version` and `snapshot_id`.
Results are ordered by resource ID. Follow `next_cursor` with the same filters;
changing the page size is allowed. A signed cursor fixes the assessment time for
up to five minutes. Later pages re-read and compare the assessed content. If it
changed, the API returns **409** and the client restarts from page one. Expiry
also returns 409. A fresh first-page request recomputes freshness and sees new
policy/capability/evidence inputs. Forged or cross-filter cursors return 400.
Cursors are process-bound; restart or a different replica rejects them with 400,
so clients restart pagination. No mutable multi-page cache is retained.

## Operational limits and validation

Each source collection is limited to 10,000 records and the combined serialized
input to 32 MiB. Exceeding either returns **503**, never incomplete totals. Reads
have a ten-second request deadline, page size is 1–200, and responses are
`Cache-Control: no-store`. Storage errors are sanitized. These limits bound the
initial OSS read model; larger installations need an indexed assessment store
rather than increasing these limits without capacity testing.

Run `go test -race ./internal/inventory ./internal/store ./internal/api ./internal/policy`.
Set `CONSIZE_TEST_DATABASE_URL` to an isolated PostgreSQL test database to include
the transaction/concurrent-refresh suite. In CI, the existing store harness starts
the pinned PostgreSQL image automatically. The canonical OpenAPI schema and
`ui/lib/generated/api.ts` define the response; UI integration is CON-41/42 and
layout/gap-review work remains CON-74.
