package securitypolicy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var fixtureTime = time.Date(2026, 9, 28, 2, 0, 0, 0, time.UTC)

func TestRepositoryPolicyAndEmptyRegistry(t *testing.T) {
	policy := loadPolicyForTest(t)
	if err := ValidatePolicy(policy); err != nil {
		t.Fatalf("repository policy is invalid: %v", err)
	}
	registry, err := LoadExceptions(filepath.Join("..", "..", ".github", "security", "exceptions.json"))
	if err != nil {
		t.Fatal(err)
	}
	checkTime := fixtureTime
	if len(registry.Exceptions) > 0 {
		checkTime = time.Now().UTC()
	}
	if err := ValidateExceptions(policy, registry, checkTime, ""); err != nil {
		t.Fatalf("repository exception registry is invalid: %v", err)
	}
}

func TestSchemaDocumentsAreValidJSON(t *testing.T) {
	for _, name := range []string{"policy.schema.json", "exceptions.schema.json", "findings.schema.json"} {
		content, err := os.ReadFile(filepath.Join("..", "..", ".github", "security", name))
		if err != nil {
			t.Fatal(err)
		}
		var schema map[string]any
		if err := json.Unmarshal(content, &schema); err != nil {
			t.Fatalf("%s is not valid JSON: %v", name, err)
		}
		if schema["$schema"] != "https://json-schema.org/draft/2020-12/schema" {
			t.Fatalf("%s does not declare JSON Schema 2020-12", name)
		}
	}
}

func TestExceptionFixtures(t *testing.T) {
	policy := loadPolicyForTest(t)
	tests := []struct {
		name      string
		fixture   string
		wantError string
	}{
		{name: "valid", fixture: "valid-exceptions.json"},
		{name: "missing owner", fixture: "invalid-exceptions.json", wantError: "ownership.owner"},
		{name: "expired", fixture: "expired-exceptions.json", wantError: "expired"},
		{name: "blanket scope", fixture: "broad-exceptions.json", wantError: "must not contain glob"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registry := loadRegistryFixture(t, test.fixture)
			err := ValidateExceptions(policy, registry, fixtureTime, "")
			if test.wantError == "" && err != nil {
				t.Fatalf("expected valid fixture: %v", err)
			}
			if test.wantError != "" && (err == nil || !strings.Contains(err.Error(), test.wantError)) {
				t.Fatalf("expected error containing %q, got %v", test.wantError, err)
			}
		})
	}
}

func TestHighRiskApprovalMustBeIndependent(t *testing.T) {
	policy := loadPolicyForTest(t)
	registry := loadRegistryFixture(t, "valid-exceptions.json")
	registry.Exceptions[0].Ownership.ApprovedBy = registry.Exceptions[0].Ownership.RequestedBy
	err := ValidateExceptions(policy, registry, fixtureTime, "")
	if err == nil || !strings.Contains(err.Error(), "independent") {
		t.Fatalf("expected independent-approval error, got %v", err)
	}
}

func TestChangeAuthorCannotApproveHighRiskException(t *testing.T) {
	policy := loadPolicyForTest(t)
	registry := loadRegistryFixture(t, "valid-exceptions.json")
	err := ValidateExceptions(policy, registry, fixtureTime, "bob")
	if err == nil || !strings.Contains(err.Error(), "change author") {
		t.Fatalf("expected change-author error, got %v", err)
	}
}

func TestUnchangedExceptionDoesNotBindUnrelatedChangeAuthor(t *testing.T) {
	registry := loadRegistryFixture(t, "valid-exceptions.json")
	if err := ValidateExceptionChanges(registry, registry, "bob"); err != nil {
		t.Fatalf("unchanged exception must not bind an unrelated PR author: %v", err)
	}
}

func TestChangedExceptionCannotBeApprovedByChangeAuthor(t *testing.T) {
	baseline := loadRegistryFixture(t, "valid-exceptions.json")
	current := loadRegistryFixture(t, "valid-exceptions.json")
	current.Exceptions[0].Reason += " Reviewed renewal."
	err := ValidateExceptionChanges(baseline, current, "bob")
	if err == nil || !strings.Contains(err.Error(), "change author") {
		t.Fatalf("expected changed-exception author error, got %v", err)
	}
}

func TestRenewalMustReferenceMatchingActiveBaselineException(t *testing.T) {
	baseline := loadRegistryFixture(t, "valid-exceptions.json")
	newRenewal := func() ExceptionRegistry {
		current := loadRegistryFixture(t, "valid-exceptions.json")
		renewal := current.Exceptions[0]
		renewal.ID = "SEC-EXC-2026-0002"
		renewal.RenewalOf = "SEC-EXC-2026-0001"
		renewal.RenewalReason = "The upstream fix remains incompatible after another reviewed test cycle."
		renewal.Timeline.ApprovedAt = "2026-10-04T00:00:00Z"
		renewal.Timeline.LastReviewedAt = "2026-10-04T00:00:00Z"
		renewal.Timeline.NextReviewAt = "2026-10-11T00:00:00Z"
		renewal.Timeline.ExpiresAt = "2026-10-18T00:00:00Z"
		current.Exceptions = []Exception{renewal}
		return current
	}

	t.Run("valid renewal", func(t *testing.T) {
		if err := ValidateExceptionChanges(baseline, newRenewal(), "carol"); err != nil {
			t.Fatalf("valid renewal was rejected: %v", err)
		}
	})

	t.Run("unknown prior id", func(t *testing.T) {
		current := newRenewal()
		current.Exceptions[0].RenewalOf = "SEC-EXC-2026-9999"
		err := ValidateExceptionChanges(baseline, current, "carol")
		if err == nil || !strings.Contains(err.Error(), "base revision") {
			t.Fatalf("expected unknown prior rejection, got %v", err)
		}
	})

	t.Run("changed scope", func(t *testing.T) {
		current := newRenewal()
		current.Exceptions[0].Scope.Component = "npm:another-package"
		err := ValidateExceptionChanges(baseline, current, "carol")
		if err == nil || !strings.Contains(err.Error(), "preserve") {
			t.Fatalf("expected changed-scope rejection, got %v", err)
		}
	})

	t.Run("approved after expiry", func(t *testing.T) {
		current := newRenewal()
		current.Exceptions[0].Timeline.ApprovedAt = "2026-10-21T00:00:00Z"
		err := ValidateExceptionChanges(baseline, current, "carol")
		if err == nil || !strings.Contains(err.Error(), "expired before renewal") {
			t.Fatalf("expected expired-prior rejection, got %v", err)
		}
	})

	t.Run("approved after overdue review", func(t *testing.T) {
		current := newRenewal()
		current.Exceptions[0].Timeline.ApprovedAt = "2026-10-06T00:00:00Z"
		err := ValidateExceptionChanges(baseline, current, "carol")
		if err == nil || !strings.Contains(err.Error(), "review was overdue") {
			t.Fatalf("expected overdue-review rejection, got %v", err)
		}
	})
}

func TestEvaluation(t *testing.T) {
	policy := loadPolicyForTest(t)
	empty := ExceptionRegistry{Schema: "./exceptions.schema.json", SchemaVersion: 1, PolicyVersion: policy.PolicyVersion, Exceptions: []Exception{}}
	tests := []struct {
		name         string
		registryFile string
		reportFile   string
		wantGate     string
		wantDecision string
	}{
		{name: "exact exception", registryFile: "valid-exceptions.json", reportFile: "high-finding-report.json", wantGate: GatePassed, wantDecision: DecisionExcepted},
		{name: "unexcepted high", reportFile: "high-finding-report.json", wantGate: GateBlocked, wantDecision: DecisionBlock},
		{name: "live secret cannot be excepted", registryFile: "live-secret-exception.json", reportFile: "live-secret-report.json", wantGate: GateBlocked, wantDecision: DecisionBlock},
		{name: "reachable Go advisory without severity", reportFile: "reachable-go-report.json", wantGate: GateBlocked, wantDecision: DecisionBlock},
		{name: "lower severity is triaged", reportFile: "lower-severity-report.json", wantGate: GatePassed, wantDecision: DecisionTriage},
		{name: "scanner timeout is incomplete", reportFile: "failed-scanner-report.json", wantGate: GateIncomplete, wantDecision: DecisionBlock},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registry := empty
			if test.registryFile != "" {
				registry = loadRegistryFixture(t, test.registryFile)
			}
			report, err := LoadReport(filepath.Join("testdata", test.reportFile))
			if err != nil {
				t.Fatal(err)
			}
			result, err := Evaluate(policy, registry, report, fixtureTime)
			if err != nil {
				t.Fatal(err)
			}
			if result.Gate != test.wantGate {
				t.Fatalf("gate = %q, want %q", result.Gate, test.wantGate)
			}
			if len(result.Decisions) != 1 || result.Decisions[0].Decision != test.wantDecision {
				t.Fatalf("decisions = %#v, want %q", result.Decisions, test.wantDecision)
			}
		})
	}
}

func TestExceptionRiskClassCannotUnderstateFinding(t *testing.T) {
	policy := loadPolicyForTest(t)
	registry := loadRegistryFixture(t, "valid-exceptions.json")
	registry.Exceptions[0].RiskClass = "low"
	report, err := LoadReport(filepath.Join("testdata", "high-finding-report.json"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := Evaluate(policy, registry, report, fixtureTime)
	if err != nil {
		t.Fatal(err)
	}
	if result.Gate != GateBlocked || result.Counts.Blocked != 1 {
		t.Fatalf("under-classified exception must not match: %#v", result)
	}
}

func TestExceptionScopeMustMatchExactly(t *testing.T) {
	policy := loadPolicyForTest(t)
	registry := loadRegistryFixture(t, "valid-exceptions.json")
	report, err := LoadReport(filepath.Join("testdata", "high-finding-report.json"))
	if err != nil {
		t.Fatal(err)
	}
	report.Findings[0].Version = "16.4.0"
	result, err := Evaluate(policy, registry, report, fixtureTime)
	if err != nil {
		t.Fatal(err)
	}
	if result.Gate != GateBlocked || result.Counts.Blocked != 1 {
		t.Fatalf("version-mismatched exception must not match: %#v", result)
	}
}

func TestOriginatingRuleCanForbidException(t *testing.T) {
	policy := loadPolicyForTest(t)
	registry := loadRegistryFixture(t, "valid-exceptions.json")
	report, err := LoadReport(filepath.Join("testdata", "high-finding-report.json"))
	if err != nil {
		t.Fatal(err)
	}
	report.Findings[0].ExceptionAllowed = false
	result, err := Evaluate(policy, registry, report, fixtureTime)
	if err != nil {
		t.Fatal(err)
	}
	if result.Gate != GateBlocked || result.Counts.Blocked != 1 {
		t.Fatalf("finding that forbids contextual exception must block: %#v", result)
	}
}

func TestNonExceptionableFindingRejectsRegistryEntry(t *testing.T) {
	policy := loadPolicyForTest(t)
	registry := loadRegistryFixture(t, "valid-exceptions.json")
	registry.Exceptions[0].FindingType = "release_integrity"
	err := ValidateExceptions(policy, registry, fixtureTime, "")
	if err == nil || !strings.Contains(err.Error(), "not exceptionable") {
		t.Fatalf("expected non-exceptionable finding error, got %v", err)
	}
}

func TestSecretReportMustBeRedacted(t *testing.T) {
	policy := loadPolicyForTest(t)
	report, err := LoadReport(filepath.Join("testdata", "live-secret-report.json"))
	if err != nil {
		t.Fatal(err)
	}
	report.Findings[0].Redacted = false
	if err := ValidateReport(policy, report, fixtureTime); err == nil || !strings.Contains(err.Error(), "must be redacted") {
		t.Fatalf("expected unredacted-secret error, got %v", err)
	}
}

func TestReportIdentityMustBeExact(t *testing.T) {
	policy := loadPolicyForTest(t)
	tests := []struct {
		name      string
		mutate    func(*FindingReport)
		wantError string
	}{
		{name: "target environment", mutate: func(report *FindingReport) { report.Target.Environment = "everywhere" }, wantError: "target environment is invalid"},
		{name: "target context", mutate: func(report *FindingReport) { report.Target.Context = "manual" }, wantError: "target context is invalid"},
		{name: "target version", mutate: func(report *FindingReport) { report.Target.Version = "*" }, wantError: "target requires"},
		{name: "component type", mutate: func(report *FindingReport) { report.Findings[0].ComponentType = "anything" }, wantError: "component_type is invalid"},
		{name: "finding path", mutate: func(report *FindingReport) { report.Findings[0].Path = "ui/**" }, wantError: "path must be an exact"},
		{name: "artifact digest", mutate: func(report *FindingReport) { report.Findings[0].FindingType = "runtime_image" }, wantError: "require an exact artifact_digest"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			report, err := LoadReport(filepath.Join("testdata", "high-finding-report.json"))
			if err != nil {
				t.Fatal(err)
			}
			test.mutate(&report)
			err = ValidateReport(policy, report, fixtureTime)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("expected error containing %q, got %v", test.wantError, err)
			}
		})
	}
}

func TestPolicySemanticValuesAreValidated(t *testing.T) {
	policy := loadPolicyForTest(t)
	policy.FindingRules[0].Owner = "nobody"
	criticalLimit := policy.ExceptionLimits["critical"]
	criticalLimit.ApproverRoles = append(criticalLimit.ApproverRoles, "author")
	policy.ExceptionLimits["critical"] = criticalLimit
	policy.ServiceLevels["critical"] = ServiceLevel{
		AcknowledgementHours:     4,
		RemediationDecisionHours: 24,
		ReleaseImpact:            "ignore",
	}
	err := ValidatePolicy(policy)
	if err == nil || !strings.Contains(err.Error(), "invalid owner") || !strings.Contains(err.Error(), "invalid release impact") || !strings.Contains(err.Error(), "invalid approver role") {
		t.Fatalf("expected invalid policy semantic values, got %v", err)
	}
}

func TestDevelopmentImageExceptionRequiresDevelopmentScope(t *testing.T) {
	policy := loadPolicyForTest(t)
	registry := loadRegistryFixture(t, "valid-exceptions.json")
	exception := &registry.Exceptions[0]
	exception.FindingType = "development_image"
	exception.Scope.ComponentType = "development_image"
	exception.Scope.Environment = "production"
	exception.Scope.Context = "development_image"
	exception.Scope.ArtifactDigest = "sha256:" + strings.Repeat("a", 64)
	err := ValidateExceptions(policy, registry, fixtureTime, "")
	if err == nil || !strings.Contains(err.Error(), "exception environment must be development") {
		t.Fatalf("expected development-scope error, got %v", err)
	}
}

func TestVersionRange(t *testing.T) {
	tests := []struct {
		constraint string
		candidate  string
		want       bool
	}{
		{constraint: ">=1.2.0 <2.0.0", candidate: "1.9.3", want: true},
		{constraint: ">=1.2.0 <2.0.0", candidate: "2.0.0", want: false},
		{constraint: "=0.3.0-alpha.1", candidate: "0.3.0-alpha.1", want: true},
		{constraint: "exact:git-abc123", candidate: "git-abc123", want: true},
		{constraint: "exact:git-abc123", candidate: "git-def456", want: false},
	}
	for _, test := range tests {
		if got := versionMatches(test.constraint, test.candidate); got != test.want {
			t.Errorf("versionMatches(%q, %q) = %v, want %v", test.constraint, test.candidate, got, test.want)
		}
	}
	if err := validateVersionRange("*"); err == nil {
		t.Fatal("wildcard version range must be rejected")
	}
	if err := validateVersionRange("999999999999999999999.0.0"); err == nil {
		t.Fatal("version components outside the supported numeric range must be rejected")
	}
}

func loadPolicyForTest(t *testing.T) Policy {
	t.Helper()
	policy, err := LoadPolicy(filepath.Join("..", "..", ".github", "security", "policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	return policy
}

func loadRegistryFixture(t *testing.T, name string) ExceptionRegistry {
	t.Helper()
	registry, err := LoadExceptions(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return registry
}
