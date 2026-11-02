package dependencysecurity

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/consize-oss/consize/internal/securitypolicy"
)

func options() Options {
	return Options{Context: "pull_request", Revision: "test-revision", Path: "ui/package-lock.json", Now: time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)}
}

const lockFixture = `{"lockfileVersion":3,"packages":{"":{"name":"fixture"},"node_modules/example":{"version":"1.0.0","dev":true},"node_modules/parent/node_modules/example":{"version":"1.0.0"}}}`
const auditFixture = `{"auditReportVersion":2,"vulnerabilities":{"example":{"name":"example","severity":"high","via":[{"source":1234,"name":"example","title":"SECRET MUST NOT BE COPIED","url":"https://example.invalid/advisory","severity":"high","range":"<1.0.1"}],"nodes":["node_modules/example","node_modules/parent/node_modules/example"],"range":"<1.0.1","fixAvailable":{"name":"example","version":"1.0.1","isSemVerMajor":false}}},"metadata":{"vulnerabilities":{"total":1,"high":1}}}`

func TestNPMAuditIncludesDevAndRedacts(t *testing.T) {
	sensitive := url.URL{Scheme: "https", Host: "example.invalid", Path: "/advisory", User: url.UserPassword("synthetic-user", "synthetic-password")}
	input := strings.Replace(auditFixture, "https://example.invalid/advisory", sensitive.String(), 1)
	report, err := NormalizeNPM(strings.NewReader(input), strings.NewReader(lockFixture), options())
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Findings) != 1 {
		t.Fatalf("want deduplicated package/version: %+v", report)
	}
	f := report.Findings[0]
	if f.Severity != "high" || f.Version != "1.0.0" || f.Reachability != "not_applicable" {
		t.Fatalf("wrong npm semantics: %+v", f)
	}
	for _, required := range []string{"development", "production", "1.0.1"} {
		if !strings.Contains(f.Summary, required) {
			t.Fatalf("missing %s: %s", required, f.Summary)
		}
	}
	if strings.Contains(f.Summary, "SECRET") || strings.Contains(f.Summary, "password") {
		t.Fatal("untrusted raw metadata leaked")
	}
	assertGate(t, report, securitypolicy.GateBlocked)
}
func TestNPMAuditRejectsIncompleteAndUnknownData(t *testing.T) {
	for _, input := range []string{`{}`, `{"error":{"code":"EAUDIT"}}`, strings.Replace(auditFixture, `"auditReportVersion":2`, `"auditReportVersion":99`, 1), strings.Replace(auditFixture, `"severity":"high"`, `"severity":"unknown"`, 1), strings.Replace(auditFixture, `"nodes":["node_modules/example","node_modules/parent/node_modules/example"]`, `"nodes":["node_modules/missing"]`, 1), auditFixture + `{}`} {
		if _, err := NormalizeNPM(strings.NewReader(input), strings.NewReader(lockFixture), options()); err == nil {
			t.Fatalf("accepted malformed npm report: %s", input)
		}
	}
	clean := `{"auditReportVersion":2,"vulnerabilities":{},"metadata":{"vulnerabilities":{"total":0}}}`
	report, err := NormalizeNPM(strings.NewReader(clean), strings.NewReader(lockFixture), options())
	if err != nil {
		t.Fatal(err)
	}
	assertGate(t, report, securitypolicy.GatePassed)
	report, err = NormalizeNPM(strings.NewReader(strings.ReplaceAll(auditFixture, `"severity":"high"`, `"severity":"moderate"`)), strings.NewReader(lockFixture), options())
	if err != nil {
		t.Fatal(err)
	}
	assertGate(t, report, securitypolicy.GatePassed)
}

const goFixtureConfig = `{"config":{"protocol_version":"v1.0.0","scanner_name":"govulncheck","scanner_version":"v1.9.0","scan_level":"symbol","scan_mode":"source","go_version":"go1.27.0"}}
{"SBOM":{"go_version":"go1.27.0","modules":[{"path":"example.org/module","version":"v1.0.0"}],"roots":["example.org/main"]}}
{"osv":{"id":"GO-2099-0001","summary":"DO NOT COPY SOURCE DETAILS"}}
`

func TestGoReachabilityStrongestObservationWins(t *testing.T) {
	input := goFixtureConfig + `{"finding":{"osv":"GO-2099-0001","fixed_version":"v1.0.1","trace":[{"module":"example.org/module","version":"v1.0.0"}]}}
{"finding":{"osv":"GO-2099-0001","fixed_version":"v1.0.1","trace":[{"module":"example.org/module","version":"v1.0.0","package":"example.org/module/lib","function":"Vulnerable"},{"module":"example.org/main","function":"main"}]}}
{"finding":{"osv":"GO-2099-0001","fixed_version":"v1.0.1","trace":[{"module":"example.org/module","version":"v1.0.0","package":"example.org/module/lib"}]}}`
	report, err := NormalizeGo(strings.NewReader(input), options())
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Findings) != 1 || report.Findings[0].Reachability != "reachable" || report.Findings[0].Severity != "unknown" {
		t.Fatalf("incorrect merged reachability: %+v", report)
	}
	if !strings.Contains(report.Findings[0].Summary, "v1.0.1") {
		t.Fatal("missing remediation")
	}
	assertGate(t, report, securitypolicy.GateBlocked)
}
func TestGoModulePresenceDoesNotProveReachability(t *testing.T) {
	for _, frame := range []string{`{"module":"example.org/module","version":"v1.0.0"}`, `{"module":"example.org/module","version":"v1.0.0","package":"example.org/module/lib"}`} {
		report, err := NormalizeGo(strings.NewReader(goFixtureConfig+`{"finding":{"osv":"GO-2099-0001","trace":[`+frame+`]}}`), options())
		if err != nil {
			t.Fatal(err)
		}
		if report.Findings[0].Reachability != "not_reachable" {
			t.Fatal("module/package presence labeled reachable")
		}
		assertGate(t, report, securitypolicy.GatePassed)
	}
	input := goFixtureConfig + `{"finding":{"osv":"GO-2099-0001","trace":[{"module":"stdlib","version":"v1.27.0","package":"net/http","function":"Serve"},{"module":"example.org/main","function":"main"}]}}`
	report, err := NormalizeGo(strings.NewReader(input), options())
	if err != nil {
		t.Fatal(err)
	}
	if report.Findings[0].Component != "go:stdlib" {
		t.Fatal("stdlib missing")
	}
	assertGate(t, report, securitypolicy.GateBlocked)
}
func TestGoRejectsTruncatedOrWrongAnalysis(t *testing.T) {
	for _, input := range []string{"", `{"config":{}}`, strings.Replace(goFixtureConfig, `"scan_level":"symbol"`, `"scan_level":"module"`, 1), goFixtureConfig + `{"finding":`, goFixtureConfig + `{"finding":{"osv":"GO-2099-0001","trace":[]}}`, goFixtureConfig + `{"finding":{"osv":"GO-2099-9999","trace":[{"module":"missing"}]}}`, goFixtureConfig + `{"error":"outage"}`} {
		if _, err := NormalizeGo(strings.NewReader(input), options()); err == nil {
			t.Fatalf("accepted incomplete Go report: %s", input)
		}
	}
	report, err := NormalizeGo(strings.NewReader(goFixtureConfig), options())
	if err != nil {
		t.Fatal(err)
	}
	assertGate(t, report, securitypolicy.GatePassed)
}
func assertGate(t *testing.T, report securitypolicy.FindingReport, expected string) {
	t.Helper()
	policy, err := securitypolicy.LoadPolicy("../../.github/security/policy.json")
	if err != nil {
		t.Fatal(err)
	}
	registry, err := securitypolicy.LoadExceptions("../../.github/security/exceptions.json")
	if err != nil {
		t.Fatal(err)
	}
	result, err := securitypolicy.Evaluate(policy, registry, report, options().Now)
	if err != nil {
		t.Fatal(err)
	}
	if result.Gate != expected {
		t.Fatalf("want %s, got %+v", expected, result)
	}
}

func TestMetavulnerabilityFingerprintBindsConcreteAdvisory(t *testing.T) {
	audit := npmAudit{}
	if err := json.Unmarshal([]byte(auditFixture), &audit); err != nil {
		t.Fatal(err)
	}
	child := audit.Vulnerabilities["example"]
	parent := child
	parent.Name = "parent"
	parent.Via = []json.RawMessage{json.RawMessage(`"example"`)}
	parent.Nodes = []string{"node_modules/parent"}
	audit.Vulnerabilities["parent"] = parent
	audit.Metadata.Vulnerabilities.Total = 2
	lock := strings.Replace(lockFixture, `"node_modules/example":`, `"node_modules/parent":{"version":"2.0.0","dev":true},"node_modules/example":`, 1)
	first, _ := json.Marshal(audit)
	report, err := NormalizeNPM(strings.NewReader(string(first)), strings.NewReader(lock), options())
	if err != nil {
		t.Fatal(err)
	}
	var before string
	for _, f := range report.Findings {
		if f.Component == "npm:parent" {
			before = f.Fingerprint
		}
	}
	child.Via = []json.RawMessage{json.RawMessage(`{"source":5678}`)}
	audit.Vulnerabilities["example"] = child
	second, _ := json.Marshal(audit)
	report, err = NormalizeNPM(strings.NewReader(string(second)), strings.NewReader(lock), options())
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range report.Findings {
		if f.Component == "npm:parent" && f.Fingerprint == before {
			t.Fatal("parent exception fingerprint failed to bind the new advisory")
		}
	}
}
