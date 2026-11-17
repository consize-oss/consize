# Prometheus evidence collection

The Kubernetes Prometheus provider collects CPU usage, memory working set,
CPU throttling, restart increases, OOM increases and eviction observations.
It also collects current CPU and memory requests and limits. Deployment identity
and membership checks follow [the mapping contract](prometheus-mapping.md).
Missing or ambiguous identity produces unavailable evidence, never guessed ownership.

## Queries and aggregation

`prometheus.QueryCatalog(mapping, cluster, namespace)` returns the rendered,
sorted query definitions without making requests. The checked-in
`pkg/plugins/prometheus/testdata/query-catalog-v1.json` fixture captures the catalog.
Versions are `kubernetes-promql-v1`, `deployment-aggregation-v2` and
`nearest-rank-v1` for queries, reduction and percentiles respectively.

At each evaluation timestamp, CPU usage and memory are summed across a pod's
regular containers and restartable init sidecars, then the largest owned pod
value is selected. Percentiles are calculated over those timestamp values, not
by adding container percentiles. Throttling uses the largest container ratio;
restart, OOM and eviction signals sum mapped members. Sorted, compensated sums
make reductions independent of response ordering. Ordinary init containers and
infrastructure containers are excluded by the mapping contract.

Range signals expose p50, p95, p99 and maximum. A configured percentile adds its
own pN value while retaining p95. Percentiles use nearest rank. Restart evidence
uses reset-adjusted Prometheus `increase` over a rolling 30-minute interval.
It can be fractional because Prometheus extrapolates; it is not rounded or summed
across overlapping windows to manufacture a period total.

Requests and limits are queried once at the evaluation time and expose only
`*_current`. Each value is the maximum per-pod sum of declared container values,
not a usage percentile, fleet total or scheduler-effective Pod budget. Pod
overhead and ordinary-init scheduling maxima are not included. Absent values
remain unavailable; an absent limit does not mean zero or unlimited capacity.

## Configuration

Bootstrap `prometheus` configuration and standalone plugin configuration accept:

```json
{
  "base_url": "https://prometheus.example.invalid",
  "window": "24h",
  "step": "5m",
  "percentile": 95,
  "minimum_samples": 2
}
```

Existing transport authentication, TLS and mapping settings still apply. Window
and step must use whole milliseconds. Window is at most 365 days; step is between
1 millisecond and 24 hours and cannot exceed the window. Percentile is 1–99;
minimum_samples is 2–100000. Zero selects the defaults shown above. Minimum sample
requirements apply to range evidence; current configuration requires one sample.
These settings do not relax core evidence-quality or recommendation gates.

Every custom `queries` entry must declare its source unit in `units`, including
an override of a built-in signal. For example, a CPU query returning millicores
uses `"units": {"cpu_cores": "millicores"}`. Values are normalized before
reduction; the original unit and scale are retained. Built-in units cannot be
relabelled without overriding the query. Unknown custom signals require a single
mapped series because no multi-series aggregation semantics have been declared.

## Quality and provenance

Explicit time intervals are narrowed to contained millisecond bounds; original
requested bounds are retained. Default collection aligns its end before
subtracting the configured window, preserving the full configured duration.
Current records share the snapshot window for identity but carry
`sample_kind=current_configuration` and their evaluation time.

Each sealed record includes the query and digest, versions, units, normalization
scale, mapping digest, step, sample count, expected evaluations, gaps, discarded
points, evaluation time and actual completion time. Records pass the universal
contract validation before being returned for persistence. Identical input and
window produce identical values and query identity; real collection timestamps
can change record IDs.

Duplicate, non-finite, negative or incomplete contributors cannot silently lower
a result: affected evaluations are discarded and quality records the failure.
Missing, partial, stale and unavailable results remain distinguishable. Sample
coverage counts query evaluations, not raw exporter scrapes. Existing raw-source
history and topology gates still determine whether evidence supports an action.

## Verification

Run the regular Go race suite. For actual PromQL evaluation and isolated
scrape-to-collector tests, first install the checksum-pinned test tools:

```sh
scripts/ci/install-prometheus-test.sh /tmp/consize-prometheus-test
CONSIZE_PROMETHEUS_BIN=/tmp/consize-prometheus-test/prometheus go test -race -p 1 ./pkg/plugins/prometheus
```

Promtool fixtures evaluate full query histories, including counter resets. The
isolated server test scrapes known CPU, memory, requests and limits and checks
normalized evidence. That short test overrides the CPU lookback to two seconds;
the production five-minute query is covered by full-history Promtool fixtures.
These tests do not qualify a production exporter's UID enrichment or scrape history.
