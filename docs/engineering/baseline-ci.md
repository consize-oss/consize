# Baseline CI (CON-58)

This replaces PR #51 from the current main branch. Risk class: F (CI and supply chain).

## What runs

- `Backend quality`: tracked Go formatting without edits, vet, unit tests and full race tests. Race tests are separate from ordinary tests so concurrency bugs are exercised with instrumentation; no test failure is retried.
- `Frontend quality`: locked install, generated declarations checked before build, ESLint with no warnings, TypeScript, API-client tests using Node’s built-in runner and production build. Next 16 does not provide `next lint`.
- `Browser smoke`: production UI → real Go API, empty temporary durable state, no demo seeds, no service workers. Verify real API health/resource reads, visible UI contract errors, navigation, theme persistence, filter reset, reload and browser errors. The UI currently calls unimplemented `/api/v1/...` routes while the API serves `/api/...`; this smoke explicitly records the gap instead of claiming an integrated dashboard happy path. Resolving it is required before release qualification. This is a read-only smoke check, not Kubernetes/Prometheus lifecycle qualification.
- `CodeQL (go)` / `CodeQL (javascript-typescript)`: public repository analysis, redacted normalization and the existing security policy evaluator. Scanner failure fails the job; findings follow the shared `sast` policy. Reachability stays unknown, not assumed exploitable. SARIF messages/snippets are not uploaded. No security-events write permission is granted to pull requests.
- Existing API contract, secret, policy, repository-security and hygiene gates remain.

Every required workflow runs for pull requests, main pushes and merge groups without path filters. Jobs have timeouts and superseded runs are cancelled. No production secrets, credentials, privileged containers or host mounts are used.

## Tools and caches

Go: `.go-version`; module minimum: `go.mod`. Node: `.node-version`; npm: `ui/package.json` and `tools/ci/package.json` packageManager. Lint/browser tooling has a separate locked install to avoid changing application dependencies. ESLint, Next ESLint config and Playwright are exact dev dependency versions with npm integrity hashes in `tools/ci/package-lock.json`. Playwright installs the browser revision paired with that exact package. GitHub Actions are full commit SHAs with release comments.

setup-go caches include runner OS, architecture, Go version and go.sum digest. Node/browser caches are deliberately disabled instead of restoring cross-version caches. Vulnerability/security databases are not cached by this baseline.

## Local reproduction

Use the declared Go/Node/npm versions. From the repository root:

```bash
scripts/ci/backend.sh
npm --prefix tools/ci ci
npm --prefix ui ci
scripts/api/check-generated.sh
npm --prefix ui run lint
npm --prefix ui run typecheck
npm --prefix ui test
NEXT_PUBLIC_CONSIZE_API_BASE_URL=http://127.0.0.1:18080 NEXT_PUBLIC_USE_MOCKS=false npm --prefix ui run build
go build -mod=readonly -o bin/consize-smoke ./cmd/consize
cd ui
node ../tools/ci/node_modules/@playwright/test/cli.js install chromium
npm run test:e2e
```

The browser test refuses existing listeners on ports 18080/3031. Playwright waits for HTTP readiness and terminates both servers; the API wrapper removes its temporary state even on interruption. Backend readiness uses `/api/health`. No developer kubeconfig or cloud account is used.

## Failure evidence

The new tests cover API permission refusal, malformed successful JSON, unavailable network, invalid error JSON, workflow trigger/check integrity, lockfile version agreement and invalid/incomplete SARIF. Report normalization discards source messages and rejects unknown rules/invalid source locations.

`internal/cibaseline` executes synthetic bad-format and failing Go tests in temporary directories. `scripts/ci/frontend-controls.mjs` executes the locked compiler and the real frontend test runner against synthetic type/assertion failures and requires their expected nonzero exits. These failure proofs run inside the named CI jobs without committing defective product code. Additional disposable-branch checks can be used when changing workflow dispatch or protection settings. Keep links to their failing jobs and the final passing PR run in the PR test plan. An intentionally failing fixture must never land on main. Do not mark CON-58 complete until canonical CI confirms the clean run and failure evidence.

## Required-check mapping

Keep the existing required checks. Replace `Frontend production build` with `Frontend quality` only after the replacement has emitted successfully. Add `Browser smoke`, `CodeQL (go)` and `CodeQL (javascript-typescript)` using their exact emitted names. Keep `Backend quality` unchanged. A temporary `Frontend production build` compatibility job requires `Frontend quality` to succeed, so the current ruleset remains enforced during the transition. Inspect GitHub ruleset `Protection` (ID 22949336); record its current settings and the intended additions in the PR. Do not change active protection until the checks are verified. Enforcement validation also belongs to CON-61.

## Maintenance and reports

Dependabot covers Go, npm, Actions and Docker definitions when present. Minor/patch version updates are grouped; security update PRs remain separately reviewable and must pass ordinary CI. GitHub's separate Dependabot security-update setting must be enabled after review; configuration alone does not prove it is enabled.

Scheduled Scorecard runs with a read-only token and does not publish results externally. Review its JSON artifact and create focused follow-up work for regressions; an assessment is not proof that all posture checks pass.

Browser traces/screenshots contain only this synthetic local workload and last 7 days. CodeQL normalized reports/decisions and Scorecard results last 14 days. Artifacts inherit repository access; this public OSS repository is not a private evidence store. Never run these jobs with customer data or credentials. On failure inspect the named job and artifact; fix the cause rather than adding blanket ignores/retries.
