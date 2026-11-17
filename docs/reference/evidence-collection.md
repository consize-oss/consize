# Durable evidence collection

Evidence collection has its own read-only queue. It does not create action jobs,
generate recommendations or change infrastructure. The API and periodic scheduler
use the same collector, fixed windows and persistence path.

## Configure and start

Use a persistent `state_path` or `database_url`, configure and register the
Prometheus plugin, and discover the Deployments first. In-memory storage is useful
for tests but cannot survive a process exit. The normal `consize serve` command
starts both the action worker and the collection worker.

Add this section to the existing Consize configuration:

```json
{
  "collection": {
    "enabled": true,
    "plugin_id": "prometheus-metrics",
    "interval": "15m",
    "window": "24h",
    "poll_interval": "5s",
    "timeout": "30s",
    "max_attempts": 4,
    "max_jobs_per_tick": 10
  }
}
```

These are the defaults except `enabled`, which defaults to false. Set the plugin
ID explicitly; if omitted, the configured recommender metrics plugin is used.
Window defaults to the Prometheus window, then 24 hours. Providers must support
fixed-window collection. Enabled scheduling fails startup if the provider is
missing or incompatible. Scheduling is limited to active, fully supported
registered Deployments; provider mapping still checks their underlying identity.

Durations use whole milliseconds. Interval is 1 second–24 hours; window is
1 millisecond–365 days; poll interval is 1–60 seconds; timeout is 1 second–4 minutes.
The provider's own query step, transport limits and unit settings still apply.
Choose a window long enough to contain multiple query steps. Attempts are 1–10;
work per tick is 1–100. Workers process that bounded batch sequentially, so
expensive providers can delay later jobs. This MVP supports one active control
plane; leases fence duplicate workers but do not provide distributed scheduling.

## Request and inspect a collection

An operator token is required when authentication is enabled. Store the token in
`CONSIZE_API_TOKEN`; do not paste it into configuration or logs.

```sh
curl --fail-with-body http://127.0.0.1:8080/api/evidence/collections \
  -H "Authorization: Bearer $CONSIZE_API_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"resource_id":"REGISTERED_RESOURCE_ID"}'
```

The response is HTTP 202 with a durable operation, initially `pending`. Collection
runs asynchronously. No credentials, queries, endpoints, plugin overrides or
attempt budgets can be supplied in this request. The authenticated account is
recorded as the actor.

Omitting `evaluation_at` selects the current interval boundary. An explicit
RFC3339 timestamp must be in the past or present and aligned to milliseconds.
The start is that end time minus the configured window. Repeating the same
resource incarnation/mapping, provider configuration, signal set and window returns the existing
operation, including an already completed operation. A different overlapping
window while equivalent work is pending returns HTTP 409. Changing provider
configuration creates a different identity; unfinished operations captured under
the previous configuration fail with `instance_changed` rather than using new
settings silently.

```sh
curl --fail-with-body \
  'http://127.0.0.1:8080/api/evidence/collections?id=OPERATION_ID' \
  -H "Authorization: Bearer $CONSIZE_API_TOKEN"
```

Use `resource_id=REGISTERED_RESOURCE_ID` instead of `id` for the latest 100
operations for that resource; neither filter returns the latest 100 globally.
All reads require operator authorization and return `Cache-Control: no-store`.
Unknown filters and duplicate query parameters are rejected.

## Results and failure handling

Each operation captures resource scope, provider configuration digest, required
signals, evaluation window, attempts, timestamps, lease state and attempt history.
The configuration digest is a hash; provider configuration and credentials are
not copied into the operation. Raw provider errors are never stored or returned.

States are `pending`, `running`, `retry_wait`, `succeeded`, `degraded` and `failed`.
A completed snapshot is stored atomically with completion and an evidence audit
event after universal-contract, resource, provider and exact-window validation.
`succeeded` requires the ten MVP signals to be present, all returned records to
be valid with values, and the reporting quality assessment to pass. Missing,
partial, stale or quality-blocked data stays `degraded`; its inspectable records
remain available. A failed provider call stores no snapshot, even if the plugin
returns partial data alongside the error. No numeric zero is invented.

Reporting success is not permission to recommend or execute a change.
Recommendation and verification quality gates remain independent. Current
Prometheus sources may not establish raw scrape continuity, so collection can
correctly remain degraded even when query-evaluation coverage is complete.
See [signal semantics and provenance](prometheus-evidence.md).

Transient Prometheus/network failures and timeouts retry with exponential delays
starting at five seconds, capped at five minutes plus up to 20% deterministic
jitter. Access, query and configuration failures stop immediately. Unknown plugin
errors fail closed. Attempts include interrupted claims and never exceed the
captured budget. Diagnostics expose only fixed error classifications such as
`provider_access`, `provider_query`, `provider_timeout` and `invalid_evidence`.

A lease lasts the collection timeout plus five seconds. An expired worker cannot
commit a result after another worker claims it. Shutdown leaves unfinished work
claimed until lease expiry, then the next process resumes the same operation and
window. A provider must honor context cancellation and its transport bounds.
Provider reads can repeat after a crash; durable completion and evidence cannot
be committed twice. No database transaction spans a provider call.

## Scheduling, retention and operations

The scheduler selects the current interval boundary with deterministic jitter up
to the smaller of 20% of the interval and 30 seconds. It resumes existing pending
work after restart and schedules the current bucket; it does not backfill every
missed interval. Buckets selected late can legitimately produce stale evidence.
Synchronized host/database clocks are required. The queue has a PostgreSQL due
index; audit events identify pending, running, retry and terminal transitions.
GET operations expose quality diagnostics and safe failure codes. Storage health
reports a collection count; collection failures do not rewrite old evidence.

Automatic evidence deletion remains disabled by default (`retention: "0s"`).
Optionally set collection `retention` to 24 hours–365 days to remove standalone
snapshot/assessment payloads after completion. Operations retain identity,
status and attempt history as idempotency tombstones. Any recommendation or
action referencing the resource conservatively pins all of its collections.
Embedded workflow evidence and action/audit history are never removed by this
policy. Backup/restore includes the queue and snapshots. The file schema migrates
from version 7 to 8; PostgreSQL adds migration 006. Stop old processes before
upgrading, and restore a compatible backup to roll back the file format.

If a job stays running, check its lease expiry and whether the worker is running.
If it fails with `provider_access` or `provider_query`, correct provider settings
and request a new operation. If it is degraded, inspect the assessment and
individual records before changing sampling or source configuration. Storage
failures return a safe API error or stop the worker; repair storage before restart.

## Verification

```sh
go test -race -p 1 ./internal/collection ./internal/store ./internal/api
```

Tests cover scheduling/deduplication, overlapping windows, concurrent claims,
lease expiry, interrupted recovery, finite retry budgets, missing/partial
signals, safe error handling, API authorization, configuration changes, retention
and atomic evidence/audit writes. PostgreSQL tests run with
`CONSIZE_TEST_DATABASE_URL` against an isolated schema; CI creates its own pinned
PostgreSQL container. Tests use explicitly named fixture data. They are not
production exporter qualification; the complete live pipeline lab is CON-20.
