# Consize Testing Standard

This file is the canonical testing standard for Consize. Contributors and coding agents must read it before changing code, configuration, contracts, workflows, plugins, infrastructure manifests, documentation, or release artifacts.

The purpose of testing is not merely to make CI green. Testing must provide credible evidence that a change works, fails safely, preserves the optimization safety lifecycle, and does not weaken security or compatibility.

This standard applies to the v0.3 development line on `main`. The `release/0.2` branch has its own historical architecture and must be tested using the commands and release requirements preserved on that branch.

## 1. Non-negotiable rules

1. Test the behavior changed, not only the file changed.
2. Run focused tests while developing, then run every applicable repository gate before requesting review.
3. A passing happy path is insufficient for security, policy, action, verification, recovery, persistence, plugin, and API changes.
4. Tests must cover refusal, failure, retry, restart, and recovery paths when the changed behavior can affect infrastructure or durable state.
5. No test may require real customer data, production credentials, production infrastructure, or undocumented files from a developer's machine.
6. Never weaken, skip, delete, or broadly exclude a test merely to obtain a green result.
7. Never update a fixture or snapshot until the behavioral change has been reviewed and the new output is understood.
8. CI must not repair generated files, formatting, lockfiles, contracts, or reports invisibly. It must detect drift and fail.
9. A scanner crash, timeout, missing comparison base, invalid report, or unavailable vulnerability data is an incomplete check, not a clean result.
10. Browser checks do not replace server-side authorization, policy, preflight, idempotency, or durability tests.
11. Mocked tests do not replace at least one real integration path for Kubernetes, Prometheus, persistence, package installation, or other external boundaries changed by a ticket.
12. Tests must be deterministic. Time, randomness, network behavior, process lifecycle, and external data must be controlled or bounded.
13. Flaky tests are defects. Do not normalize rerunning a failing test until it passes.
14. Never claim end-to-end coverage when any step was mocked, skipped, manually repaired, or supported by hidden local state. State exactly what was exercised.
15. Every pull request must include reproducible test evidence and identify anything that was not run.

## 2. Required agent workflow

When an agent is asked to implement or review a change, it must use this sequence.

### 2.1 Before editing

1. Read this file, the ticket, affected contracts, and nearby tests.
2. Inspect `git status` and preserve unrelated user changes.
3. Identify the behavioral surface, trust boundary, durable state, external dependencies, and user-visible states affected.
4. Classify the change using Section 3.
5. Write a short test plan mapping each acceptance criterion and material risk to evidence.
6. Confirm that required tools and fixtures are available. Missing tools must be reported, not silently bypassed.

### 2.2 During implementation

1. Add or update the narrowest test that fails for the missing behavior.
2. Implement the behavior.
3. Run focused tests after each meaningful change.
4. Add negative and boundary cases before broadening the implementation.
5. Keep test fixtures synthetic, minimal, readable, and version controlled.
6. Do not introduce production fallback data or tests that depend on execution order.

### 2.3 Before declaring completion

1. Run all checks required by the change classification.
2. Exercise the normal path, refusal path, failure path, and recovery path where applicable.
3. Review the final diff and tracked files.
4. Verify that no secret, local state, binary, cache, generated build output, or audit log was added.
5. Record exact commands, outcomes, environment, and limitations.
6. Do not say "all tests pass" when only focused tests were run.

## 3. Change classification and minimum depth

Use the highest applicable risk class. A change may belong to several categories; requirements accumulate.

| Class | Examples | Minimum evidence |
| --- | --- | --- |
| A - Documentation only | Prose with no executable examples, links, commands, policy meaning, or API claims changed | Link check, spelling/readability review, rendered layout where relevant, factual comparison with current code |
| B - Local logic | Pure functions, formatting, mapping, validation, UI presentation without mutation | Focused unit tests, boundaries, full affected package tests, formatting, static analysis, production build when UI is affected |
| C - Contract or persistence | OpenAPI, generated types, resource identity, state schema, migrations, configuration, audit records | Unit and integration tests, compatibility/drift checks, old/new state fixtures, restart test, malformed input, clean-checkout validation |
| D - External integration or plugin | Kubernetes, Prometheus, GitHub, pricing, marketplace, network clients | Contract tests, real local integration test, permission failure, timeout, malformed response, retry/idempotency, health degradation |
| E - Infrastructure mutation | Plan, approval, direct apply, GitOps, rollback, recovery | Complete governed lifecycle, stale-resource rejection, exact-plan binding, duplicate submission, partial failure, restart recovery, verification and rollback evidence |
| F - Security or supply chain | Authentication, authorization, CI, scanners, workflows, plugin trust, release provenance | Abuse cases, fail-closed behavior, redaction, invalid/expired exception, malicious or malformed artifact, least privilege, isolated CI failure proof |
| G - Release critical | Installer, images, manifests, release workflow, upgrade, supported environment | Clean installation, upgrade/rollback, smoke and live E2E, security gates, artifact identity, checksums/provenance, release checklist |

Small diffs can be high risk. A one-line authorization, policy, query, timeout, or workflow change may require Class E or F testing.

## 4. Canonical local checks

Run commands from the repository root unless a section says otherwise.

### 4.1 Toolchain and clean checkout

Use the versions declared by the repository:

```bash
go version
node --version
npm --version
git status --short
```

The exact Go CI version comes from `.go-version` (and must satisfy `go.mod`); Node comes from `.node-version` and npm from `ui/package.json` `packageManager`. Use `npm ci`, not `npm install`, for validation.

A clean-checkout test must not depend on:

- `.consize` state from another run;
- a sibling checkout;
- untracked generated files;
- a developer's global Node modules;
- uncommitted OpenAPI declarations;
- credentials stored in shell history;
- a previously installed plugin binary;
- a Kubernetes resource created outside the documented setup.

### 4.2 Backend quality

```bash
test -z "$(gofmt -l cmd internal pkg)"
go vet ./...
go test ./...
go test -race ./...
```

Use focused tests during development:

```bash
go test ./internal/orchestrator -run TestName -count=1
go test ./pkg/plugins/kubernetes -run TestName -count=1
go test ./pkg/plugins/prometheus -run TestName -count=1
```

Use `-count=1` when cached results could hide timing, state, filesystem, or process behavior.

For concurrency-sensitive work, run the affected test repeatedly in addition to the full race suite:

```bash
go test -race ./path/to/package -run TestName -count=20
```

### 4.3 Frontend quality

```bash
cd ui
npm ci
npm run lint
npm run typecheck
npm test
npm run build
cd ..
```

`Frontend quality` runs lint, type checks, unit tests, generated-declaration drift checks and a production build. `Browser smoke` builds the real Go API, starts it with a new temporary durable store, and runs the production UI with MSW disabled. See [the baseline CI runbook](docs/engineering/baseline-ci.md) for the local browser command, evidence and required-check migration.

### 4.4 API contract

Install locked frontend tools before running contract checks:

```bash
cd ui
npm ci
npx --no-install redocly lint ../openapi/openapi.yaml --config ../redocly.yaml
cd ..

go test ./internal/api -run OpenAPI
scripts/api/check-generated.sh
scripts/api/test-contract.sh
scripts/api/bundle.sh "$(mktemp -d)"
```

If the canonical OpenAPI document changes:

1. Regenerate declarations with the pinned repository command.
2. Commit the OpenAPI change, generated declarations, backend behavior, and affected UI consumers together.
3. Run compatibility comparison against a valid base revision.
4. Treat `oasdiff` warnings as review items even when they are not hard errors.
5. Add explicit behavioral tests for guarantees schema comparison cannot prove.

Do not accept an invalid or missing base revision as a successful compatibility result.

### 4.5 Security policy and repository security

```bash
go test ./internal/securitypolicy ./cmd/security-policy
go run ./cmd/security-policy validate \
  --policy .github/security/policy.json \
  --exceptions .github/security/exceptions.json

go test ./internal/repositorysecurity ./cmd/repository-security
go run ./cmd/repository-security \
  --context build \
  --output /tmp/consize-repository-security-report.json

go run ./cmd/security-policy evaluate \
  --policy .github/security/policy.json \
  --exceptions .github/security/exceptions.json \
  --report /tmp/consize-repository-security-report.json \
  --output /tmp/consize-repository-security-decision.json
```

Validate exceptions with fixtures for:

- valid active exception;
- malformed exception;
- expired exception;
- unauthorized approver;
- change author approving their own high or critical exception;
- invalid or inactive `renewal_of` reference;
- component, context, version, digest, or rule mismatch;
- prohibited exception type such as a live secret or integrity-verification failure.

Reports must be redacted. Never place secret values, credentials, private telemetry, or sensitive exploit details in CI logs or public artifacts.

### 4.6 Secret scanning

Install and test the pinned scanner through repository scripts:

```bash
scripts/security/install-gitleaks.sh /tmp/consize-gitleaks
GITLEAKS_BIN=/tmp/consize-gitleaks scripts/security/test-gitleaks.sh
GITLEAKS_BIN=/tmp/consize-gitleaks \
  scripts/security/run-gitleaks.sh snapshot /tmp/consize-gitleaks-snapshot.json
```

For pull-request-equivalent testing, scan the complete introduced commit range using a valid merge base:

```bash
base="$(git merge-base HEAD origin/main)"
GITLEAKS_BIN=/tmp/consize-gitleaks \
  scripts/security/run-gitleaks.sh range /tmp/consize-gitleaks-history.json "$base"
```

The history test must detect a synthetic test token even when it is added and removed within the tested branch. Use only documented synthetic fixtures in a disposable, unmerged branch. Never test with a live credential.

### 4.7 Dependency security

Use `scripts/security/install-govulncheck.sh "$PWD/.tools"`, then run `go test -race ./internal/dependencysecurity ./internal/securitypolicy ./cmd/dependency-security`. The live scan command and report interpretation are in [the dependency security runbook](docs/engineering/dependency-security.md). Both npm lockfiles, including CI tooling, must remain in coverage; no lockfile is changed in CI.

### 4.8 Repository hygiene

```bash
git status --short
git diff --check
git grep -n 'github.com/consize-oss/consize-0' -- '*.go' go.mod || true
```

Review tracked files for:

- private `.env` files;
- keys, kubeconfigs, credential archives, and cloud credential files;
- `.consize` state and audit logs;
- `.next`, `node_modules`, coverage, caches, and downloaded tools;
- compiled binaries and unexpected large files;
- generated declarations that do not match their source;
- workflow permissions that are broader than required;
- mutable GitHub Action references;
- temporary screenshots or debugging output.

`git diff --check` and `git status` are mandatory but do not replace the repository security gate.

## 5. Backend test design

### 5.1 Unit tests

Unit tests must be table driven where multiple policy or state combinations exist. Cover:

- ordinary valid values;
- zero, minimum, maximum, and just-inside/just-outside boundaries;
- empty, missing, stale, malformed, duplicate, and contradictory input;
- overflow, rounding, units, and precision where cost or resources are calculated;
- deterministic output ordering;
- cancellation and timeout;
- invariants that must never be violated;
- error identity and stable reason codes used by callers.

Avoid testing private implementation details when a stable package contract can be asserted.

### 5.2 Concurrency and ownership

For stores, workers, orchestrators, registries, caches, and plugin runtimes, test:

- two callers attempting the same operation;
- duplicate idempotency keys;
- concurrent operations on the same resource;
- permitted parallel operations on unrelated resources;
- lock acquisition and release;
- process cancellation while work is pending;
- stale owner or lease recovery;
- no data race under `go test -race`;
- no double apply, double rollback, or duplicate audit terminal event.

### 5.3 Time-dependent behavior

Inject or control clocks. Do not make unit tests sleep for real verification windows.

Test exact boundaries for:

- evidence freshness;
- recommendation expiry;
- approval expiry;
- maintenance windows;
- verification wait and timeout;
- retry backoff;
- exception expiry;
- plugin health staleness;
- billing-data freshness.

Use bounded polling in integration tests and print useful state when a timeout occurs.

## 6. Universal resource model and registry

Changes to resource identity, discovery, normalization, ownership, readiness, lifecycle, or persistence must test:

1. Stable identity across repeated discovery.
2. Uniqueness across provider, account/project/subscription, region, cluster, namespace, type, and provider-native identifier.
3. The same display name in two clusters or accounts does not collide.
4. Provider observations normalize without discarding required provider extensions.
5. Unknown fields and future provider extensions fail or round-trip according to the declared compatibility policy.
6. Ownership and environment precedence is deterministic and records provenance.
7. Active, stale, deleted, rediscovered, and superseded lifecycle transitions.
8. Discovery replay is idempotent.
9. Partial provider failure does not erase previously known resources.
10. Malformed provider data cannot create an actionable resource.
11. Readiness distinguishes `Optimization ready`, `Inventory only`, `Limited support`, `Setup required`, and `Unsupported` using stable reason codes.
12. Persistence survives restart without identity drift or duplicate records.

## 7. Evidence and recommendation engine

Evidence and algorithm tests must cover:

- query construction and label matching;
- units and conversions;
- sample count, time coverage, gaps, freshness, and source health;
- p95 and other percentile calculations against hand-checked fixtures;
- stable, bursty, scheduled, seasonal, batch, autoscaled, latency-sensitive, and unknown workload signals when classification is implemented;
- missing peak coverage and low-confidence outcomes;
- retained headroom and maximum step-down boundaries;
- request/limit invariants;
- preservation or change of limits as an explicit algorithm decision;
- recommendation explainability fields;
- deterministic algorithm ID and version;
- cost estimate source, currency, period, freshness, and assumptions;
- superseding a recommendation when evidence or resource state changes;
- no recommendation when evidence is stale, insufficient, contradictory, or unsafe;
- separation of projected, operationally verified, and billing-reconciled realized savings.

Confidence tests must not reduce confidence to sample count alone. They should cover observation-window coverage, recency, missing intervals, variability, peak representation, source health, workload classification confidence, and contradictory evidence as those signals become available.

## 8. Policy, authorization, and preflight

Every action-capable change must prove that the server, not the UI or plugin, enforces:

- authenticated actor identity;
- role and scope authorization;
- separation of duties where required;
- exact recommendation and immutable plan binding;
- approval expiry and approver eligibility;
- policy version and decision reasons;
- permitted remediation path;
- environment, owner, criticality, and resource-type constraints;
- maximum step size and action limits;
- evidence freshness and confidence requirements;
- plugin health, capability, trust, and permission requirements;
- current resource version and stale-resource rejection;
- active job conflicts;
- maintenance windows;
- verification and rollback requirements.

Required abuse tests include direct API calls that bypass the UI, modified resource identifiers, replayed approvals, changed plans, browser-supplied actor identities, privilege escalation, cross-scope access, stale resource versions, and unsupported action plugins.

## 9. Durable orchestration, apply, verification, and rollback

Infrastructure mutation is complete only when the entire lifecycle is tested:

```text
recommendation
-> plan and dry-run
-> policy and authorization
-> approval when required
-> durable job accepted
-> preflight against current state
-> apply
-> rollout readiness
-> isolated verification window
-> verified, rolled back, failed, or manual intervention
-> durable audit history
```

### 9.1 Planning

Assert that planning:

- causes no infrastructure mutation;
- describes the exact current and proposed state;
- records resource version, plugin, policy decision, verification plan, and rollback availability;
- is deterministic for the same immutable inputs;
- becomes unusable after material resource or recommendation drift.

### 9.2 Apply

Assert that apply:

- cannot bypass authorization, policy, approval, preflight, or audit persistence;
- accepts one idempotency key and does not mutate twice;
- mutates only the exact approved resource and fields;
- preserves a recovery snapshot before mutation;
- detects partial failure and records what happened;
- waits for the provider's readiness condition;
- does not report success merely because a request was accepted.

### 9.3 Restart recovery

Stop the process at each durable boundary when practical:

- after intent is stored but before apply;
- during apply;
- after apply but before verification starts;
- during verification;
- after failed verification but before rollback;
- during rollback;
- before terminal audit persistence.

After restart, assert that work resumes or enters explicit manual intervention without duplicate mutation or lost evidence.

### 9.4 Verification

Verification tests require an isolated pre-action baseline and post-action window. Cover:

- passing CPU, memory, latency, error, restart, and provider-specific checks that are configured;
- threshold equality and just-over-threshold failure;
- missing series, stale metrics, scrape gaps, reset counters, and source outage;
- inconclusive outcome distinct from passed;
- a changed query or label set after apply;
- unrelated global traffic changes where isolation is required;
- dependency SLI checks when relationship data is available;
- no claim of billing realization from operational metrics alone.

### 9.5 Rollback and manual intervention

Assert that rollback:

- requires policy authorization and a valid recovery snapshot;
- restores the exact previous controlled state;
- is idempotent;
- verifies recovery after restoration;
- cannot loop indefinitely when signals flap;
- records failure and manual intervention when automatic recovery cannot be proven;
- never erases the failed action or verification evidence.

## 10. Plugin SDK, packages, and marketplace

Test plugins as untrusted capability providers. A plugin must not control the platform lifecycle.

### 10.1 Contract tests

Every plugin category must have shared conformance tests for:

- manifest schema and semantic validation;
- stable ID, publisher, version, SDK/API compatibility, and capabilities;
- supported resource types and operations;
- required permissions and read/write distinction;
- dry-run, apply, rollback, and health declarations matching real behavior;
- typed request/response validation;
- cancellation, timeout, malformed output, crash, and unavailable dependency;
- redaction of credentials and sensitive values;
- deterministic errors and reason codes.

### 10.2 Package trust

Marketplace and package installation tests must cover:

- exact artifact digest;
- valid and invalid checksum;
- valid, unknown, rotated, and revoked signing identity;
- unsigned artifact rejection under trusted-only policy;
- manifest/artifact mismatch;
- incompatible core or SDK version;
- permission expansion during upgrade;
- downgrade and revoked version behavior;
- interrupted download and atomic installation;
- archive traversal, symlink, executable, and oversized payload defenses;
- catalog tampering, duplicate ID/version, and stale metadata;
- install does not imply enablement;
- no arbitrary GitHub release becomes trusted merely because it is public.

### 10.3 Runtime isolation

Where the runtime uses child processes or remote agents, test process identity, environment allowlisting, filesystem scope, network policy, resource limits, protocol framing, crash containment, log redaction, upgrade compatibility, and termination of orphaned work.

## 11. Frontend testing

Frontend tests must validate product behavior, not only rendering.

### 11.1 Required states

For each affected view, cover:

- initial loading;
- background refresh without layout replacement;
- success;
- no resources;
- no recommendations;
- empty filter result;
- partial API degradation;
- API unavailable;
- permission denied;
- stale evidence or resource state;
- missing plugin or setup required;
- blocked policy decision;
- accepted durable job;
- applying and verifying;
- verified, rolled back, failed, superseded, and manual intervention where relevant.

No production view may substitute hardcoded demo values when the API fails.

### 11.2 Interactions

Test that:

- recommendation row selection updates the correct detail panel;
- selection remains valid after non-material refresh and clears safely if the object disappears;
- action controls show the server-provided availability reason;
- double click, repeated submit, refresh, and back navigation do not duplicate mutation;
- scope and date controls are keyboard accessible and update only supported API filters;
- theme selection persists and all semantic states remain readable;
- navigation expands, collapses, and becomes a mobile drawer without covering primary actions;
- long identifiers, currency values, policy names, plugin names, and errors wrap without overlap;
- the action column remains reachable at supported laptop widths;
- deep links and URL filters can be refreshed and shared;
- correlation IDs and support details are available without leaking sensitive data.

### 11.3 Responsive and visual evidence

Inspect at minimum:

- 390 x 844;
- 768 x 1024;
- 1024 x 768;
- 1440 x 900;
- a wide desktop viewport.

Check dark and light themes. Capture screenshots for changed surfaces and inspect for overlap, clipping, hidden actions, unexpected horizontal page scrolling, inconsistent spacing, unreadable contrast, layout shift, and stale demo content.

Canvas or chart changes require a nonblank pixel/content assertion in addition to screenshots.

### 11.4 Accessibility

Critical workflows must be usable by keyboard alone. Verify:

- logical tab order;
- visible focus in both themes;
- accessible names for icon-only controls;
- correct menu, dialog, tab, table, alert, and progress semantics;
- focus restoration after a dialog or drawer closes;
- status is not conveyed by color alone;
- reduced-motion behavior;
- useful announcements without polling noise;
- reflow at 200 percent zoom;
- no keyboard trap.

Automated accessibility tooling is required once added, but it never replaces keyboard and screen-reader review of mutating workflows.

## 12. Kubernetes integration tests

Use an ephemeral local cluster or a dedicated disposable test environment. Never point mutation tests at production.

Test:

- least-privilege read discovery;
- namespace and object scope;
- missing RBAC permission;
- Deployment discovery and normalized identity;
- multi-container selection and unsupported layouts;
- dry-run patch;
- exact patch fields;
- resource-version conflict;
- rollout timeout and failed rollout;
- object deleted or changed after approval;
- repeated apply idempotency;
- exact recovery from stored previous state;
- API outage, timeout, and cancellation;
- worker restart at durable boundaries;
- audit history for every result.

Verify the Kubernetes object directly with `kubectl`; an API response or UI message alone is insufficient.

## 13. Prometheus integration tests

Use a real Prometheus endpoint with synthetic, controlled series for integration coverage.

Test:

- readiness and health queries;
- authentication and transport failure where supported;
- instant and range query encoding;
- label escaping and identity matching;
- empty vector, multiple unexpected series, NaN, infinity, stale markers, and malformed data;
- time range, step, sample count, and coverage;
- counter resets for restart/error metrics;
- timeout and cancellation;
- pre-action baseline isolation;
- post-action verification pass, fail, inconclusive, and timeout;
- query freshness after restart.

Mocked HTTP tests may cover protocol edges, but the release path requires a real Prometheus workflow.

## 14. API and integration testing

API tests must cover:

- canonical success and error bodies;
- validation for path, query, header, and body inputs;
- authentication and role/scope authorization;
- request and correlation IDs;
- content type and method handling;
- pagination, sorting, and filtering when available;
- CORS allow and deny behavior;
- idempotency and replay;
- timeout and cancellation propagation;
- redaction of internal paths, secrets, and stack traces;
- durable acceptance versus terminal action state;
- backward compatibility for supported clients.

External service adapters must test rate limits, retries with bounded backoff, non-retryable errors, malformed responses, partial responses, pagination, cancellation, and credential redaction.

Do not call paid third-party services in ordinary pull-request CI. Use contract fixtures and dedicated opt-in integration environments. Tests against real services must use narrowly scoped test accounts and record the exact environment without exposing credentials.

## 15. Persistence, migration, and audit testing

Persistence changes must test:

- new empty store initialization;
- current-version reopen;
- supported previous-version migration;
- invalid or ambiguous state fails with actionable guidance;
- atomic write and interrupted write recovery;
- file locking and single owner behavior;
- schema/version mismatch;
- duplicate and concurrent writes;
- restart with pending jobs and verification;
- deterministic serialization where files are compared;
- forward state is not silently downgraded.

Audit tests must prove events are durable, ordered or causally linked, correlated, append-oriented, redacted, and sufficient to reconstruct recommendation, plan, policy, approval, apply, verification, rollback, recovery, and actor identity.

No mutation may proceed when required durable intent or audit persistence fails.

## 16. Security testing

Security review and tests must address the changed threat surface, including:

- authentication bypass;
- horizontal and vertical authorization bypass;
- confused deputy and browser-supplied identity;
- injection into shell, URL, path, query, logs, templates, YAML, JSON, or Kubernetes fields;
- path traversal and archive extraction;
- SSRF and unrestricted plugin network access;
- secret exposure in UI, logs, errors, reports, artifacts, and fixtures;
- unsafe deserialization and unbounded input;
- denial of service through size, concurrency, retry, or expensive queries;
- insecure defaults and unauthenticated non-loopback exposure;
- workflow token permissions and untrusted pull-request execution;
- dependency, license, provenance, and end-of-life runtime risk;
- plugin signature, checksum, permission, compatibility, and revocation controls.

Security controls require regression fixtures that demonstrate both rejection and permitted narrow examples. Do not publish real exploit material or incident evidence in a public fixture.

## 17. Documentation and configuration testing

Documentation changes are executable product changes when they contain commands, configuration, permissions, API examples, install steps, or safety claims.

Verify:

- commands from a clean shell or checkout;
- links and referenced paths;
- configuration parses and validates;
- example values are synthetic and non-secret;
- copy/paste blocks preserve required quoting and line breaks;
- documented defaults match code;
- unsupported features are not presented as available;
- screenshots and UI terminology match the product;
- tables render without overlapping text;
- generated PDF or document output is visually inspected when applicable.

## 18. End-to-end OSS MVP test

The release-defining test must work from documented artifacts without mock dashboard data, hidden local files, manual state repair, or maintainer-only knowledge.

1. Install Consize from the documented artifact or a clean source checkout.
2. Start a supported local Kubernetes cluster.
3. Install the documented synthetic workload and Prometheus.
4. Start Consize with a new state directory and documented configuration.
5. Confirm localhost-only exposure when authentication is disabled.
6. Discover the Kubernetes Deployment through the Kubernetes plugin.
7. Confirm normalized resource identity and readiness.
8. Collect real Prometheus evidence with freshness and coverage.
9. Generate an explainable recommendation.
10. Inspect algorithm version, evidence, confidence, proposed change, cost classification, policy context, and verification plan.
11. Create a plan and dry-run; verify Kubernetes is unchanged.
12. Demonstrate a policy refusal or approval requirement.
13. Approve the exact current plan through an authorized server path.
14. Direct apply the permitted change.
15. Verify the Kubernetes rollout and exact target state independently.
16. Observe persisted post-action verification.
17. Demonstrate a verification failure and automatic rollback where policy permits.
18. Verify the exact previous state is restored and recovery is checked.
19. Inspect the complete audit history.
20. Restart Consize with pending work and prove recovery without duplicate action.
21. Confirm the UI reconstructs state from the API after reload.
22. Confirm projected, operationally verified, and realized savings are not conflated.
23. Tear down the disposable environment.

Use `DEVELOPMENT.md` and `local-lab/SAFETY-NOTES.md` for the current setup. Mutation testing is restricted to the documented `consize-demo/checkout-api` workload unless the test plan explicitly creates another disposable target.

## 19. Release qualification

A release candidate requires all pull-request gates plus:

- clean installation on every declared supported environment;
- upgrade from the supported previous release or an explicit statement that no upgrade is supported;
- rollback to the previous release where supported;
- full OSS MVP E2E;
- restart recovery at critical lifecycle points;
- container and manifest security checks when release images/manifests exist;
- immutable artifact identity, checksums, SBOM, provenance, and signatures according to release policy;
- plugin compatibility matrix for bundled plugins;
- documentation and example validation;
- no unresolved release-blocking security finding without an approved, unexpired exception;
- known limitations and unsupported behavior published accurately;
- v0.2 maintenance artifacts remain independently recoverable while that line is supported.

Alpha does not mean partially connected. It means the declared scope is coherent, testable, and honest about limitations.

## 20. CI expectations

The current canonical checks include:

- `API contract`;
- `Secret scan`;
- `Security policy`;
- `Dependency security`;
- `Repository security`;
- `Backend quality`;
- `Frontend quality`;
- `Browser smoke`;
- `CodeQL (go)` and `CodeQL (javascript-typescript)`;
- `Repository hygiene`.

Required checks must run on pull requests, `main` pushes, and merge groups where configured. Scheduled security rescans should detect newly disclosed risk without a code change.

CI workflows must use least-privilege tokens, immutable action references, bounded timeouts, explicit failure propagation, and redacted artifacts. A required job must not be hidden behind a workflow path filter that leaves branch protection waiting indefinitely.

Local success is necessary but not sufficient. The pull request must also show successful required checks from the canonical repository.

## 21. Test failure handling

When a test fails:

1. Preserve the first useful failure output.
2. Reproduce with the narrowest command.
3. Determine whether the product, test, fixture, environment, or requirement is wrong.
4. Check for race, timing, order, leaked state, port, filesystem, and external dependency causes.
5. Fix the root cause.
6. Run the focused test repeatedly when flakiness or concurrency is suspected.
7. Run the broader applicable suite.

Do not:

- add unconditional retries;
- increase a timeout without evidence;
- skip the test on CI;
- remove an assertion;
- accept all new snapshots;
- catch and ignore an error;
- add `|| true` or unconditional success handling;
- downgrade a security severity globally;
- create a broad allowlist or directory exclusion.

If a test is genuinely obsolete, explain the changed contract in the pull request and replace it with coverage for the new behavior.

## 22. Pull request test evidence

Every pull request description must include:

```markdown
## Test plan

### Risk classification
- Class: B/C/D/E/F/G
- Main risks:

### Acceptance criteria mapping
- Criterion:
  - Evidence:

### Automated checks
- `exact command` - PASS/FAIL

### Manual or live checks
- Environment:
- Steps:
- Result:

### Failure and recovery coverage
- Refusal path:
- Failure path:
- Restart/retry path:
- Rollback/manual intervention path:

### UI evidence, when applicable
- Desktop:
- Mobile:
- Dark mode:
- Light mode:
- Keyboard/accessibility:

### Not run
- Check:
- Reason:
- Residual risk and owner:
```

Evidence should include relevant CI run links, screenshots, redacted reports, logs, resource state, and correlation IDs. Do not attach secrets, credentials, private telemetry, or uncontrolled customer data.

## 23. Definition of tested

A change is tested only when:

- acceptance criteria map to evidence;
- all applicable automated checks pass;
- changed contracts and generated outputs agree;
- normal, boundary, refusal, failure, and recovery behavior are covered according to risk;
- external boundaries have appropriate real integration evidence;
- security controls fail closed;
- durable work survives restart where applicable;
- UI states are responsive and accessible where applicable;
- no hidden state, production credential, or customer data was required;
- the final diff contains no unrelated or generated debris;
- limitations and unrun checks are disclosed;
- canonical CI confirms the result.

Anything less must be described as partial testing, not completion.
