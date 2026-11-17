# Continuous discovery and scoped reconciliation

Consize can repeat Kubernetes discovery through durable registry operations.
Provider calls happen outside storage transactions. A successful complete scan
commits observations, lifecycle changes, run completion and audit evidence together.
Failures commit no resource or absence changes.

## Configure

Use `database_url` or persistent `state_path`, configure Kubernetes discovery,
and add this section to the existing Consize configuration:

```json
{
  "discovery": {
    "enabled": true,
    "interval": "5m",
    "poll_interval": "5s",
    "timeout": "60s",
    "stale_after": "30m",
    "tombstone_after": "24h",
    "max_attempts": 4,
    "max_runs_per_tick": 10
  }
}
```

These are the defaults except `enabled`, which defaults to false. Kubernetes
namespaces, selectors and credentials stay in the existing `kubernetes` section;
this section controls scheduling and registry lifecycle only. See
[Kubernetes discovery setup](kubernetes-discovery.md).

`consize serve -config PATH` and `consize worker -config PATH` run the discovery
loop. In-memory storage cannot survive restart. The standalone worker requires
persistent storage. Use one active control plane; high-availability scheduling is
outside this MVP. PostgreSQL leases fence duplicate workers; the file store has
one process owner.

Interval is 1 second–24 hours; polling is 1–60 seconds; timeout is 1 second–4 minutes.
Stale delay can be zero; tombstone delay must be greater than stale delay and at
most 365 days. Durations use whole milliseconds. Attempts are 1–10 and the batch
limit is 1–100. A worker processes its bounded batch sequentially. Providers must
honor context cancellation and their own transport bounds.

## Manual rescans

Administrator authorization is required. With `CONSIZE_API_TOKEN` set to an admin
token, queue a rescan through the configured provider:

```sh
curl --fail-with-body http://127.0.0.1:8080/api/discovery/runs \
  -H "Authorization: Bearer $CONSIZE_API_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"plugin_id":"kubernetes-action"}'
```

HTTP 202 returns a durable run immediately. No provider call occurs in the HTTP
request. Only a configured plugin ID is accepted; callers cannot override scope,
credentials, endpoints, actor or lifecycle thresholds. The authenticated account
is recorded as actor.

Requests for the same provider/scope within one interval share an operation ID.
Another pending operation for the same scope returns HTTP 409. A completed failed
operation is also deduplicated for that interval; correct the configuration and
request in the next interval, or use the compatibility rescan below.

```sh
curl --fail-with-body \
  'http://127.0.0.1:8080/api/discovery/runs?id=RUN_ID' \
  -H "Authorization: Bearer $CONSIZE_API_TOKEN"
```

Use `plugin_id=kubernetes-action` instead of `id` for its latest 100 runs, or omit
both for the latest 100 globally. These reads require admin authorization and
return `Cache-Control: no-store`. The record includes scope/digest, attempts,
lease, next-run time, safe error code, scan completeness, resource IDs and counts
of stale/deleted transitions. Run health is `pending`, `running`, `healthy`,
`degraded` while waiting to retry, or `failed`. It describes that operation;
it does not assert application uptime or perpetual cluster access.

The existing `POST /api/discovery` and `consize discover -config PATH` still return
synchronous provider results. Scoped providers use the same durable exclusion,
validation and atomic registry path. Concurrent scope work returns a safe error
instead of starting another scan. Legacy Discover-only providers remain upsert-only:
they cannot prove authority, schedule periodic scans or reconcile absence.

## Scope and lifecycle rules

The supported authoritative descriptor binds plugin ID, cluster, location,
namespace selection and label selector. The scan must declare exactly that scope,
prove completeness, and contain only matching valid observations. The Kubernetes
plugin exposes this descriptor without contacting the API server.

Each accepted resource receives a core-owned authority marker. Absence processing
only considers resources last accepted under that exact authority. A scan cannot
mark another namespace, selector, cluster or plugin scope missing. Changing scope
does not grant authority over old-scope records; this can deliberately retain old
records until they are observed under a current scope or explicitly reviewed.
Records created by older upsert-only discovery are not absence-managed until a
complete authoritative scan observes them.

A first complete absence records its start time. After `stale_after`, another
complete scan can mark the resource stale; zero permits the first absence to do
so. Tombstoning requires elapsed `tombstone_after` and at least two complete
absence confirmations. Failed, partial, canceled, timed-out or access-denied scans
never advance these facts. Replayed completion and expired workers cannot count
as extra confirmations. A resource updated after scan start is excluded from
absence processing. Registry observation clocks and identity checks remain in force.

A tombstone means the resource is no longer observed in the selected discovery
scope. It is not proof that the Kubernetes object was physically deleted; removing
a matching label can also remove it from a selector. No Kubernetes deletion occurs.
Fresh observations can reactivate the same provider instance through the registry.
A new UID under the same logical name still requires the existing explicit identity
review; this ticket does not relax that protection or silently replace history.

Resources keep their stable IDs through ordinary metadata updates. The whole
batch is validated before committing; a conflicting or invalid observation aborts
all registry effects. Existing registry preparation and lifecycle rules are reused.

## Retry, recovery and diagnostics

The scheduler selects the current interval boundary with deterministic jitter up
to the smaller of 20% of the interval and 30 seconds. It does not backfill every
missed interval. Existing pending/retry runs resume after restart.

Transient unavailability, throttling and timeouts retry with exponential delays
starting at five seconds, capped at five minutes plus up to 20% jitter. Access,
configuration, incomplete-scan and identity conflicts stop without a hot loop.
Unknown plugin errors fail closed. [Kubernetes access handling](kubernetes-access.md)
defines safe authentication, authorization, TLS, network and server classes.
Deployment-list requests have no hidden client retries, and Retry-After hints are bounded.

A claim lasts timeout plus five seconds. On interruption the next worker waits for
lease expiry, records the interrupted attempt and repeats provider enumeration.
A stale worker cannot commit. Registry writes, completion and domain audit records
are atomic, so restarting cannot apply half a reconciliation or duplicate absence
confirmations. Attempts include interrupted claims and obey the captured budget.
Host and database clocks must be synchronized.

Inspect `error_code`, attempt history and scan metadata when a run fails.
`scope_changed` means the configured scope no longer matches the captured run;
`incomplete_scan` proves no complete snapshot was accepted; `registry_conflict`
requires checking identity/observation freshness. Raw provider errors, kubeconfig
contents and credentials are not copied into run errors or audit details.

Storage health includes a discovery-run count. Domain audit events identify
pending, running, retry and final transitions. Runs and registry absence history
are included in backup/restore and retained without automatic deletion. File
storage migrates schema 8 to 9; PostgreSQL adds migration 007. Stop old processes
before upgrading. Downgrade by restoring a compatible backup, not by editing the
migration ledger or dropping historical records.

## Verification

```sh
go test -race -p 1 ./internal/discovery ./internal/store ./internal/api ./pkg/plugins/kubernetes
```

Tests cover create/update/delete through the Kubernetes client adapter, scope
isolation, incomplete scan safety, concurrent claims, retries, expired-owner
fencing, reopened-store recovery, atomic registry/audit failures and API access
controls. Client-adapter tests use an explicitly identified fake API. Existing
Kubernetes live-cluster tests and the new PostgreSQL transaction tests run in CI's
isolated environments; no production cluster is touched or certified by these tests.
