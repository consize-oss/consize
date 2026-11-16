package policy

import (
	"context"
	"github.com/consize-oss/consize/pkg/plugin"
	"github.com/consize-oss/consize/pkg/resource"
	"testing"
)

func TestUnconfirmedMetadataNeverEnablesAutoApply(t *testing.T) {
	e := &Engine{AutoApplyNonProduction: true}
	manifest := plugin.Manifest{ID: "fixture", SupportedResourceTypes: []string{resource.TypeKubernetesDeployment}}
	for _, tc := range []struct {
		owner, environment string
		auto               bool
	}{
		{"platform", resource.EnvDevelopment, true}, {"platform", resource.EnvStaging, true}, {"platform", resource.EnvUnknown, false}, {"platform", "", false}, {"platform", "unexpected", false}, {"platform", resource.EnvProduction, false}, {" unassigned ", resource.EnvDevelopment, false}, {"UNASSIGNED", resource.EnvStaging, false}, {" ", resource.EnvDevelopment, false},
	} {
		r := resource.Resource{ID: "fixture", Type: resource.TypeKubernetesDeployment, Owner: tc.owner, Environment: tc.environment}
		got := e.Evaluate(context.Background(), r, manifest, "auto", "")
		if (got.Decision == DecisionAutoApply) != tc.auto {
			t.Fatalf("owner=%q env=%q decision=%s", tc.owner, tc.environment, got.Decision)
		}
	}
}
