# v0.3 consolidation qualification (CON-56)

## Decision

Qualification is **incomplete**. A green baseline CI run does not establish the
complete live optimization workflow or cross-functional review.

This record was checked on 4 November 2026 against:

- v0.3 main: `d8b44dede6c502f4837e400c48d0a9eb4282ff82`.
- Immutable v0.2.0 tag: `d323b7faa2e1600e1f0256d08589e81b29a89a64`.
- v0.2 maintenance: `b1a086fde879c42069bfb6cf6e0b9b40ffb4db6f`.

The tag and maintenance checkout were detached and neither historical source was
edited. Local preserved-line checks used Go 1.27.0 on darwin/arm64, Node 22.23.1
and npm 10.9.8. Re-run qualification against a new commit when implementation changes;
do not reuse these results as evidence for a different source revision.

## Evidence already established

Current main passed its [baseline CI](https://github.com/consize-oss/consize/actions/runs/41660927551),
[CodeQL](https://github.com/consize-oss/consize/actions/runs/41660927979),
[dependency security](https://github.com/consize-oss/consize/actions/runs/41660927881)
and [documentation deployment](https://github.com/consize-oss/consize/actions/runs/41660927951).
Baseline CI includes backend unit/race checks, disposable PostgreSQL integration,
frontend lint/type checking/build, and browser smoke without service-worker mocks.
The backend suite includes worker restart/recovery and packaged-plugin
installation/tampering tests. Those tests are reproducible controlled scenarios;
they are not a live cluster qualification result.

The original [foundation PR #23](https://github.com/consize-oss/consize/pull/23)
and [capability migration PR #25](https://github.com/consize-oss/consize/pull/25)
are merged. Their GitHub review lists did not provide recorded cross-functional
approvals at inspection time. Obtain backend/integration and frontend review on
the qualification evidence before closing CON-56.

The preserved `release/0.2` checkout passed backend tests and vet, frontend lint,
and the production UI build. Its lockfile is identical to the tag's lockfile;
the reviewed installed dependency tree was moved between checkouts to conserve
disk. This was not recorded as two independent fresh dependency installs.
Both revisions also passed Helm lint, team-scoped/production Helm rendering and
Compose configuration validation without starting their deployment stacks.

## Known gaps and outstanding checks

1. **Live evidence incomplete.** On 7 November, Docker Desktop recovered after
   a normal restart timed out and its supported force-stop/start commands were
   used. Docker and Kubernetes responded, the demo pods were running, and the
   local-only Prometheus port-forward restored readiness at `127.0.0.1:9090`.
   Against `b7448fc`, `TestLiveKubernetesApplyRollback` passed (6.97s) and
   `TestLivePromQLHealthSemantics` passed (1.63s). Neither test was skipped.
   `TestLiveLabVerificationEvidence` failed: all six required signals had
   insufficient or stale coverage and missing or untrustworthy derived
   provenance in its 30-minute window immediately after recovery. Keep the
   lab collecting and rerun with the original evidence requirements; do not
   reduce the window or treat readiness as usable history. Discovery,
   recommendation, plan, approved apply, verification, rollback and audit have
   not been requalified end to end in this run.
2. **UI integration remains incomplete.** The current browser smoke explicitly
   expects `/api/v1/dashboard/summary` to return 404 and checks visible failure
   cards. This proves honest error behavior; it does not prove functional
   dashboard/recommendation integration. CON-41 must provide that integration
   before it can count as a completed qualification path.
3. **The immutable release has a known lint defect.** Current tag testing
   reproduced `no-explicit-any` and an unused setter warning in
   `CostOpportunitiesView.tsx`. The tag's backend tests and production UI build
   passed with the locked dependencies in this run. Do not move or patch the
   tag to make qualification green. Validate maintenance fixes independently.
4. **Review is outstanding.** Automated results do not substitute for the
   affected backend/integration and frontend reviews.

## Reproduce the checks

### Follow-up diagnosis — 7 November

Fresh workload samples resumed, but a later live check still found 13–15.5-minute
gaps within the 30-minute window: CPU 31/61, throttling 30/61, memory and eviction
35/61 samples. Restart and OOM signals had 60/61 samples. Latest samples were
approximately 30 seconds old. Thus current collection had recovered, while the
historical window remained incomplete; the cause of the collection interruption
was not established.

The investigation also found a separate HTTP client defect: whole-second query
and response timestamp truncation moved a fractional start sample outside the
requested interval, causing partial provenance even with otherwise complete data.
The client now uses millisecond boundaries contained within the requested interval
and preserves response milliseconds. HTTP regression tests cover fractional,
millisecond-aligned and whole-second windows. No quality threshold was relaxed.
The live test now logs counts, latest age and largest gap before verification.
Retry the unchanged live qualification after an uninterrupted fresh window;
these diagnostics and the client fix do not establish a passing live workflow.

Use clean detached worktrees or fresh clones. Record `git rev-parse HEAD`,
`git status --short`, Go, Node and npm versions, commands, exit codes and CI run
URLs. Never publish kubeconfig, environment values, database URLs, credentials or
full provider payloads as evidence. Keep raw local logs private and attach only
reviewed, redacted summaries.

### Current development baseline

Install the versions pinned in `.go-version`, `.node-version` and
`ui/package.json`. Follow [CONTRIBUTING.md](https://github.com/consize-oss/consize/blob/main/CONTRIBUTING.md)
for dependency installation and the supported short-lived branch model.

```sh
scripts/ci/backend.sh
CI=1 go test -race ./internal/store -count=1
npm --prefix ui run lint
npm --prefix ui run typecheck
npm --prefix ui test
npm --prefix ui run build
scripts/api/check-generated.sh
```

The PostgreSQL command requires Docker. A skipped PostgreSQL suite is not a pass.
Use the real API browser-smoke commands from
[CI](https://github.com/consize-oss/consize/blob/main/.github/workflows/ci.yml),
with `NEXT_PUBLIC_USE_MOCKS=false` and API base `http://127.0.0.1:18080` for both
build and test. That smoke has the limited scope described above.

### Live Kubernetes and Prometheus

Use only the isolated Docker Desktop lab described in
[DEVELOPMENT.md](https://github.com/consize-oss/consize/blob/main/DEVELOPMENT.md).
Do not change kube context globally or target a production cluster to obtain
qualification evidence.

First run the bounded, read-only prerequisite check:

```sh
python3 scripts/qualification/preflight.py
```

It exits 0 only when Docker, the `consize-demo/checkout-api` deployment and local
Prometheus readiness are reachable. It exits 2 when any prerequisite is missing
or times out. This is readiness for testing, not a qualification certificate.
Tool output is withheld because connection errors can contain local details.

When ready, run the opt-in tests:

```sh
CONSIZE_LOCAL_INTEGRATION=1 go test ./pkg/plugins/kubernetes -run TestLiveKubernetesApplyRollback -v -count=1
CONSIZE_LAB_METRICS=1 go test ./pkg/plugins/prometheus -run TestLiveLabVerificationEvidence -v -count=1
CONSIZE_PROMQL_INTEGRATION=1 go test ./pkg/plugins/prometheus -run TestLivePromQLHealthSemantics -v -count=1
```

Confirm the named tests actually run; an unmatched `-run` or skipped test is not
evidence. The Kubernetes test creates and removes its own
`consize-safety-smoke-*` namespace. It proves the plugin apply/rollback boundary;
the passive metrics test proves real measurement availability. Neither alone
proves the complete governed workflow.

Then exercise the documented local flow from discovery through recommendation,
plan, explicit approval, worker execution, verification and policy-controlled
rollback. Preserve a dedicated disposable state store across API restarts. Capture
resource/recommendation/action/job IDs, evidence references, before/after resource
settings and audit correlations. Verify pending action and verification work
resumes and restoration matches the captured original state. Keep unsafe or
inconclusive outcomes explicit; do not shorten evidence windows or fabricate
health to complete the run.

### v0.2 preservation and maintenance

Create detached checkouts from both recorded revisions. Run separately in each:

```sh
cd engine
go test -mod=readonly -p 1 ./...
go vet -mod=readonly -p 1 ./...
cd ../ui
npm ci
npm run lint
npm run build
cd ..
helm lint charts/consize
helm template consize charts/consize -f charts/consize/examples/values-team-scoped.yaml > /dev/null
helm template consize charts/consize -f charts/consize/examples/values-prod.yaml > /dev/null
docker compose -f docker-compose.demo.yaml config --quiet
```

Serial compilation (`-p 1`) reduces peak disk use without changing test coverage.
Record failures separately from environment failures such as disk exhaustion.
The two revisions have identical frontend lockfiles; a reviewed locked dependency
installation can be moved between checkouts when disk is constrained, but source
and build outputs must remain separate. Do not describe shared dependencies as two
independent fresh installs.

The historical lint defect is a known failure, not a reason to weaken its gate.
The maintenance branch includes [hotfix PR #21](https://github.com/consize-oss/consize/pull/21).
Follow [the maintenance policy](../maintenance-0.2.md) for patch releases.

## Closeout checklist

### Governed live run started — 7 November

Source `17644c6` was built into a dedicated loopback API at port 18081 with
private disposable state and a Docker Desktop-only kubeconfig. Discovery found
`consize-demo/checkout-api`; real memory evidence produced a medium-confidence
lab recommendation (ID 1) from 512 MiB to 384 MiB, retaining the 1 GiB limit.
Dry-run action 1 passed QoS/HPA/VPA preflight. Explicit approved execution created
job 1/action 2, and the live deployment reached three ready replicas at the target.

The API was stopped with SIGTERM while the job was `applying`, then restarted
with the same state. It resumed to `verifying`. A second restart during
verification preserved the job/action IDs, window start and absolute deadline.
Submitting the same approval again returned job 1. Durable action records showed
`prepared`, `applying`, `waiting_rollout` and `verifying` correlated to action 2.

The captured policy was unchanged: 30-minute isolation, 5-minute observation and
10-minute timeout, with rollback on failure/timeout enabled. The window begins
09:37:34 and becomes eligible for assessment at 09:42:34 Lagos time; its deadline
is 09:52:34. At recording time the job was still `verifying`. No final verification
or policy-driven rollback outcome is claimed. The worker remains running with
the dedicated state; inspect its terminal result and audit before qualification.
The separate plugin rollback test passed earlier but does not replace that
governed rollback evidence.

### Governed verification result — 7 November

At 09:42:37 Lagos time, job 1/action 2 reached `verified`. The verification
result was `passed` with no failure reasons. All six required signals had 11/11
samples in the isolated five-minute observation interval, with 30-second maximum
gaps. Durable audit event 7 records that resource state, readiness and isolated
metrics checks passed. This establishes the live discovery-to-verified path and
the two observed API restart recoveries for this run. No rollback was triggered;
governed rollback remains unqualified, and CON-41 integration/reviews remain open.

- [ ] Attach passing current-main CI for the exact reviewed commit.
- [ ] Attach real live-workflow and restart/recovery evidence with no skipped stages.
- [ ] Verify the supported UI/API path works rather than only showing error states.
- [ ] Record separate tag and maintenance results, including known historical defects.
- [ ] Check tracked artifacts, clean-checkout setup and documentation links.
- [ ] Obtain backend/integration and frontend approvals.
- [ ] Merge the qualification changes through a PR and delete its temporary branch afterward.

Keep CON-56 open until the applicable outstanding evidence is resolved. This
qualification record must not be presented as an alpha release approval.
