package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/consize-oss/consize/pkg/resource"
	"regexp"
	"strings"

	"k8s.io/apimachinery/pkg/util/version"
)

const RuntimeSDKVersion = "1.0.0"
const RuntimeCoreVersion = "0.3.0-alpha"
const CapabilityRecommend = "recommendation.generate"

// RuntimeManifest describes a package, never a configured installation. The
// enclosing Manifest remains the signed wire contract for older executables.
type RuntimeManifest struct {
	SDKRange             string                 `json:"sdk_range"`
	CoreRange            string                 `json:"core_range"`
	Provider             string                 `json:"provider"`
	ConfigurationVersion string                 `json:"configuration_version"`
	Distribution         string                 `json:"distribution"`
	Integrity            string                 `json:"integrity"`
	Capabilities         []CapabilityDescriptor `json:"capabilities"`
}

type CapabilityDescriptor struct {
	Name          string   `json:"name"`
	Version       string   `json:"version"`
	ResourceTypes []string `json:"resource_types"`
	Providers     []string `json:"providers"`
	Operations    []string `json:"operations"`
	Modes         []string `json:"modes"`
}

var runtimeID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)
var integrityIdentity = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
var semanticVersion = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$`)

func parseVersion(raw string) (*version.Version, error) {
	if !semanticVersion.MatchString(raw) {
		return nil, errors.New("expected a semantic version")
	}
	return version.ParseSemantic(raw)
}

// MatchVersionRange accepts an intersection of explicit comparisons separated
// by spaces, e.g. ">=1.0.0 <2.0.0". Unsupported syntax is rejected, never guessed.
func MatchVersionRange(current, constraint string) (bool, error) {
	v, err := parseVersion(current)
	if err != nil {
		return false, err
	}
	terms := strings.Fields(constraint)
	if len(terms) == 0 || len(terms) > 8 {
		return false, errors.New("version range requires one to eight comparisons")
	}
	match := true
	for _, term := range terms {
		op := "="
		for _, candidate := range []string{">=", "<=", ">", "<", "="} {
			if strings.HasPrefix(term, candidate) {
				op = candidate
				term = strings.TrimPrefix(term, candidate)
				break
			}
		}
		if _, err := parseVersion(term); err != nil {
			return false, errors.New("invalid version range comparator")
		}
		cmp, err := v.Compare(term)
		if err != nil {
			return false, errors.New("invalid version range")
		}
		switch op {
		case "=":
			match = match && cmp == 0
		case ">=":
			match = match && cmp >= 0
		case "<=":
			match = match && cmp <= 0
		case ">":
			match = match && cmp > 0
		case "<":
			match = match && cmp < 0
		}
	}
	return match, nil
}

// ValidateRuntimeManifest is intentionally strict for newly configured instances.
// Legacy registrations retain their historical contract until explicitly migrated.
func ValidateRuntimeManifest(m Manifest) error {
	encoded, _ := json.Marshal(m)
	if len(encoded) > 64<<10 {
		return errors.New("manifest exceeds 64 KiB")
	}
	if !runtimeID.MatchString(m.ID) || strings.TrimSpace(m.DisplayName) == "" || len(m.DisplayName) > 160 {
		return errors.New("manifest requires a valid package id and display name")
	}
	switch m.Category {
	case CategoryAction, CategoryDataSource, CategoryMetrics, CategoryCost, CategoryAlgorithm:
	default:
		return errors.New("unsupported package category")
	}
	if len(m.SupportedResourceTypes) > 64 || len(m.SupportedActionTypes) > 64 {
		return errors.New("manifest selectors exceed limits")
	}
	if _, err := parseVersion(m.Version); err != nil {
		return errors.New("manifest version must be semantic")
	}
	r := m.Runtime
	if r == nil {
		return errors.New("manifest runtime declaration is required")
	}
	if !runtimeID.MatchString(r.Provider) || !runtimeID.MatchString(r.ConfigurationVersion) {
		return errors.New("manifest provider and configuration version are required")
	}
	switch r.Distribution {
	case "builtin":
		if r.Integrity != "" {
			return errors.New("builtin integrity is supplied by the core build")
		}
	case "external":
		if r.Integrity != "" && !integrityIdentity.MatchString(r.Integrity) {
			return errors.New("external package requires a sha256 integrity identity")
		}
	default:
		return errors.New("unsupported distribution")
	}
	for _, c := range []struct{ name, current, constraint string }{{"SDK", RuntimeSDKVersion, r.SDKRange}, {"core", RuntimeCoreVersion, r.CoreRange}} {
		ok, err := MatchVersionRange(c.current, c.constraint)
		if err != nil {
			return fmt.Errorf("invalid %s compatibility range", c.name)
		}
		if !ok {
			return &RuntimeError{Code: FailureIncompatible}
		}
	}
	if len(r.Capabilities) == 0 || len(r.Capabilities) > 32 {
		return errors.New("manifest requires bounded capability declarations")
	}
	seen := map[string]bool{}
	for _, c := range r.Capabilities {
		if seen[c.Name] {
			return errors.New("duplicate capability declaration")
		}
		seen[c.Name] = true
		switch c.Name {
		case CapabilityResourceDiscover, CapabilityMetricsRead, CapabilityActionPlan, CapabilityActionExecute, CapabilityActionPreflight, CapabilityCostRead, CapabilityRecommend, "notification.send":
		default:
			return errors.New("unsupported capability")
		}
		if parsed, err := parseVersion(c.Version); err != nil || parsed.Major() != 1 {
			return errors.New("capability version must be semantic")
		}
		if !Supports(m.Capabilities, c.Name) {
			return errors.New("capability descriptors disagree with manifest")
		}
		for _, values := range [][]string{c.ResourceTypes, c.Providers, c.Operations, c.Modes} {
			if len(values) > 64 {
				return errors.New("capability selector exceeds limit")
			}
			unique := map[string]bool{}
			for _, value := range values {
				if value == "" || len(value) > 160 || unique[value] {
					return errors.New("invalid capability selector")
				}
				unique[value] = true
			}
		}
	}
	if len(seen) != len(m.Capabilities) {
		return errors.New("every capability requires a versioned descriptor")
	}
	if seen[CapabilityActionExecute] && !seen[CapabilityActionPlan] {
		return errors.New("execution requires action.plan")
	}
	if m.CanMutateInfrastructure && !seen[CapabilityActionExecute] {
		return errors.New("mutating package requires action.execute")
	}
	return nil
}

// WithBuiltinRuntime declares the core-shipped SDK contract at the composition
// boundary. Package version is still supplied by each provider's manifest.
func WithBuiltinRuntime(m Manifest, provider string) Manifest {
	descriptors := make([]CapabilityDescriptor, 0, len(m.Capabilities))
	for _, capability := range m.Capabilities {
		descriptors = append(descriptors, CapabilityDescriptor{Name: capability, Version: "1.0.0", ResourceTypes: append([]string(nil), m.SupportedResourceTypes...), Providers: []string{provider}})
	}
	for i := range descriptors {
		if strings.HasPrefix(descriptors[i].Name, "action.") {
			descriptors[i].Operations = append([]string(nil), m.SupportedActionTypes...)
		}
	}
	m.Runtime = &RuntimeManifest{SDKRange: ">=1.0.0 <2.0.0", CoreRange: ">=0.3.0-alpha <1.0.0", Provider: provider, ConfigurationVersion: "1", Distribution: "builtin", Capabilities: descriptors}
	return m
}

func (p managedPlugin) acceptCapability(name string, res resource.Resource, operation, mode string) error {
	if err := p.accept(res); err != nil {
		return err
	}
	if p.e.manifest.Runtime == nil {
		return nil
	}
	for _, descriptor := range p.e.manifest.Runtime.Capabilities {
		if descriptor.Name == name && selected(descriptor.ResourceTypes, res.Type) && selected(descriptor.Providers, res.Provider) && selected(descriptor.Operations, operation) && selected(descriptor.Modes, mode) {
			return nil
		}
	}
	return &RuntimeError{Code: FailureUnsupported}
}
