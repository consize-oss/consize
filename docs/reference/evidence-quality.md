# Evidence quality

`evidence-quality-v1` assesses whether observations support reporting,
recommendation generation or post-change verification. Quality is separate from
recommendation confidence, action risk, policy and approval.

## Inspect an assessment

Call `GET /api/evidence/quality?resource_id=<id>&purpose=recommendation` with an
operator or administrator bearer token. Purposes are `reporting` (default),
`recommendation` and `verification`. The API uses the configured workflow's metrics
plugin and a 10-second context deadline. Plugins must honor cancellation. Responses
use `Cache-Control: no-store`; errors exclude provider credentials and payloads.
Callers cannot select an endpoint, query or provider. This API does not create jobs
or write inventory evidence.

The response includes a version, purpose, assessment time, effective thresholds,
overall state, eligibility, examined evidence IDs and per-signal dimensions. Each dimension has a stable
reason code, pass/fail/unknown status, mandatory flag, and nullable observed/required
values; count dimensions also expose the expected total. **Null means unknown; numeric zero means observed zero.** Failed or unknown
mandatory dimensions make `eligible` false. Overall state precedence is invalid,
unavailable, missing, stale, partial, unknown, valid. Inspect dimensions for all
reasons rather than inferring them from the state.

## Default requirements

- Valid sealed identity, available nonnegative measurements, fresh collection and
  actual source observations, timestamped coverage, at least two samples, 90% of
  expected query points, and no gap over three query steps.
- Recommendation history: 24 hours, or seven days for production/high-criticality
  resources, with at least 24 observation intervals. The existing lab profile
  permits 30 minutes only for non-critical development resources.
- Recommendations and verification: exact mapping, stable series membership,
  unchanged replicas, complete replica/container coverage and known scrape
  resolution. Recommendations also require sufficient workload age and history
  since its last change, plus an observed maximum no greater than twice p95.
- Reporting exposes topology limitations without requiring complete topology.
  Exact mapping and actual source freshness remain mandatory for usable data.
- Verification assesses historical snapshots at collection time so the baseline
  does not expire during the post-change wait. Before/after identities and
  chronology must match. Controller checks still enforce the isolated post-change
  window. Quality failures return `inconclusive`, never `passed`.

Optional `quality` settings under `recommender` and `verification` configure
`min_history`, `max_age`, `minimum_coverage`, `minimum_samples`,
`maximum_gap_steps` and `maximum_peak_ratio`. Omitted/zero numeric settings select
defaults. Durations use Go syntax, for example `168h`, not `7d`. Bounds reject
malformed/nonfinite values at startup: history 0–365 days (at least 30 minutes for
recommendations), age >0–24 hours, coverage 0.5–1, samples 2–1,000,000, gap steps
1–100 and peak ratio 1–100. Configuration cannot disable provenance, mapping or
required topology checks. Lowering thresholds does not provide missing evidence.

## Producer contract

`MetricsSnapshot.AssessQuality` adapts immutable records to the pure model in
`pkg/evidence/quality`. Bind these facts to each sealed record:

- Existing extensions: `step`, `expected_points`, `actual_points`, `max_gap`,
  `first_sample`, `last_sample`. Times use RFC3339Nano and durations use Go syntax.
- `quality_context_v1`: JSON from `quality.EncodeContext`. Fields are `mapping`
  (`exact`, `ambiguous`, `unknown`), `cardinality_stable`, `replica_count_changed`,
  `expected_replicas`, `observed_replicas`, `expected_containers`,
  `observed_containers`, `resource_created_at`, `resource_changed_at`,
  `scrape_interval`, `observed_samples`, `first_observation_at`,
  `last_observation_at` and `source_max_gap`.

Counts describe expected and fully observed members throughout that signal's
window, not current discovery counts. Cardinality compares series membership,
not only series count. Mapping follows actual resource identity.
For aggregates, `last_observation_at` must conservatively represent the oldest
latest source observation among required members. It is not the HTTP
request time or query evaluation timestamp. Omit unproven facts; do not invent
values from names, registry first/last-seen clocks or current replicas.

Invalid or unknown context fields fail provenance checks. JSON must fit the
existing 1024-byte extension budget. Seal after adding extensions; later changes
invalidate the content ID. Hashes detect changes, not malicious producers:
configured plugins remain a trust boundary.

The adapter validates sealed identity and numeric projections. Mutable legacy
coverage cannot supply missing sealed facts; conflicting projections fail closed.
Expected query points are `floor(window / step) + 1`, with unique observed points
and consistent counts. Query steps and source scrape intervals are assessed
separately: dense evaluation grids do not establish independent raw observations.
`observed_samples` counts distinct source observation instants with the required
members covered, not repeated query points or a sum across members. Source coverage
compares that count with `floor(window / scrape_interval) + 1`, applying the same
minimum coverage and sample thresholds. Source boundaries must span the window
within two scrape intervals; source gaps must not exceed the configured number of
scrape intervals. Unknown source facts block eligibility. This also detects outages
hidden by repeated query results.
Observed peaks describe history and do not predict future demand.

## Workflow behavior and limits

Generation rejects mandatory failures before creating a recommendation and returns
HTTP 400 with `error` and structured `quality` diagnostics. Successful recommendations
retain `quality_assessment` beside their immutable evidence in parameters.
Verification results retain both assessments. The execution controller reassesses
baseline quality, including for legacy recommendations.

Current Prometheus aggregation removes pod/container identity and does not supply
historical membership, workload changes or actual source timestamps. These remain
**unknown** and block actionable eligibility until a producer supplies observed
facts. No permissive metadata fallback is provided. Provider transport and
resource discovery/mapping collection remain in CON-17/CON-19.

The resource inventory uses this same reporting-purpose assessment over persisted
records, with dimensions under `assessment.telemetry.quality`. It never queries
providers. Its existing usable/stale/unmonitored/unknown statuses and counts derive
from these gates. Conflicting records and mixed collection windows cannot form a
complete observation. Last-measurement time comes from a successful underlying
source observation, rather than query evaluation time, resource last-seen or plugin
reachability. This change adds no collection scheduler or frontend redesign.

References: Prometheus [query API](https://prometheus.io/docs/prometheus/latest/querying/api/)
and [query evaluation](https://prometheus.io/docs/prometheus/latest/querying/basics/).

## Request example

With the API running locally and an operator token in `CONSIZE_TOKEN`:

```sh
curl --fail-with-body \
  -H "Authorization: Bearer ${CONSIZE_TOKEN}" \
  'http://127.0.0.1:8080/api/evidence/quality?resource_id=checkout-api&purpose=recommendation'
```

Use the resource ID returned by `/api/resources`. HTTP 200 means an assessment was
produced; read `eligible` and the mandatory dimensions to determine whether it
passed. A successful assessment does not grant permission to execute a change.
