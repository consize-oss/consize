package codeqlreport

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/consize-oss/consize/internal/securitypolicy"
)

const fixture = `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"CodeQL","rules":[{"id":"go/synthetic","properties":{"security-severity":"9.1","precision":"high"}}]}},"invocations":[{"executionSuccessful":true}],"results":[{"ruleId":"go/synthetic","message":{"text":"DO NOT RETAIN source secret"},"locations":[{"physicalLocation":{"artifactLocation":{"uri":"internal/example.go"},"region":{"startLine":5}}}]}]}]}`

func TestNormalizeAndFailClosed(t *testing.T) {
	target := securitypolicy.ReportTarget{Component: "consize-source", Environment: "ci", Context: "build", Version: "synthetic-revision"}
	report, err := Normalize([]byte(fixture), target, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Findings) != 1 || report.Findings[0].Severity != "critical" || report.Findings[0].Confidence != "high" || report.Findings[0].Reachability != "unknown" {
		t.Fatalf("incorrect finding: %+v", report)
	}
	b, _ := json.Marshal(report)
	if strings.Contains(string(b), "DO NOT RETAIN") {
		t.Fatal("source text leaked")
	}
	for _, invalid := range []string{"{", `{"version":"2.1.0","runs":[]}`, strings.Replace(fixture, `"executionSuccessful":true`, `"executionSuccessful":false`, 1), strings.Replace(fixture, `go/synthetic","message`, `unknown/rule","message`, 1), strings.Replace(fixture, `internal/example.go`, `../outside`, 1)} {
		if _, err := Normalize([]byte(invalid), target, time.Now()); err == nil {
			t.Fatalf("accepted invalid input %s", invalid)
		}
	}
	clean := `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"CodeQL","rules":[]}},"results":[]}]}`
	report, err = Normalize([]byte(clean), target, time.Now())
	if err != nil || len(report.Findings) != 0 {
		t.Fatal("clean result rejected")
	}
}

func TestCodeQLPackRuleReferences(t *testing.T) {
	input := `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"CodeQL"},"extensions":[{"rules":[{"id":"js/example","properties":{"security-severity":"8","precision":"high"}}]}]},"results":[{"rule":{"index":0,"toolComponent":{"index":0}},"locations":[{"physicalLocation":{"artifactLocation":{"uri":"ui/src/example.ts"},"region":{"startLine":1}}}]}]}]}`
	report, err := Normalize([]byte(input), securitypolicy.ReportTarget{Component: "consize-source", Environment: "ci", Context: "build", Version: "test-revision"}, time.Now())
	if err != nil || len(report.Findings) != 1 || report.Findings[0].RuleID != "js/example" {
		t.Fatalf("pack rule reference: %+v %v", report, err)
	}
}

func TestDuplicateFindingsAreNormalizedWithoutHidingDistinctLocations(t *testing.T) {
	var doc map[string]any
	if err := json.Unmarshal([]byte(fixture), &doc); err != nil {
		t.Fatal(err)
	}
	runs := doc["runs"].([]any)
	run := runs[0].(map[string]any)
	result := run["results"].([]any)[0]
	run["results"] = []any{result, result}
	data, _ := json.Marshal(doc)
	target := securitypolicy.ReportTarget{Component: "consize-source", Environment: "ci", Context: "default_branch", Version: "synthetic-revision"}
	report, err := Normalize(data, target, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Findings) != 1 {
		t.Fatalf("duplicate findings retained: %d", len(report.Findings))
	}
	policy, err := securitypolicy.LoadPolicy("../../.github/security/policy.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = securitypolicy.ValidateReport(policy, report, time.Now()); err != nil {
		t.Fatal("normalized duplicates rejected by shared policy", err)
	}
	exceptions, err := securitypolicy.LoadExceptions("../../.github/security/exceptions.json")
	if err != nil {
		t.Fatal(err)
	}
	decision, err := securitypolicy.Evaluate(policy, exceptions, report, time.Now())
	if err != nil || decision.Gate != "blocked" || decision.Counts.Blocked != 1 {
		t.Fatalf("deduplication weakened blocking: %+v %v", decision, err)
	}
	// A distinct location must still be reported, even when the rule is identical.
	var other any
	encoded, _ := json.Marshal(result)
	_ = json.Unmarshal(encoded, &other)
	other.(map[string]any)["locations"].([]any)[0].(map[string]any)["physicalLocation"].(map[string]any)["region"].(map[string]any)["startLine"] = 6
	run["results"] = []any{result, result, other}
	data, _ = json.Marshal(doc)
	report, err = Normalize(data, target, time.Now())
	if err != nil || len(report.Findings) != 2 {
		t.Fatalf("distinct location lost: %d %v", len(report.Findings), err)
	}
	// Repeated runs are deduplicated too; conflicting metadata fails closed.
	doc["runs"] = []any{run, run}
	data, _ = json.Marshal(doc)
	report, err = Normalize(data, target, time.Now())
	if err != nil || len(report.Findings) != 2 {
		t.Fatal("cross-run duplicates retained", err)
	}
	var conflicting any
	encoded, _ = json.Marshal(run)
	_ = json.Unmarshal(encoded, &conflicting)
	conflicting.(map[string]any)["tool"].(map[string]any)["driver"].(map[string]any)["rules"].([]any)[0].(map[string]any)["properties"].(map[string]any)["security-severity"] = "2.0"
	doc["runs"] = []any{run, conflicting}
	data, _ = json.Marshal(doc)
	if _, err = Normalize(data, target, time.Now()); err == nil {
		t.Fatal("conflicting duplicate metadata accepted")
	}
}
