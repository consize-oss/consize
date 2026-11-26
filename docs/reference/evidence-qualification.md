# Real-source evidence qualification

CON-20 qualifies discovery, Prometheus reads, sealed evidence, quality gates and
durable collection together. The suite creates a disposable Kind cluster and
runs same-name Deployments in two namespaces, with two replicas and two containers.
The containers vary real CPU work and resident memory. No usage measurements are
seeded into Consize or Prometheus.

## Run locally

Install Docker, Go matching `.go-version`, and Kind 0.33.0. Start the local Docker
engine, then run from the repository root:

```sh
scripts/test/evidence.sh --output /tmp/consize-evidence-run
```

Use a new output directory each time. The script uses the repository's pinned
multi-platform Kind node image and downloads checksum-verified Prometheus 3.16.0
for Linux amd64 or macOS arm64. It checks that Docker uses a local Unix endpoint.
It never selects the current kubectl context or deploys into an existing local
cluster. Kubeconfig is private and removed when the owned cluster is deleted.

For a longer source-observation window:

```sh
CONSIZE_EVIDENCE_LONG_PROFILE=1 scripts/test/evidence.sh --output /tmp/consize-evidence-long
```

The default window is two minutes; the longer profile uses five minutes. Both use
three-second scrapes and ten-second query steps. The disposable node uses
one-second kubelet housekeeping, configured in `scripts/test/evidence-kind.yaml`,
so the short run observes real changes between scrapes. They qualify the plumbing and
source-quality behavior, not production seasonality or recommendation eligibility.
Existing deterministic Promtool tests remain responsible for full-history query
math, resets and exact aggregation results.

To reuse the **current isolated CI run's** cluster and pinned Prometheus binary,
CI can run `scripts/test/evidence.sh --reuse-ci`. This path requires the exact
GitHub run context and explicit fixture opt-in; it is not a production-cluster
reuse option.

## What supplies the measurements

The fixture proxy forwards actual kubelet `/metrics/cadvisor` output unchanged.
Its separate metadata endpoint reads Kubernetes APIs and exports actual Pod,
ReplicaSet and Deployment UID relationships, resource requests/limits, restart
counts and observed phases. Missing fields are omitted, not converted to zero.

Test queries extract Pod UID from the native cgroup ID, then join against the
API-observed UID inventory. Workload names alone never establish ownership.
This is a small qualification bridge, not a new production exporter. A production
source still needs the [documented UID enrichment](prometheus-mapping.md).

For each discovered resource, expect CPU usage, memory working set, requests,
limits, restarts, throttling, OOM increases and eviction observations. Every record
includes resource/plugin identity, actual query/version/digest, normalized unit,
window, sample counts, coverage, quality and source-observation facts. Real zero
counters are valid observations; absent measurements have null values.

## Source-quality configuration

Query evaluation points do not establish independent exporter scrapes. The
Prometheus provider accepts these additional configuration fields:

- `source_scrape_interval`: the upstream observation cadence, such as `15s`.
- `source_scrape_intervals`: optional per-signal overrides for different sources.
- `source_timestamp_queries`: a map from base signal to an identity-preserving
  PromQL query whose **values** are the original source sample timestamps.

For a source that already carries the required UID labels, a CPU timestamp query
can use `timestamp(container_cpu_usage_seconds_total{{scope},container!="",container!="POD"})`.
For enriched sources, apply `timestamp()` to the raw selector **before** joining
metadata. `timestamp()` around an arithmetic/recording expression can describe
query evaluation time instead and must not be presented as an exporter timestamp.
Queries use the existing `{scope}` substitution and must retain the same validated
resource/pod/container identity as the measurement.

The provider deduplicates source timestamps per member, conservatively intersects
coverage boundaries, uses the smallest independent sample count and largest gap,
and binds the resulting quality context and source-query identity into sealed
records. Source sampling is at least twice per configured scrape interval where
query bounds allow. Missing, malformed, mismatched or insufficient observations
remain unknown/unusable; query-step timestamps are never substituted.

Without this configuration, source continuity stays unknown and mandatory source
quality gates remain closed. Do not change thresholds to hide that absence.
Use `source_scrape_intervals` for per-signal cadence overrides when sources differ.
The fixture preserves original kubelet timestamps. It configures the disposable
node to collect observations faster than Prometheus scrapes; it does not replace
native timestamps with query-evaluation timestamps. Production cadence must match
the actual exporter and scrape configuration.

## Assertions and failure cases

The suite checks separate same-name resource identities, replica/container
membership, finite measured usage, all MVP units/provenance and eligible reporting
quality. It exercises fractional window bounds and lets real evidence expire
without rewriting its timestamps.

Missing metrics and a CPU rate lookback shorter than the scrape cadence must fail
quality gates. A malformed query reaches the real Prometheus parser. A local
authorization boundary rejects a client credential and verifies that no credential
enters persisted diagnostics. These failures must not create a snapshot containing
measured zero usage.

Scheduled and manual requests for one captured window must share an operation.
The test closes/reopens durable Consize state with an interrupted lease, checks
recovery and preserves discovered identities. Finally it stops the actual
Prometheus process, observes a retryable collection failure without a snapshot,
restarts the same TSDB and verifies recovery of the captured operation.

## Results and cleanup

The output directory contains `test.log`, `cluster.log`, `cluster-name.txt` and
`summary.json`. A successful summary has `passed: true` and
`cleanup_verified: true`. The latter confirms fixture namespace deletion with UID
preconditions; the shell runner separately checks owned-cluster deletion.
The summary includes a real sample snapshot, not a fabricated example payload.
Never publish kubeconfig, private credentials or complete environment dumps.

CI runs this qualification in the backend unit and race suites, using the owned
Kind cluster and pinned Prometheus already provisioned for backend integration.
It uploads the final real-source report as `evidence-qualification-<run>-<attempt>`.
A skipped local test is not a live qualification result.

If workloads do not start, inspect the labelled fixture Pod statuses and image
pull events through the explicit generated kubeconfig/context. If measurements
are missing, inspect mapping status and the pinned kubelet metric availability.
If records exist but quality is blocked, inspect source timestamps/counts/gaps
and actual scrape cadence before considering any policy changes.

Cleanup runs on success or failure. If it fails, the runner returns nonzero and
keeps the generated cluster name/log so the same explicit disposable cluster can
be removed. It does not remove unrelated clusters, images or user workloads.
