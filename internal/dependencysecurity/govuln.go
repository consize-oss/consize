package dependencysecurity

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/consize-oss/consize/internal/securitypolicy"
)

type goConfig struct {
	Protocol  string `json:"protocol_version"`
	Name      string `json:"scanner_name"`
	Version   string `json:"scanner_version"`
	Level     string `json:"scan_level"`
	Mode      string `json:"scan_mode"`
	GoVersion string `json:"go_version"`
}
type goFinding struct {
	OSV   string `json:"osv"`
	Fixed string `json:"fixed_version"`
	Trace []struct {
		Module   string `json:"module"`
		Version  string `json:"version"`
		Package  string `json:"package"`
		Function string `json:"function"`
	} `json:"trace"`
}

func NormalizeGo(input io.Reader, o Options) (securitypolicy.FindingReport, error) {
	report := baseReport("govulncheck", o)
	data, err := io.ReadAll(io.LimitReader(input, maxReportBytes+1))
	if err != nil || len(data) > maxReportBytes {
		return report, fmt.Errorf("govulncheck report exceeds limit or cannot be read")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	var config goConfig
	configured, sbom := false, false
	advisories := map[string]bool{}
	findings := []goFinding{}
	messages := 0
	for {
		var message map[string]json.RawMessage
		err := d.Decode(&message)
		if err == io.EOF {
			break
		}
		if err != nil {
			return report, fmt.Errorf("invalid govulncheck stream")
		}
		if len(message) != 1 {
			return report, fmt.Errorf("unexpected govulncheck message")
		}
		messages++
		for key, value := range message {
			switch key {
			case "config":
				if messages != 1 || configured || json.Unmarshal(value, &config) != nil {
					return report, fmt.Errorf("invalid govulncheck configuration")
				}
				configured = true
			case "SBOM":
				var inventory struct {
					Roots   []string          `json:"roots"`
					Modules []json.RawMessage `json:"modules"`
				}
				if sbom || json.Unmarshal(value, &inventory) != nil || len(inventory.Roots) == 0 || len(inventory.Modules) == 0 {
					return report, fmt.Errorf("missing analyzed package graph")
				}
				sbom = true
			case "osv":
				var entry struct {
					ID string `json:"id"`
				}
				if json.Unmarshal(value, &entry) != nil || !validName(entry.ID) {
					return report, fmt.Errorf("invalid advisory record")
				}
				advisories[entry.ID] = true
			case "finding":
				var f goFinding
				if json.Unmarshal(value, &f) != nil {
					return report, fmt.Errorf("invalid Go finding")
				}
				findings = append(findings, f)
			case "progress": // Informational messages contain no security conclusions.
			default:
				return report, fmt.Errorf("unsupported govulncheck message")
			}
		}
	}
	if !configured || !sbom || config.Protocol != "v1.0.0" || config.Name != "govulncheck" || config.Version != GovulncheckVersion || config.Level != "symbol" || config.Mode != "source" {
		return report, fmt.Errorf("expected a complete pinned source/symbol analysis")
	}
	merged := map[string]securitypolicy.Finding{}
	ranks := map[string]int{}
	for _, item := range findings {
		if !advisories[item.OSV] || len(item.Trace) == 0 {
			return report, fmt.Errorf("Go finding lacks advisory or trace")
		}
		first := item.Trace[0]
		version := first.Version
		if first.Module == "stdlib" && version == "" {
			version = strings.TrimPrefix(config.GoVersion, "go")
		}
		if !validName(first.Module) || !dependencyVersion.MatchString(version) || !validName(item.OSV) {
			return report, fmt.Errorf("invalid Go module identity")
		}
		rank := 0
		layer := "module present; no vulnerable symbol reported reachable in this analysis"
		if first.Package != "" {
			rank = 1
			layer = "package imported; no vulnerable symbol reported reachable in this analysis"
		}
		if first.Function != "" {
			if first.Package == "" {
				return report, fmt.Errorf("symbol finding lacks package")
			}
			rank = 2
			layer = "vulnerable symbol reported reachable by source analysis (not proof of exploitability)"
		}
		key := item.OSV + "|" + first.Module + "|" + version
		if previous, ok := ranks[key]; ok && previous >= rank {
			continue
		}
		f := newFinding(report, "go:"+first.Module, version, item.OSV, "go_vulnerability")
		f.Path = "go.mod"
		f.Owner = "backend"
		f.Reachability = "not_reachable"
		if rank == 2 {
			f.Reachability = "reachable"
		}
		f.Fingerprint = fingerprint(item.OSV, first.Module, version)
		remediation := "no fixed version advertised; review advisory"
		if item.Fixed != "" {
			if !dependencyVersion.MatchString(item.Fixed) {
				return report, fmt.Errorf("invalid fixed Go version")
			}
			remediation = "fixed version " + item.Fixed + "; review an upgrade"
		}
		f.Summary = fmt.Sprintf("%s in %s@%s: %s; %s.", item.OSV, first.Module, version, layer, remediation)
		merged[key] = f
		ranks[key] = rank
	}
	keys := []string{}
	for k := range merged {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		report.Findings = append(report.Findings, merged[k])
	}
	return report, nil
}
