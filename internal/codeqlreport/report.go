// Package codeqlreport converts CodeQL SARIF into redacted policy input.
package codeqlreport

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/consize-oss/consize/internal/securitypolicy"
)

type rule struct {
	ID         string         `json:"id"`
	Properties map[string]any `json:"properties"`
}
type document struct {
	Version string `json:"version"`
	Runs    []struct {
		Tool struct {
			Extensions []struct {
				Rules []rule `json:"rules"`
			} `json:"extensions"`
			Driver struct {
				Name  string `json:"name"`
				Rules []rule `json:"rules"`
			} `json:"driver"`
		} `json:"tool"`
		Invocations []struct {
			Successful bool `json:"executionSuccessful"`
		} `json:"invocations"`
		Results []struct {
			RuleID    string `json:"ruleId"`
			RuleIndex *int   `json:"ruleIndex"`
			Rule      struct {
				ID            string `json:"id"`
				Index         *int   `json:"index"`
				ToolComponent struct {
					Index *int `json:"index"`
				} `json:"toolComponent"`
			} `json:"rule"`
			Locations []struct {
				Physical struct {
					Artifact struct {
						URI string `json:"uri"`
					} `json:"artifactLocation"`
					Region struct {
						Line int `json:"startLine"`
					} `json:"region"`
				} `json:"physicalLocation"`
			} `json:"locations"`
		} `json:"results"`
	} `json:"runs"`
}

func Normalize(data []byte, target securitypolicy.ReportTarget, at time.Time) (securitypolicy.FindingReport, error) {
	var doc document
	if err := json.Unmarshal(data, &doc); err != nil {
		return securitypolicy.FindingReport{}, fmt.Errorf("invalid SARIF: %w", err)
	}
	if doc.Version != "2.1.0" || len(doc.Runs) == 0 {
		return securitypolicy.FindingReport{}, fmt.Errorf("missing SARIF 2.1.0 runs")
	}
	report := securitypolicy.FindingReport{Schema: "./findings.schema.json", SchemaVersion: 1, Scanner: "codeql", ScanID: "codeql-" + target.Version, GeneratedAt: at.UTC().Format(time.RFC3339), Status: "completed", Attempts: 1, Target: target, Findings: []securitypolicy.Finding{}}
	seen := map[string]securitypolicy.Finding{}
	for _, run := range doc.Runs {
		if run.Tool.Driver.Name != "CodeQL" {
			return report, fmt.Errorf("unexpected SARIF producer")
		}
		for _, invocation := range run.Invocations {
			if !invocation.Successful {
				return report, fmt.Errorf("CodeQL invocation incomplete")
			}
		}
		rules := map[string]rule{}
		for _, r := range run.Tool.Driver.Rules {
			rules[r.ID] = r
		}
		for _, extension := range run.Tool.Extensions {
			for _, r := range extension.Rules {
				rules[r.ID] = r
			}
		}
		for _, result := range run.Results {
			id := result.RuleID
			if id == "" {
				id = result.Rule.ID
			}
			if id == "" && result.Rule.Index != nil {
				candidates := run.Tool.Driver.Rules
				if component := result.Rule.ToolComponent.Index; component != nil {
					if *component < 0 || *component >= len(run.Tool.Extensions) {
						return report, fmt.Errorf("unknown rule component")
					}
					candidates = run.Tool.Extensions[*component].Rules
				}
				if index := *result.Rule.Index; index >= 0 && index < len(candidates) {
					id = candidates[index].ID
				}
			}
			if id == "" && result.RuleIndex != nil && *result.RuleIndex >= 0 && *result.RuleIndex < len(run.Tool.Driver.Rules) {
				id = run.Tool.Driver.Rules[*result.RuleIndex].ID
			}
			r, ok := rules[id]
			if !ok || id == "" {
				return report, fmt.Errorf("result references unknown rule")
			}
			severity := "unknown"
			if raw, ok := r.Properties["security-severity"]; ok {
				score, err := strconv.ParseFloat(fmt.Sprint(raw), 64)
				if err != nil || score < 0 || score > 10 {
					return report, fmt.Errorf("invalid security severity")
				}
				switch {
				case score >= 9:
					severity = "critical"
				case score >= 7:
					severity = "high"
				case score >= 4:
					severity = "medium"
				default:
					severity = "low"
				}
			}
			confidence := "unknown"
			switch r.Properties["precision"] {
			case "very-high", "high":
				confidence = "high"
			case "medium":
				confidence = "medium"
			case "low":
				confidence = "low"
			}
			if len(result.Locations) == 0 {
				return report, fmt.Errorf("result has no source location")
			}
			location := result.Locations[0].Physical
			path := location.Artifact.URI
			if path == "" || strings.HasPrefix(path, "/") || strings.Contains(path, ":") || strings.Contains(path, "..") {
				return report, fmt.Errorf("invalid source location")
			}
			fp := fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%s:%s:%d", id, path, location.Region.Line))))
			finding := securitypolicy.Finding{Fingerprint: fp, RuleID: id, FindingType: "sast", Severity: severity, Confidence: confidence, Reachability: "unknown", Mandatory: true, ExceptionAllowed: true, Component: target.Component, ComponentType: "source", Path: path, Environment: target.Environment, Context: target.Context, Version: target.Version, Summary: fmt.Sprintf("CodeQL %s at source line %d", id, location.Region.Line), Redacted: true}
			if previous, ok := seen[fp]; ok {
				if previous != finding {
					return report, fmt.Errorf("conflicting metadata for duplicate finding")
				}
				continue
			}
			seen[fp] = finding
			report.Findings = append(report.Findings, finding)
		}
	}
	return report, nil
}
