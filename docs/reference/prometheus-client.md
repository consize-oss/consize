# Prometheus client and connection configuration

The Prometheus plugin owns HTTP, authentication, TLS, query parsing, retries and
provider diagnostics. Core workflows consume plugin evidence; they do not build
Prometheus requests. This document covers CON-17 transport behavior, not mapping,
PromQL aggregation, evidence scheduling or recommendation policy.

## Configure the connection

For a local unauthenticated server, keep the existing bootstrap configuration:

```json
{
  "prometheus": {
    "enabled": true,
    "base_url": "http://127.0.0.1:9090",
    "window": "24h",
    "step": "5m"
  }
}
```

Merge this object into your normal Consize configuration. It does not configure
Kubernetes, workers or recommendation execution. Connection success alone does
not establish resource mapping or usable evidence.

For an authenticated remote service, use HTTPS and credential references:

```json
{
  "prometheus": {
    "enabled": true,
    "base_url": "https://metrics.example.net/prometheus",
    "window": "168h",
    "step": "1m",
    "transport": {
      "auth": {"bearer_token_env": "PROMETHEUS_TOKEN"},
      "tls": {"ca_file": "/run/secrets/prometheus-ca.pem"},
      "header_env": {"X-Scope-OrgID": "PROMETHEUS_TENANT"},
      "request_timeout": "10s",
      "query_timeout": "8s",
      "retry_attempts": 2,
      "retry_backoff": "250ms"
    }
  }
}
```

Inject secret environment variables through your secret manager or mount secret
files. Do not put values in JSON, URLs, PromQL, shell history or diagnostic output.
Omit `tls.ca_file` when the server uses a system-trusted certificate. URL prefixes
are supported; endpoints are appended beneath the prefix.

Authentication choices are mutually exclusive:

- Bearer: `bearer_token_env` or `bearer_token_file`.
- Basic: `username_env` plus `password_env` or `password_file`.
- Mutual TLS: `tls.cert_file` and `tls.key_file`, optionally combined with bearer
  or basic authentication. Both certificate and key are required.

`tls.server_name` explicitly selects the certificate name when connecting through
an IP or alias. Certificate chain and hostname verification remain enabled; there
is no insecure-skip-verify setting. TLS 1.2 is the minimum. Protect private keys and
credential files with restrictive permissions and mount them read-only. Files must
be regular files; CA/cert/key reads are bounded at 2 MiB and credential reads at
64 KiB. Missing/empty references fail setup. Secrets are read at construction;
restart or reconstruct the plugin to rotate them.

Optional headers use `header_env` references only. At most 16 headers are allowed,
with values at most 8 KiB. Header names are validated and case-insensitive duplicates
are rejected. Authorization, cookies, host, hop-by-hop and framing/content headers
cannot be overridden. Plain HTTP credentials are permitted only on loopback for
local development. Use HTTPS for authenticated remote connections.

The standalone `prometheus-metrics` executable accepts the same `transport` object
in plugin configuration and rejects unknown fields. Its execution environment must
make the referenced secrets available under the plugin runtime's existing
permissions; this change does not widen those permissions.

## Time, size and retry limits

All durations use Go syntax, such as `250ms` or `10s`.

- `request_timeout`: default 10 seconds; must be positive and at most 2 minutes.
  It covers the entire operation, including retries, waits and body reads. Health
  readiness and query probes share one such budget.
- `query_timeout`: default 8 seconds, reduced to the request timeout when smaller;
  explicit values must be positive and no greater than the request timeout. This
  is also sent to Prometheus as its server-side query timeout.
- `max_response_bytes`: default 4 MiB; range 1 KiB–16 MiB. Applies to response
  bodies regardless of Content-Length. Response headers are capped at 32 KiB.
- `max_series`: default 1,024; range 1–4,096.
- `max_samples`: default 100,000; range 1–1,000,000 across a response. Requested
  evaluation points cannot already exceed that limit.
- `retry_attempts`: total attempts, default 1; range 1–3. Retryable network,
  rate-limit and server failures use bounded exponential backoff.
- `retry_backoff`: default 250 milliseconds; range 1 millisecond–5 seconds.
  `Retry-After` on 429/503 is honored. If waiting would exceed the operation budget,
  return the classified failure instead of retrying early or waiting indefinitely.

Authentication/authorization failures, invalid configuration/queries, malformed
responses and unsupported samples are not retried. Parent cancellation stops
requests and backoff. The SDK preserves `errors.Is` cancellation/deadline semantics
without retaining a raw URL-bearing network error.

Queries are bounded at 8 KiB. Collection windows are at most 365 days; steps are
1 millisecond–24 hours and cannot exceed the window. Range bounds use PR #79's
millisecond rounding **inside** the caller's interval; returned milliseconds are
preserved. Subsecond steps are sent as fractional seconds.

## Destination and response safety

URLs must be absolute HTTP/HTTPS URLs without userinfo, query strings, fragments,
encoded path separators or dot traversal. Redirects are rejected. DNS results are
validated at connection time; unspecified, multicast, link-local and known cloud
metadata destinations are blocked. Private and loopback addresses remain supported
for self-hosted Prometheus. Mixed allowed/blocked DNS results fail closed.

The configured transport dials validated resolved addresses directly and does not
use environment proxies. Set an approved directly reachable endpoint; proxy support
is not implied. `NewHTTPClient` retains its explicit trusted-Go HTTP-client injection
hook for tests/custom integrations; its caller owns that injected transport's DNS,
TLS and proxy policy. It still enforces request/response bounds and redirect refusal.

Successful query responses must be UTF-8 JSON with the documented result type.
Instant queries support matrix, vector, scalar and string results; range queries
require a matrix. Float values and timestamps are validated. NaN/Inf, malformed
pairs, duplicate series/timestamps, out-of-window range points and unsupported
native histogram samples fail explicitly. Empty results remain empty; they never
become measured zero. Exceeding a limit fails rather than truncating results.

`QueryRangeResult` and `QueryInstant` return typed results and normalized warning/info
annotations. Raw annotations can contain labels or query text, so diagnostics expose
safe codes and counts instead. Warnings mark the result degraded. The existing
`QueryRange` evidence path rejects warned responses with `partial_response`; it
cannot silently create clean evidence. Informational annotations remain visible
without automatically declaring the response partial.

## Inspect health and errors

`GET /api/plugins` retains its existing authorization and response shape, adding
optional `health.details.provider`. It includes provider state, last request and
success times, latest safe error, annotation counts and attempt count. Before any
request the SDK state is `configured`; successful requests are `reachable`, warned
responses `degraded`, and failed requests `unavailable`. Times describe provider
requests, including probes, not evidence freshness. Diagnostics are concurrency-safe
and an older slow request cannot overwrite a newer request's state.

Health checks readiness (`/-/ready`) and a harmless instant query. They do not
persist probe results as evidence or claim that workload metrics are available.
The generic plugin health remains healthy/degraded/unhealthy; inspect provider
state and error codes for the reason. Credentials, headers, query text, response
bodies, URLs and raw provider errors are excluded from diagnostic/error payloads.

Failure codes and next steps:

- `configuration`: check URL, duration/size bounds, secret references and TLS files.
- `endpoint_blocked`, `redirect_blocked`: configure the intended permitted endpoint
  directly; do not relax destination checks.
- `authentication`, `authorization`: correct credentials or read permissions.
- `tls`: check the CA, hostname, expiry and client certificate requirements.
- `network`, `timeout`, `canceled`: check connectivity and configured/parent deadlines.
- `not_ready`: Prometheus is not ready to serve queries.
- `rate_limited`, `server`, `http_status`: inspect service availability and limits;
  any suggested retry delay is reported without the provider body.
- `query`, `query_canceled`: inspect the query in the provider's protected tooling.
- `invalid_response`, `invalid_result_type`, `invalid_sample`, `unsupported_sample`,
  `unsupported_encoding`: correct the endpoint/format/sample type; do not substitute zero.
- `response_too_large`, `response_limit`: narrow the query/window/cardinality before
  raising a bounded limit.
- `partial_response`: inspect warnings in protected provider tooling and repeat
  collection once the response is complete.
- `provider_failure`: the provider did not establish a trustworthy result.

## Validate locally

Run `go test -race ./pkg/plugins/prometheus`. For the official-server integration:

```sh
scripts/ci/install-prometheus-test.sh /tmp/consize-prometheus-test
CONSIZE_PROMETHEUS_BIN=/tmp/consize-prometheus-test/prometheus \
  go test -race ./pkg/plugins/prometheus -run TestPrometheusServerIntegration -count=1 -v
```

The installer requires curl, Python 3 and tar. It pins Prometheus 3.16.0 and SHA-256 digests for Linux amd64 and macOS
arm64. This is a test tool, not a deployment artifact or production version policy.
The test starts an isolated loopback server with no external scrape targets, bounded
WAL storage and a temporary TSDB, then stops it and removes its data. Delete the
separate downloaded test binary directory afterward if no longer needed. CI installs
the pinned binary and fails if that integration prerequisite is missing.

The older live workload/PromQL tests retain their explicit opt-ins and prerequisites;
this transport integration does not claim to qualify a Kubernetes evidence pipeline.
[Evidence quality](evidence-quality.md) still requires source/membership facts that
transport connectivity alone cannot provide.

References: Prometheus [HTTP API](https://prometheus.io/docs/prometheus/latest/querying/api/),
[readiness API](https://prometheus.io/docs/prometheus/latest/management_api/), and
[official release](https://github.com/prometheus/prometheus/releases/tag/v3.16.0).

Kubernetes series attribution and source-label requirements are specified in
[resource-to-series mapping](prometheus-mapping.md). Transport readiness does not
bypass that contract.
