# Dependency security (CON-59)

The `Dependency security` check audits the full production and development graphs in `ui/package-lock.json` and `tools/ci/package-lock.json`, then runs govulncheck source/symbol analysis for both the tracked first-party package roots and their command entry-point roots from the root Go module. Roots are derived from tracked Go source, excluding fixture/vendor directories; installing npm packages that contain auxiliary Go files cannot silently change the analyzed roots. It runs on pull requests, main pushes, merge groups, weekly schedules and manual dispatch. No source, lockfile or dependency is repaired in CI.

## Decisions

- npm high/critical findings block; lower severities are retained for triage. Development and optional dependencies are included. npm audit is not source reachability analysis.
- Go findings with vulnerable symbols reported reachable by source analysis block even when advisory severity is absent. Mere module/package presence is retained for triage, not called exploitable. Repeated module/package/symbol findings are combined without losing the strongest result.
- Exact, active exceptions use the existing `.github/security/exceptions.json` policy. Expired, malformed or out-of-scope exceptions fail. Findings never trigger an automatic retry or fix.
- Scanner crashes, malformed output, unsupported protocol/analysis mode and missing source graphs are incomplete checks. Timeouts and advisory-network failures get at most one retry; other failures do not. All four scans are evaluated so one blocked input does not hide another's report.

## Pins and evidence

Node and npm come from `.node-version` and the application's `packageManager`. govulncheck is pinned to v1.9.0 in `internal/dependencysecurity/report.go`, installed through the public Go module proxy with checksum-database verification, and its embedded module/version identity is checked before use. No advisory database is restored from a long-lived CI cache. Each invocation has a timeout; the job is capped at 30 minutes.

The artifact contains per-scan normalized reports and shared-policy decisions plus `manifest.json`: scanner/version, platform, source revision, lockfile or Go module/sum digest, Go/database metadata when available, attempt count, scanner exit and gate result. Raw npm output, diagnostics, OSV descriptions and source traces are never uploaded. npm uses temporary empty user/global configuration files; public registry lookups do not use developer tokens. Artifacts inherit repository visibility and are retained for 14 days; this public repository is not a private incident store.

The stable required-check name is `Dependency security`. Add it to the Protection ruleset only after the clean PR result is reviewed; enforcement qualification belongs to CON-61. Existing required checks remain unchanged.

## Local reproduction

Use the declared Go/Node/npm versions, then run from the repository root:

```bash
scripts/security/install-govulncheck.sh "$PWD/.tools"
go test -race ./internal/dependencysecurity ./internal/securitypolicy ./cmd/dependency-security
go run ./cmd/dependency-security \
  --govulncheck "$PWD/.tools/govulncheck" \
  --output /tmp/consize-dependency-security \
  --revision "$(git rev-parse HEAD)" \
  --context build
```

A nonzero result means a blocked finding, incomplete scan, invalid policy or setup failure. Inspect each `*-report.json`, `*-decision.json` and the manifest. An npm exit of 1 with a valid audit is a finding, not an outage. govulncheck JSON can exit 0 with vulnerabilities, so its stream is always parsed and evaluated.

Fix dependencies in a normal reviewed source change, regenerate lockfiles locally, and run lint/types/unit/build/browser checks for frontend changes. Never use `npm audit fix` in CI or broadly omit dev dependencies to make the check green. If a parent package prevents remediation, retain the finding and use the existing independently reviewed exception process.

## Test plan and boundaries

Risk class F. Synthetic fixtures prove npm/dev blocking, Go reachable blocking, non-reachable triage, standard-library inclusion, repeated findings, exact exception matching, expiry, malformed output, crashes, timeouts and one-retry recovery. Real npm advisory lookups and pinned govulncheck runs establish the external integration path; fixture-only results are not a live scan.

Go results apply to the configured Go version, operating system, architecture, build tags and package roots. Reflection/unsafe and other static-analysis limits can hide runtime paths. This gate does not establish absence of vulnerabilities, scan release binaries/images, . Supported alternative build targets need explicit additional source/binary scans when introduced.

References: [govulncheck command and limitations](https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck), [stream protocol](https://github.com/golang/vuln/blob/v1.9.0/internal/govulncheck/govulncheck.go), [npm audit](https://docs.npmjs.com/cli/commands/npm-audit/), [security policy](ci-security.md).
