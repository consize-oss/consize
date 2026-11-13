# Frontend runtime integration (CON-41)

## Run locally

Use the Node/npm versions pinned in `.node-version` and `ui/package.json`.
Install dependencies from the repository root:

```sh
npm --prefix tools/ci ci
npm --prefix ui ci
```

Start the real API using `DEVELOPMENT.md`. Start the UI separately:

```sh
NEXT_PUBLIC_USE_MOCKS=false NEXT_PUBLIC_CONSIZE_API_BASE_URL=http://127.0.0.1:8080 npm --prefix ui run dev -- --hostname 127.0.0.1 --port 3030
```

Set the API URL before building a production UI; Next embeds the public URL at
build time. Invalid URLs, credentials in URLs, query strings and fragments are
rejected. The API must allow the UI origin. This browser setup targets the
loopback OSS API; it does not store or inject bearer credentials. A protected
API returns 401/403 and consequential controls remain unavailable until an
appropriate authenticated deployment integration is provided. Never put secrets
in `NEXT_PUBLIC_*` variables.

## Data and commands

`src/lib/api-client.ts` is the common transport. It validates responses with Zod,
retains HTTP errors and request IDs when available, combines cancellation with a
30-second timeout, and never automatically retries a mutation. Query retries are
bounded. The presentation adapters in `src/lib/runtime-api.ts` read the existing
OpenAPI-backed runtime endpoints; they do not call the design-only `/api/v1` API.

- `/api/dashboard`: inventory joins, recommendations, plugins and configured policy.
- `/api/resources` and `/api/discovery`: resource inventory and explicit sync.
- `/api/recommendations/generate`: explicit evidence evaluation of one resource.
- `/api/recommendations/{id}/plan`: dry-run policy/preflight review.
- `/api/recommendations/{id}/execute`: explicit confirmed approved execution.
- `/api/recommendations/{id}/recover`: separately confirmed original-state recovery.
- `/api/jobs`: persisted action/verification state and deadlines.
- `/api/actions`: action and verification history.

Summary, list, policy and plugin observers share the runtime-dashboard cache.
Filters select from the complete fetched dataset. Job polling runs every five
seconds only while the selected job is nonterminal, and stops on unmount or a
terminal state. Reload reads persisted jobs again. A command response loss triggers
a job refresh; it never silently resubmits. The backend repeats policy, baseline,
preflight and drift checks; reviewing a plan never authorizes a later mutation.

## Supported screens and limits

Overview, Resources, Recommendations/detail, Plugins, Policies, Approvals and Audit
read real runtime data. Approvals opens the filtered recommendation queue. Audit
shows recorded action/verification events; the full domain audit API remains
available separately. Reports, global search, workspace/account switching,
notifications, marketplace and configuration commands have no implemented UI
backend and stay unavailable with explanations.

Monetary totals, period comparisons, personal assignments, package trust,
telemetry freshness and policy coverage are unavailable unless explicitly supplied.
Connectivity does not establish usable telemetry or signature verification.
Unpriced recommendations display a dash, not a zero-dollar saving. Date filtering
is disabled because the runtime provides current state and recorded history, not
windowed summaries. The Policies screen shows the configured foundation policy;
it does not invent an editable policy inventory, exception counts or coverage.

The production runtime never imports fixture rows. Explicit development MSW
scenarios remain isolated and use their own design API; they do not represent
real-runtime integration validation.

## Validation

```sh
npm --prefix ui test
npm --prefix ui run typecheck
npm --prefix ui run lint
NEXT_PUBLIC_USE_MOCKS=false NEXT_PUBLIC_CONSIZE_API_BASE_URL=http://127.0.0.1:18080 npm --prefix ui run build
go build -o bin/consize-smoke ./cmd/consize
node tools/ci/node_modules/@playwright/test/cli.js install chromium --only-shell
npm --prefix ui run test:e2e
```

Playwright starts a disposable real API with no plugins and a production UI.
It blocks service workers, tests disconnection/recovery, and registers its resource
fixture through the supported resource API. The matrix covers five core screens
at 390, 768, 1280, 1440 and 1920 pixels in light/dark themes. Axe checks WCAG A/AA
rules; overflow assertions, screenshots, traces and keyboard Escape/focus checks
supply additional evidence. CI retains reports and screenshots for seven days.
These screenshots are review artifacts, not approved visual-diff baselines.

The automated browser matrix uses Chromium. Firefox/Safari, populated high-volume
performance, approved visual baselines, and the full browser-driven live
apply/verification-failure/rollback/restart loop still require qualification.
The backend's separate live qualification does not substitute for those browser
checks. Keep CON-41 open until that remaining release evidence and independent
review are completed. Passing the empty-state suite alone is not alpha release
approval.
