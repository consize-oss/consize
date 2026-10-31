package dependencysecurity

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/consize-oss/consize/internal/securitypolicy"
)

func TestScannerFailuresAreIncompleteAndRetriesBounded(t *testing.T) {
	cases := []struct {
		name, scanner string
		results       []processResult
		status        string
		attempts      int
	}{
		{"npm outage", "npm-audit", []processResult{{Exit: 1, Output: []byte(`{"error":{"code":"ETIMEDOUT","message":"credential=DO_NOT_RETAIN"}}`)}}, "data_unavailable", 2},
		{"npm invalid output", "npm-audit", []processResult{{Exit: 1, Output: []byte(`{`)}}, "invalid_report", 1},
		{"npm scanner crash", "npm-audit", []processResult{{Exit: 9, Stderr: []byte("private diagnostic")}}, "failed", 1},
		{"Go timeout", "govulncheck", []processResult{{Exit: -1, TimedOut: true}}, "timed_out", 2},
		{"Go partial stream on error", "govulncheck", []processResult{{Exit: 1, Output: []byte(goFixtureConfig)}}, "failed", 1},
		{"Go malformed success", "govulncheck", []processResult{{Exit: 0, Output: []byte(`{`)}}, "invalid_report", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			runner := func(context.Context, string, []string, string, []string) processResult { calls++; return tc.results[0] }
			report, attempts, _ := scan(context.Background(), time.Second, scanSpec{scanner: tc.scanner}, options(), []byte(lockFixture), nil, ".", runner)
			if calls != tc.attempts || attempts != tc.attempts || report.Status != tc.status {
				t.Fatalf("wrong retry/status: calls=%d attempts=%d status=%s", calls, attempts, report.Status)
			}
			b, _ := json.Marshal(report)
			if strings.Contains(string(b), "DO_NOT_RETAIN") || strings.Contains(string(b), "private diagnostic") {
				t.Fatal("raw scanner diagnostic escaped")
			}
			assertGate(t, report, securitypolicy.GateIncomplete)
		})
	}
}
func TestTransientFailureRecoversButFindingsAreNotRetried(t *testing.T) {
	calls := 0
	runner := func(context.Context, string, []string, string, []string) processResult {
		calls++
		if calls == 1 {
			return processResult{Exit: 1, Output: []byte(`{"error":{"code":"ECONNRESET"}}`)}
		}
		return processResult{Exit: 1, Output: []byte(auditFixture)}
	}
	report, attempts, _ := scan(context.Background(), time.Second, scanSpec{scanner: "npm-audit"}, options(), []byte(lockFixture), nil, ".", runner)
	if attempts != 2 || calls != 2 || report.Status != "completed" {
		t.Fatal("transient failure did not recover")
	}
	assertGate(t, report, securitypolicy.GateBlocked)
	calls = 0
	runner = func(context.Context, string, []string, string, []string) processResult {
		calls++
		return processResult{Exit: 1, Output: []byte(auditFixture)}
	}
	report, _, _ = scan(context.Background(), time.Second, scanSpec{scanner: "npm-audit"}, options(), []byte(lockFixture), nil, ".", runner)
	if calls != 1 {
		t.Fatal("vulnerability findings must not be retried")
	}
	assertGate(t, report, securitypolicy.GateBlocked)
}
func TestExactExceptionAndExpiryApplyToNormalizedFindings(t *testing.T) {
	report, err := NormalizeNPM(strings.NewReader(auditFixture), strings.NewReader(lockFixture), options())
	if err != nil {
		t.Fatal(err)
	}
	policy, err := securitypolicy.LoadPolicy("../../.github/security/policy.json")
	if err != nil {
		t.Fatal(err)
	}
	registry, err := securitypolicy.LoadExceptions("../securitypolicy/testdata/valid-exceptions.json")
	if err != nil {
		t.Fatal(err)
	}
	e := &registry.Exceptions[0]
	f := report.Findings[0]
	e.Scanner = report.Scanner
	e.RuleID = f.RuleID
	e.Fingerprint = f.Fingerprint
	e.Scope.Component = f.Component
	e.Scope.AffectedVersionRange = "=1.0.0"
	now := options().Now
	e.Timeline.ApprovedAt = now.Add(-time.Hour).Format(time.RFC3339)
	e.Timeline.LastReviewedAt = e.Timeline.ApprovedAt
	e.Timeline.NextReviewAt = now.Add(24 * time.Hour).Format(time.RFC3339)
	e.Timeline.ExpiresAt = now.Add(48 * time.Hour).Format(time.RFC3339)
	decision, err := securitypolicy.Evaluate(policy, registry, report, now)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Gate != securitypolicy.GatePassed || decision.Counts.Excepted != 1 {
		t.Fatalf("exact exception failed: %+v", decision)
	}
	// Same package in the tooling lockfile is outside this reviewed scope.
	report.Findings[0].Path = "tools/ci/package-lock.json"
	decision, err = securitypolicy.Evaluate(policy, registry, report, now)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Gate != securitypolicy.GateBlocked {
		t.Fatal("exception crossed lockfile scope")
	}
	e.Timeline.ExpiresAt = now.Add(-time.Minute).Format(time.RFC3339)
	if _, err := securitypolicy.Evaluate(policy, registry, report, now); err == nil {
		t.Fatal("expired exception was accepted")
	}
}

func TestGoRootsIgnoreInstalledToolingAndFixtures(t *testing.T) {
	files := []string{"cmd/consize/main.go", "internal/api/server.go", "cmd/consize/main_test.go", "internal/api/testdata/broken.go", "vendor/example/pkg.go", "tools/ci/node_modules/flatted/golang/pkg/flatted/main.go", "_scratch/debug.go", ".cache/fixture.go"}
	all, commands := goRoots(files)
	if strings.Join(all, ",") != "./cmd/consize,./internal/api" || strings.Join(commands, ",") != "./cmd/consize" {
		t.Fatalf("unexpected scan roots: %v %v", all, commands)
	}
}
