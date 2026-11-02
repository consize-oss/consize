# CI Security Policy and Exception Standard

- **Policy version:** 1.0.0
- **Schema version:** 1
- **Applies to:** Pull requests, merge queues, default-branch pushes,
  scheduled scans, release builds, plugin publication, and deployment artifacts
- **Policy owner:** Repository security owner
- **Enforcement command:** `go run ./cmd/security-policy`

## 1. Purpose

Consize changes production infrastructure. Its repository security checks must
therefore fail predictably and must not be made green by hiding findings,
ignoring scanner errors, or accepting broad risk.

This document defines:

- which security findings block a pull request or release;
- which findings are recorded for triage;
- when an exception is permitted;
- who owns and approves an exception;
- how long an exception can remain active;
- how scanners exchange findings with the common policy evaluator;
- what happens when a scanner or report is incomplete;
- how unresolved findings affect releases;
- how sensitive evidence is handled.

The human-readable policy in this file and the machine-readable files under
`.github/security/` form one control. A scanner-specific workflow must not
invent a weaker exception mechanism.

## 2. Authority and Scope

This policy applies to first-party code, generated code that is committed to
the repository, dependencies, container images, GitHub Actions, build scripts,
Kubernetes and infrastructure-as-code configuration, plugin packages, release
artifacts, software bills of materials, signatures, checksums, provenance, and
security reports.

The policy applies equally to production and development dependencies unless a
rule below explicitly assigns different release impact. Development context
can change the approval scope; it does not make a known high-risk finding
invisible.

This policy does not replace incident response. A live credential, malicious
package, compromised signing identity, or tampered release is an incident and
must follow the incident process. Such an event cannot be converted into an
ordinary vulnerability exception.

## 3. Control Principles

### 3.1 Fail closed

A scanner crash, timeout, unavailable vulnerability database, invalid output,
missing report, or report upload failure is an incomplete security check. It is
not a clean scan.

### 3.2 Keep execution separate from disposition

An exception may affect the disposition of one exact finding. It must never:

- prevent the scanner from running;
- skip target or artifact validation;
- skip report-schema validation;
- change all findings to a lower severity;
- turn a scanner error into success;
- exclude an entire source directory, environment-file class, or dependency
  type;
- disable release-integrity verification.

### 3.3 Match narrowly

An exception is matched against the scanner, rule ID, stable fingerprint,
finding type, component, environment, execution context, and affected version.
Images and plugin packages also require an exact SHA-256 artifact digest.

Glob patterns, `all`, wildcard version ranges, and blanket paths are rejected by
the validator.

### 3.4 Treat expiry as a failure

An expired, malformed, over-duration, or overdue-for-review exception makes the
exception registry invalid. CI fails until the finding is fixed, the exception
is removed, or a new independently reviewed exception is approved.

### 3.5 Keep evidence honest

Reports may contain finding metadata, package names, file locations, redacted
fingerprints, and remediation guidance. They must not contain secret values,
private incident evidence, access tokens, exploit payloads, or other material
that would increase exposure.

### 3.6 Separate security risk from release ownership

The engineering owner investigates and remediates. The security or repository
owner approves eligible risk. The release owner decides whether a release can
proceed under the policy and must explicitly review every unresolved
release-blocking finding.

## 4. Repository Artifacts

The control is implemented by these reviewed files:

- `docs/engineering/ci-security.md` is the contributor-facing policy.
- `.github/security/policy.json` contains the machine-readable blocking,
  exception, ownership, duration, and service-level rules.
- `.github/security/policy.schema.json` versions the policy format.
- `.github/security/exceptions.json` is the only accepted repository exception
  registry.
- `.github/security/exceptions.schema.json` versions the exception format.
- `.github/security/findings.schema.json` defines the normalized scanner report.
- `internal/securitypolicy` validates policy, exceptions, reports, matching, and
  gate decisions.
- `cmd/security-policy` provides the local and CI command-line interface.
- `.github/CODEOWNERS` requires security-owner review of policy and exception
  changes.

The JSON Schemas support editor tooling and external integrations. The Go
validator performs strict decoding and the semantic checks that JSON Schema
cannot fully express, including current-time expiry, approval independence,
maximum duration, review cadence, exact scope, and non-exceptionable classes.

## 5. Common Gate Flow

Every security scanner must eventually use the following sequence:

```text
Run pinned scanner against the intended target
-> retry only a transient scanner failure within the bounded retry policy
-> preserve a redacted raw report when useful
-> normalize findings to .github/security/findings.schema.json
-> validate the policy and exception registry
-> evaluate every normalized finding through cmd/security-policy
-> upload the redacted report and policy decision when required
-> return the evaluator's gate result to CI
```

The scanner is responsible for finding security issues. The common evaluator
is responsible for consistent disposition and exceptions. Neither replaces the
other.

## 6. Scanner Completion and Retry Rules

Security jobs must distinguish findings from tool failures.

### 6.1 Completed scan

A completed scan queried its required data, scanned the intended target,
produced syntactically valid output, and was normalized without dropping
findings. A completed scan can contain zero or many findings.

### 6.2 Incomplete scan

The normalized status must be one of the following when a scan did not
complete:

- `failed` for a scanner process or adapter failure;
- `timed_out` for a bounded timeout;
- `data_unavailable` when required advisory or metadata sources are unavailable;
- `invalid_report` when scanner output cannot be parsed or validated.

All four statuses fail the gate as incomplete.

### 6.3 Bounded retry

A scanner integration may make one initial attempt and one retry for a
transient network, service, or data error. A third attempt requires an explicit
workflow change reviewed with the scanner integration. Findings themselves are
not retried away.

The normalized report records the number of attempts. A retry must use the same
target identity. It must not silently change the image, commit, dependency
graph, or rule set being scanned.

### 6.4 Invalid reports

Unknown fields, missing required identity, malformed timestamps, duplicate
fingerprints, unredacted secret findings, and unsupported finding types make a
report invalid. Invalid reports fail before exception matching.

## 7. Blocking and Triage Policy

### 7.1 Secrets

Any detected secret blocks. It remains blocked until one of these outcomes is
recorded:

- the credential is removed and, if it was live, revoked or rotated;
- the value is confirmed through narrow review to be a non-secret fixture or
  false positive.

An exception for a secret finding may use only the `false_positive`
disposition. It must include non-secret evidence without recording the matched
value. If the report marks the value as a live credential, no exception can
match it.

Deleting a live credential from the current file is not containment if it
remains valid or exists in Git history. Incident handling must include
revocation or rotation and history review.

### 7.2 Frontend dependencies

High and critical vulnerabilities in production or development dependencies
block unless an approved, active exception matches exactly. Medium and low
findings are reported and assigned for triage.

The scanner must use the repository lockfile. CI must not run an automatic fix
command or modify the lockfile to obtain a green result.

### 7.3 Go vulnerabilities

A source scan that identifies a reachable Go vulnerability blocks even when
the advisory does not provide a CVSS severity. Reachability is the blocking
criterion.

Module or package findings not shown as reachable are retained for triage. They
must not be presented as proven exploitability. Scanner errors and standard
library or toolchain findings remain distinct from application findings.

### 7.4 Deployable runtime images

High and critical vulnerabilities in a deployable runtime image block unless a
valid exception matches the exact image digest and version. Findings marked
unfixed remain visible in the report and follow the same exception rules.

Build stages and final runtime images are reported separately. When API and
worker targets share an identical final digest, one vulnerability scan may be
reused only after both targets have built and the shared digest has been
verified.

### 7.5 Development-only images

High and critical findings in development-only images require triage and block
until fixed or covered by an active exception whose environment is exactly
`development` and whose context identifies a development image.

The result must not be described as a production-image release failure. It is a
development dependency gate with its own accountable owner.

### 7.6 Repository and configuration rules

A high or critical violation of a mandatory repository rule blocks. A
contextual exception is permitted only when the originating rule explicitly
sets `exception_allowed` in the normalized finding.

Mandatory rules include controls such as tracked private keys, credential files,
production secrets in Docker or Compose configuration, deliberate exposure of
server-only configuration to a public client bundle, and prohibited generated
or runtime files.

Pattern matching is a focused control and must not be described as proof that
no secret exists.

### 7.7 SAST and code scanning

Supported high and critical findings block when all of these conditions apply:

- scanner confidence is medium or high;
- reachability is `reachable` or `unknown` for a relevant source path;
- the rule is supported by the configured policy.

Low-confidence and demonstrably unreachable findings are retained for triage.
Contextual exceptions require the scanner or adapter to mark the rule as
exceptionable. A tool error is a scanner failure, not a finding.

### 7.8 CI workflow and GitHub Actions supply chain

High and critical workflow findings block. This class includes:

- unpinned third-party actions where immutable pinning is required;
- excessive token permissions;
- unsafe privileged execution of untrusted pull-request code;
- untrusted artifact download or execution;
- unsafe interpolation into shell commands;
- credential exposure through logs or artifacts;
- mutable or unverified tool downloads.

Actions must be pinned to immutable commit SHAs with a version comment. Workflow
tokens use least privilege. A privileged workflow must not execute untrusted PR
code.

### 7.9 License and dependency source policy

High and critical prohibited-license or incompatible-license findings block.
Dependencies from prohibited, untrusted, or unverifiable sources also block.

A legal or repository-owner decision may accept a narrowly scoped eligible
license risk. Malicious-package indicators and unverifiable source integrity
are not ordinary license exceptions.

### 7.10 SBOM generation and validation

A required SBOM that is missing, invalid, incomplete, or cannot be associated
with the release artifact fails the release security gate. This is not
exceptionable.

An SBOM tool failure must not produce an empty document that is reported as a
successful result.

### 7.11 Release provenance, checksums, and signatures

Missing, invalid, or unverifiable release provenance, checksum, or signature
blocks release and is not exceptionable. Verification must be performed against
the artifact being released, not only against a similarly tagged build.

### 7.12 Kubernetes and infrastructure as code

Mandatory high and critical Kubernetes or IaC findings block. Contextual
exceptions are allowed only for rules that explicitly permit them and must
identify the exact path, environment, deployment context, and version.

At minimum, future gates should cover privileged execution, host access,
dangerous capabilities, root containers, writable root filesystems, unrestricted
RBAC, public service exposure, unpinned images, secret material, and missing
resource or security settings where the deployment policy requires them.

### 7.13 Plugin permissions and trust

A plugin permission expansion blocks until the new capability is reviewed. The
finding and exception, when allowed, must identify the exact plugin artifact
digest, version, requested permission, and target environment.

A revoked plugin version or a plugin signed by an untrusted identity is not
exceptionable. The plugin must not be installed or executed.

The exception system cannot authorize a plugin to bypass server-side policy,
target validation, signature validation, or compatibility checks.

### 7.14 End-of-life components

High and critical findings for unsupported runtimes, base images, or
dependencies block. A temporary exception must identify the exact component,
supported migration plan, compensating controls, and expiry.

### 7.15 Security-control regressions

A required security regression test failure is not exceptionable. Examples
include tests proving server-side authorization, policy enforcement, secret
redaction, plugin signature rejection, rollback drift protection, or audit
durability.

If a control is intentionally replaced, the replacement and its test must be
reviewed as a policy change. The existing test must not simply be suppressed.

### 7.16 Required report delivery and schema

Failure to generate, retain, upload, or validate a required security report
fails the gate. The control is not satisfied by a successful scanner process if
reviewers cannot inspect the required redacted evidence.

### 7.17 Lower-severity findings

Medium, low, and informational findings below the applicable blocking threshold
do not fail the pull request by default. They are emitted as `triage` decisions
with the responsible owner from the policy. They must enter the team's issue or
risk-tracking process within the acknowledgement target below.

Repeated lower-severity findings may be promoted through a policy change when
their combination, exploit chain, or affected context creates higher risk.

## 8. Normalized Finding Contract

Scanner output formats differ. Each scanner adapter must produce a report that
matches `.github/security/findings.schema.json` before policy evaluation.

### 8.1 Report identity

Every report identifies:

- schema version;
- scanner ID;
- unique scan ID;
- generation time;
- completion status;
- attempt count;
- target component;
- target artifact digest when applicable;
- environment and CI context;
- target version or revision.

### 8.2 Finding identity

Every finding identifies:

- stable fingerprint;
- scanner rule or advisory ID;
- common finding type;
- severity and confidence when supported;
- reachability when supported;
- whether the rule is mandatory;
- whether the originating rule allows an exception;
- whether the finding represents a live credential;
- exact component and component type;
- repository path when applicable;
- artifact digest when applicable;
- environment and execution context;
- affected version;
- redacted summary;
- optional assigned owner and remediation issue.

### 8.3 Adapter requirements

An adapter must not discard a finding because it lacks CVSS data. It must use
`unknown` for unsupported severity, confidence, or reachability fields.

An adapter must not synthesize `not_reachable` merely because the scanner did
not provide reachability. The correct value is `unknown`.

Secret findings set `redacted: true` and must not include the matched value in
the summary, fingerprint, path, output, or retained artifact.

## 9. Exception Registry

The only accepted exception registry is
`.github/security/exceptions.json`. Scanner-local allowlists do not replace it.
A scanner configuration may contain a reviewed false-positive rule only when
that rule itself is narrow, documented, and unable to conceal unrelated
findings. The common registry remains the source of expiry, ownership, and
approval.

### 9.1 Required exception identity

Every exception contains:

- an ID in `SEC-EXC-YYYY-NNNN` format;
- exact scanner ID;
- exact finding type;
- exact rule ID;
- stable exact fingerprint;
- assigned risk class;
- disposition of `risk_acceptance` or `false_positive`.

### 9.2 Required scope

Every exception is bound to:

- one component;
- one component type;
- a repository-relative path for source, workflow, configuration, Kubernetes,
  or IaC findings;
- exact artifact digest for image and plugin permission findings;
- one environment;
- one execution context;
- an exact version or constrained semantic-version range.

Supported version expressions are:

```text
1.2.3
=1.2.3
>=1.2.0 <1.3.0
=0.3.0-alpha.1
exact:git-560916a
```

Wildcards and global version scopes are invalid.

### 9.3 Required justification

Every exception records:

- a specific reason;
- at least one compensating control;
- false-positive evidence when the disposition is `false_positive`;
- accountable owner;
- requester;
- approver;
- approver role;
- approval time;
- expiry;
- last review and next review;
- HTTPS remediation issue.

Sensitive exploit details and incident evidence belong in an access-controlled
system. The public exception should contain enough redacted information to
explain scope and decision without increasing exploitability.

### 9.4 Exact matching

An active exception applies only when all applicable fields match the normalized
finding. A scanner name, rule, component, environment, context, version, path,
or digest mismatch leaves the finding blocked.

An exception cannot change an incomplete scanner result into a pass.

### 9.5 False positives

A false-positive disposition means the detected material is not the security
condition represented by the rule. It does not mean the risk is small.

A secret false positive must explain why the matched material is a non-secret
without copying it. A known live credential is never a false positive.

### 9.6 Risk acceptance

Risk acceptance means the finding is real, remediation is deferred for a
bounded period, compensating controls are active, and an accountable person has
accepted the residual risk.

Risk acceptance is not available when the policy marks the finding type
non-exceptionable.

## 10. Non-Exceptionable Conditions

The following cannot be suppressed through the ordinary exception registry:

- scanner execution failure after bounded retry;
- target identity or artifact validation failure;
- invalid normalized report;
- required report delivery failure;
- a confirmed live credential exposure;
- malicious-package indicators;
- missing or invalid required SBOM;
- missing, invalid, or unverifiable provenance, checksum, or signature;
- revoked plugin version;
- untrusted plugin signing identity;
- required security-control regression failure.

Resolving one of these conditions requires restoring the control, replacing the
artifact, responding to the incident, or changing the policy through a
separately reviewed design decision. Adding an exception entry is not a valid
resolution.

## 11. Approval Independence and CODEOWNERS

High and critical exceptions require an approver different from the requester
and from the author of the exception change.

The validator checks requester and approver identity. On pull requests that
change the exception registry, CI compares the base and proposed registries and
checks changed entries against the PR author. CODEOWNERS review provides the
repository-level approval evidence.

CODEOWNERS enforcement depends on repository settings. The `main` branch or
repository ruleset must require:

- pull requests for changes;
- at least one approving review;
- code-owner review for owned paths;
- dismissal of stale approvals after new commits;
- required `Security policy` and existing quality checks;
- no force pushes or branch deletion;
- administrator bypass restricted and audited.

The initial CODEOWNER is the repository security owner. As the team grows, the
repository should create a security-maintainers team with at least two eligible
reviewers and replace the individual owner. A one-person team cannot provide
independent approval when that person authors the exception.

## 12. Exception Duration and Review Cadence

Durations run from `approved_at`, not from the first CI run that notices the
exception.

| Risk class | Maximum duration | Maximum review interval | Independent approval |
|---|---:|---:|---|
| Critical | 7 days | 24 hours | Required |
| High | 30 days | 7 days | Required |
| Medium | 90 days | 30 days | Policy reviewer required |
| Low | 180 days | 90 days | Policy reviewer required |

The next review must occur after the last review and no later than expiry. An
overdue review invalidates the registry even when the final expiry is later.

Shorter durations may be required by an incident commander, legal obligation,
customer commitment, or release owner.

## 13. Renewals

Expiry is never extended automatically.

A renewal requires:

1. A new exception ID.
2. `renewal_of` pointing to the prior ID.
3. A new explanation of why remediation is incomplete.
4. Confirmation that compensating controls still operate.
5. A new approval and review timeline.
6. An updated remediation issue.
7. Removal of the expired record when its audit history is preserved elsewhere.

CI verifies that `renewal_of` exists in the pull request's base revision, that
the new record preserves the exact scanner, finding, fingerprint, risk,
disposition and scope, and that approval occurred before the prior exception's
expiry and next-review deadline. `renewal_of` cannot revive an expired or
overdue exception, point to an arbitrary ID, or broaden the accepted risk.

The same maximum duration applies to the new record. Repeated renewals should
trigger escalation to the repository owner and release owner because they
indicate that temporary risk has become normal operating practice.

## 14. Triage and Remediation Service Levels

Targets begin when CI or a scheduled scan first records the finding.

| Risk class | Acknowledge within | Remediate or make risk decision within | Release impact |
|---|---:|---:|---|
| Critical | 4 hours | 24 hours | Blocks merge and release |
| High | 24 hours | 7 days | Blocks merge and release |
| Medium | 3 days | 30 days | Release-owner review if unresolved |
| Low | 10 days | 90 days | Track in normal backlog |

These are maximum initial targets, not promises to delay urgent work. A known
active exploit, credential exposure, malicious dependency, or compromised
release process is handled immediately as an incident.

## 15. Ownership and Escalation

### 15.1 Change author

The change author fixes the introduced finding or supplies the technical context
needed for triage. The author cannot self-approve a high or critical exception.

### 15.2 Engineering owner

The owner named by the policy or normalized report investigates reachability,
proposes remediation, implements compensating controls, and maintains the
remediation issue.

Default policy ownership is:

- frontend team for frontend dependency findings;
- backend team for reachable Go findings;
- integration team for Kubernetes and IaC findings;
- plugin team for plugin permissions and trust;
- security owner for secrets, SAST, workflow supply chain, configuration,
  reporting, and general policy;
- release owner for runtime images, SBOM, integrity, and release artifacts.

### 15.3 Security approver

The security approver confirms classification, scope, compensating controls,
duration, review cadence, and residual risk. Approval must be independent for
high and critical risk.

### 15.4 Release owner

The release owner reviews every unresolved finding whose policy release impact
is `block` or `release_owner_review`. A release cannot proceed while a blocking
finding lacks an eligible active exception.

The review must use the decision report for the exact release commit and
artifact digest.

### 15.5 Escalation route

Escalation proceeds from engineering owner to security owner, then repository
owner, then release owner. Active exploitation or credential compromise enters
the incident process immediately rather than waiting for ordinary SLA expiry.

## 16. Release Rules

A release security decision is valid only for the exact commit and artifacts
evaluated.

Release must stop when:

- a required security gate is blocked or incomplete;
- an exception is expired, malformed, overdue, or out of scope;
- the release artifact does not match the scanned digest;
- SBOM, signature, checksum, or provenance verification fails;
- a plugin dependency is revoked or signed by an untrusted identity;
- a required report cannot be retained and reviewed;
- the release owner has not reviewed unresolved release-impacting findings.

Re-running a job without changing the underlying condition is not a risk
decision.

## 17. Report Retention and Access

Public pull-request output should contain concise, redacted summaries and links
to remediation guidance. It must not expose credentials, private vulnerability
details, customer identifiers, internal infrastructure addresses, or exploit
material.

Raw scanner reports may be retained as restricted CI artifacts when needed for
investigation. Scanner integrations must define:

- artifact retention period;
- who can access the report;
- redaction performed before upload;
- schema and scanner version;
- target commit or digest;
- deletion or incident handling for accidentally captured secrets.

Public OSS documentation may explain the control. Sensitive incident evidence
belongs in a restricted tracker or incident system.

## 18. Local Commands

Validate the repository policy and active exception registry:

```bash
go run ./cmd/security-policy validate \
  --policy .github/security/policy.json \
  --exceptions .github/security/exceptions.json
```

Use a deterministic time while testing fixtures:

```bash
go run ./cmd/security-policy validate \
  --policy .github/security/policy.json \
  --exceptions internal/securitypolicy/testdata/valid-exceptions.json \
  --at 2026-09-28T02:00:00Z
```

Evaluate a normalized scanner report:

```bash
go run ./cmd/security-policy evaluate \
  --policy .github/security/policy.json \
  --exceptions .github/security/exceptions.json \
  --report path/to/normalized-findings.json \
  --output path/to/security-decision.json
```

The command exits:

- `0` when the report is complete and no unexcepted blocking findings remain;
- `1` when the gate is blocked or incomplete;
- `2` when policy, exception, arguments, or report data are malformed.

Run the policy tests:

```bash
go test ./internal/securitypolicy ./cmd/security-policy
```

## 19. Adding a Scanner Gate

A new scanner integration is complete only when it does all of the following:

1. Pins the scanner, action, and downloaded tools to reviewed immutable versions.
2. Uses least-privilege workflow permissions.
3. Identifies the exact target commit, lockfile, image digest, plugin digest, or
   release artifact.
4. Runs on pull requests, default-branch pushes, merge groups when enabled, and
   the appropriate scheduled or release event.
5. Applies a bounded timeout and one transient retry.
6. Distinguishes a finding from scanner failure.
7. Produces a redacted normalized report.
8. Validates the report before evaluation.
9. Uses the shared exception registry and evaluator.
10. Uploads the required redacted evidence even when the gate fails.
11. Returns the evaluator result without `continue-on-error`, unconditional
    success, blanket severity downgrade, or broad path exclusion.
12. Documents the local equivalent, owner, troubleshooting, and report
    retention.

The exact emitted check name must be stable before it is added to the repository
ruleset as a required check.

## 20. Changing the Policy

A policy change affects every security gate and requires:

- a dedicated pull request;
- security-owner review;
- tests for the new blocking and exception behavior;
- schema changes when the stored format changes;
- a policy-version increase for semantic behavior changes;
- migration instructions when existing exceptions or adapters are affected;
- confirmation that required check names remain stable.

Do not weaken a global threshold to make one finding pass. Add a narrow eligible
exception or fix the finding.

## 21. Fixture Coverage

The repository tests currently prove:

- a valid exact exception passes;
- a missing accountable owner fails;
- an expired exception fails;
- wildcard rules, fingerprints, components, and versions fail;
- a requester cannot approve the same high-risk exception;
- a changed high-risk exception cannot name the PR author as approver;
- an unrelated PR author does not invalidate an unchanged exception;
- a matching exception permits an eligible high dependency finding;
- the same mechanism cannot permit a live credential;
- a reachable Go finding blocks without CVSS severity;
- a lower-severity finding is assigned for triage without blocking;
- a scanner timeout produces an incomplete gate;
- report paths, contexts, versions, component types, and required artifact
  digests are validated before evaluation;
- an unredacted secret report is rejected;
- non-exceptionable findings reject registry entries;
- development-image exceptions must stay development-scoped;
- a rule that forbids contextual exception remains blocked;
- an exception with a mismatched version cannot apply;
- semantic and exact version scopes match only their intended versions.

Future scanner tickets must add adapter-specific fixtures proving parsing,
redaction, failure propagation, and exact target identity.

## 22. Known Boundaries

This control establishes policy, exception validation, and a common evaluation
contract. It does not by itself install every scanner listed in the policy.
Scanner-specific CI jobs are separate implementation work and must adopt this
contract as they are added.

CODEOWNERS is committed in this change, but it is enforced only after the
repository ruleset requires code-owner review. Required-check configuration is
also a repository setting and must be verified after the `Security policy`
check name appears on a successful pull request.

The evaluator cannot prove that a human identity written into JSON is truthful.
Independent GitHub review, CODEOWNERS, protected branches, and audit history
provide that external evidence.

## 23. Contributor Checklist

Before opening a change that affects security findings or exceptions, confirm:

- the scanner still executes against the intended target;
- the report contains no secret value;
- the finding type and rule exist in policy;
- the fingerprint is stable and narrow;
- the component, path, environment, context, version, and digest are exact;
- the remediation issue is accessible to the responsible team;
- compensating controls already exist and can be checked;
- the approver is eligible and independent where required;
- expiry and next review comply with the risk class;
- release impact is understood;
- local policy tests pass.

Green CI is evidence that the defined checks completed under this policy. It is
not proof that the repository contains no vulnerability. The team must continue
to review coverage, new attack paths, scanner quality, and the threat model.

## CodeQL verification after a main-branch failure

Pull-request CodeQL runs use changed-code filtering. A green PR does not establish that the full default-branch scan is clean. The `CodeQL` workflow also supports manual dispatch against a selected branch for a full scan before merging a scanner or security remediation:

```bash
gh workflow run codeql.yml --repo consize-oss/consize --ref BRANCH_NAME
```

Verify both language jobs and their redacted policy decisions. Identical CodeQL findings at the same rule/file/line are normalized once; conflicting duplicate metadata fails closed. Distinct locations remain separate findings. This does not lower severity, ignore report errors or create an exception. Reports and decisions retain the existing 14-day artifact retention and repository visibility; raw source-bearing SARIF is not uploaded by this workflow.
