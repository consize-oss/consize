# Local lab review and verification

Recommendations propose staged request reductions of at most 25% per change.
Memory limits remain unchanged. Reservation reductions are not billing savings.
Confidence uses evidence history, coverage, freshness and peak-safety checks.
See `internal/recommender/confidence.go` for the current eligibility rules.

Review Change prepares a dry-run plan. Apply requires a separate confirmation.
The backend repeats planning and reads current deployment values at execution.
Preparation events remain in the full action API and durable audit; the dashboard
activity feed shows execution and verification events instead.

API execution requires enabled verification, a valid baseline, and a metrics
plugin with explicit time-window support. Missing metrics produce an inconclusive
result rather than passing. CPU increases from a zero baseline are not skipped.

The local Prometheus queries include 5-minute CPU rates and 30-minute restart
lookbacks. Verification waits 30 minutes to isolate those inputs, then observes
the configured interval (5 minutes in the lab). This is a conservative basic
check, not a complete SLO, load-cycle, or realized-savings guarantee. The worker
also checks rollout readiness and live state throughout the observation period.

Registry entries, recommendations, action audit, original state, baselines,
verification policy, deadlines, retries and final results are persisted together
under `state_path`. Writes use a private temporary file, file fsync, atomic rename
and directory fsync. A lifetime file lock enforces one active owner. Corrupt or
unwritable state stops execution rather than silently reverting to memory.

The API and CLI execution paths use the same durable safety controller. The old
orchestrator is restricted to dry-run review. API apply queues a durable job with
HTTP 202; the worker performs mutation only after intent is persisted. The worker
resumes unfinished jobs on restart and reconciles live state before retrying.
Repeated submissions for the same recommendation return the existing job. A
resource can have only one active or unresolved job.

Kubernetes plans capture the immutable Deployment UID, workload configuration,
exact original container resources and exact target resources. Apply and rollback
are idempotent, use optimistic concurrency, and refuse configuration or identity
drift. Restoring the old request through a proportional reverse calculation is
not used. Memory limits remain unchanged even for imported recommendations.

`rollback_on_failure` and `rollback_on_timeout` are explicit configuration
settings captured with each action. The local configuration enables both. Missing
or stale metrics retry until the captured deadline; they never count as a pass.
Successful restoration must pass state, readiness and isolated metrics checks
before the job becomes `rolled_back`. Failed restoration or external drift becomes
`manual_intervention` and keeps the resource blocked against further actions.

The selected recommendation shows execution state, deadlines, retry errors and
verification results. Review & Retry Recovery requires another explicit approval.
It can retry original-state restoration after an outage, but cannot override drift.
The full durable audit is available through `/api/actions`; jobs and evidence are
available through `/api/jobs`. JSONL output remains an additional review export;
the durable store is the authoritative execution audit.

This is a single-process local OSS store, not a distributed worker database. Keep
the state file and its directory on a local filesystem with working fsync and
locking, back it up, and keep `state_path` stable across restarts. Run either the
API worker or the CLI worker for a given state file; two owners are rejected.

To resume pending work after interruption, restart the API with the same configuration:

```bash
go run ./cmd/consize serve -config examples/local-kind.config.json -demo=false -addr 127.0.0.1:8080
```

The API starts this worker automatically. Verification deadlines are absolute:
an outage beyond the deadline follows timeout policy on startup; it does not
silently extend the verification period. Timing tests use a controlled clock;
the isolated local Kubernetes integration test exercises actual apply and rollback.

Prometheus storage in this lab is ephemeral. This configuration is for local
testing, not production deployment.
