package dependencysecurity

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/consize-oss/consize/internal/securitypolicy"
)

type npmAudit struct {
	Version         int             `json:"auditReportVersion"`
	Error           json.RawMessage `json:"error"`
	Vulnerabilities map[string]struct {
		Name     string            `json:"name"`
		Severity string            `json:"severity"`
		Via      []json.RawMessage `json:"via"`
		Nodes    []string          `json:"nodes"`
		Fix      json.RawMessage   `json:"fixAvailable"`
	} `json:"vulnerabilities"`
	Metadata *struct {
		Vulnerabilities *struct {
			Total int `json:"total"`
		} `json:"vulnerabilities"`
	} `json:"metadata"`
}
type npmLock struct {
	Version  int `json:"lockfileVersion"`
	Packages map[string]struct {
		Version     string `json:"version"`
		Dev         bool   `json:"dev"`
		DevOptional bool   `json:"devOptional"`
		Optional    bool   `json:"optional"`
	} `json:"packages"`
}

func NormalizeNPM(input, lockInput io.Reader, o Options) (securitypolicy.FindingReport, error) {
	var audit npmAudit
	var lock npmLock
	report := baseReport("npm-audit", o)
	if err := decodeOne(input, &audit); err != nil {
		return report, err
	}
	if err := decodeOne(lockInput, &lock); err != nil {
		return report, fmt.Errorf("invalid dependency lockfile")
	}
	if audit.Version != 2 || len(audit.Error) > 0 && string(audit.Error) != "null" || audit.Vulnerabilities == nil || audit.Metadata == nil || audit.Metadata.Vulnerabilities == nil || audit.Metadata.Vulnerabilities.Total != len(audit.Vulnerabilities) {
		return report, fmt.Errorf("incomplete or unsupported npm audit report")
	}
	if lock.Version != 3 || len(lock.Packages) == 0 {
		return report, fmt.Errorf("expected a complete npm v3 lockfile")
	}
	names := make([]string, 0, len(audit.Vulnerabilities))
	for name := range audit.Vulnerabilities {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		vulnerability := audit.Vulnerabilities[name]
		if !npmPackageName.MatchString(name) || vulnerability.Name != name || len(vulnerability.Nodes) == 0 || len(vulnerability.Via) == 0 {
			return report, fmt.Errorf("npm finding lacks package, advisory or affected nodes")
		}
		severity := vulnerability.Severity
		if severity == "moderate" {
			severity = "medium"
		}
		switch severity {
		case "critical", "high", "medium", "low", "info":
		default:
			return report, fmt.Errorf("unknown npm severity")
		}
		ids, err := npmAdvisoryIDs(audit, name)
		if err != nil {
			return report, err
		}
		sort.Strings(ids)
		versions := map[string]map[string]bool{}
		for _, node := range vulnerability.Nodes {
			pkg, ok := lock.Packages[node]
			if !ok || !strings.HasSuffix(node, "node_modules/"+name) || !dependencyVersion.MatchString(pkg.Version) {
				return report, fmt.Errorf("npm affected node is not present in the scanned lockfile")
			}
			contexts := versions[pkg.Version]
			if contexts == nil {
				contexts = map[string]bool{}
				versions[pkg.Version] = contexts
			}
			if pkg.Dev {
				contexts["development"] = true
			} else if pkg.DevOptional {
				contexts["development"] = true
				contexts["production"] = true
			} else {
				contexts["production"] = true
			}
			if pkg.Optional {
				contexts["optional"] = true
			}
		}
		hint := "no automatic fix advertised; review advisory and parent dependency"
		if string(vulnerability.Fix) == "true" {
			hint = "a dependency update is available; review before changing the lockfile"
		}
		var fix struct {
			Name    string `json:"name"`
			Version string `json:"version"`
			Major   bool   `json:"isSemVerMajor"`
		}
		if len(vulnerability.Fix) > 0 && vulnerability.Fix[0] == '{' {
			if json.Unmarshal(vulnerability.Fix, &fix) != nil || !npmPackageName.MatchString(fix.Name) || !dependencyVersion.MatchString(fix.Version) {
				return report, fmt.Errorf("invalid npm remediation metadata")
			}
			hint = "review update to " + fix.Name + "@" + fix.Version
			if fix.Major {
				hint += " (major-version change)"
			}
		}
		installed := []string{}
		for version := range versions {
			installed = append(installed, version)
		}
		sort.Strings(installed)
		for _, version := range installed {
			contexts := []string{}
			for c := range versions[version] {
				contexts = append(contexts, c)
			}
			sort.Strings(contexts)
			f := newFinding(report, "npm:"+name, version, "npm-audit:"+name, "frontend_dependency")
			f.Severity = severity
			f.Owner = "frontend"
			f.Fingerprint = fingerprint(o.Path, name, version, strings.Join(ids, ","), strings.Join(contexts, ","), severity)
			visible := ids
			if len(visible) > 6 {
				visible = visible[:6]
			}
			f.Summary = fmt.Sprintf("%s@%s (%s); advisories %s (%d total); %s. npm audit does not prove runtime reachability.", name, version, strings.Join(contexts, ", "), strings.Join(visible, ", "), len(ids), hint)
			report.Findings = append(report.Findings, f)
		}
	}
	return report, nil
}

// Resolve metavulnerabilities to concrete advisory IDs. Exceptions must not
// silently cover a new advisory hidden behind the same parent dependency.
func npmAdvisoryIDs(audit npmAudit, name string) ([]string, error) {
	seen := map[string]bool{}
	ids := map[string]bool{}
	var visit func(string) error
	visit = func(name string) error {
		if seen[name] {
			return nil
		}
		seen[name] = true
		item, ok := audit.Vulnerabilities[name]
		if !ok || len(item.Via) == 0 {
			return fmt.Errorf("unresolved npm advisory reference")
		}
		for _, via := range item.Via {
			var inherited string
			if json.Unmarshal(via, &inherited) == nil {
				if !npmPackageName.MatchString(inherited) {
					return fmt.Errorf("invalid npm advisory reference")
				}
				if err := visit(inherited); err != nil {
					return err
				}
				continue
			}
			var advisory struct {
				Source int    `json:"source"`
				URL    string `json:"url"`
			}
			if json.Unmarshal(via, &advisory) != nil || advisory.Source <= 0 {
				return fmt.Errorf("invalid npm advisory identifier")
			}
			id := strconv.Itoa(advisory.Source)
			if u, err := url.Parse(advisory.URL); err == nil && u.Scheme == "https" && u.Host == "github.com" && u.User == nil {
				candidate := strings.TrimPrefix(u.Path, "/advisories/")
				if regexp.MustCompile(`^GHSA-[a-z0-9]{4}-[a-z0-9]{4}-[a-z0-9]{4}$`).MatchString(candidate) {
					id = candidate
				}
			}
			ids[id] = true
		}
		return nil
	}
	if err := visit(name); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("npm finding has no concrete advisory")
	}
	result := []string{}
	for id := range ids {
		result = append(result, id)
	}
	sort.Strings(result)
	return result, nil
}
