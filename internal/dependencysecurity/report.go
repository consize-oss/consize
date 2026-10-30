// Package dependencysecurity normalizes and evaluates dependency scanner output.
package dependencysecurity

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/consize-oss/consize/internal/securitypolicy"
)

const GovulncheckVersion = "v1.9.0"
const maxReportBytes = 64 << 20

type Options struct {
	Context  string
	Revision string
	Path     string
	Now      time.Time
}

func baseReport(scanner string, o Options) securitypolicy.FindingReport {
	return securitypolicy.FindingReport{Schema: "./findings.schema.json", SchemaVersion: 1, Scanner: scanner, ScanID: scanner + "-" + fingerprint(o.Revision, o.Path), GeneratedAt: o.Now.UTC().Format(time.RFC3339), Status: "completed", Attempts: 1, Target: securitypolicy.ReportTarget{Component: o.Path, Environment: "ci", Context: o.Context, Version: o.Revision}, Findings: []securitypolicy.Finding{}}
}
func fingerprint(values ...string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(values, "|"))))
}
func decodeOne(r io.Reader, value any) error {
	data, err := io.ReadAll(io.LimitReader(r, maxReportBytes+1))
	if err != nil || len(data) > maxReportBytes {
		return fmt.Errorf("scanner JSON exceeds limit or cannot be read")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	if err := d.Decode(value); err != nil {
		return fmt.Errorf("invalid scanner JSON")
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return fmt.Errorf("unexpected trailing scanner data")
	}
	return nil
}

var npmPackageName = regexp.MustCompile(`^(@[a-z0-9][a-z0-9._-]*/)?[a-z0-9][a-z0-9._-]*$`)
var dependencyVersion = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+([-+][0-9A-Za-z.+-]+)?$`)
var safeName = regexp.MustCompile(`^[a-zA-Z0-9@][a-zA-Z0-9_./@+~:-]*$`)

func validName(s string) bool {
	return len(s) > 0 && len(s) < 300 && safeName.MatchString(s) && !strings.Contains(s, "://") && !strings.Contains(s, "..")
}
func newFinding(report securitypolicy.FindingReport, component, version, rule, kind string) securitypolicy.Finding {
	return securitypolicy.Finding{RuleID: rule, FindingType: kind, Severity: "unknown", Confidence: "high", Reachability: "not_applicable", Mandatory: true, ExceptionAllowed: true, Component: component, ComponentType: "dependency", Path: report.Target.Component, Environment: "ci", Context: report.Target.Context, Version: version, Redacted: true}
}
