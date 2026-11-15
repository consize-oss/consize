package policy

import (
	"github.com/consize-oss/consize/pkg/resource"
	"testing"
)

func TestFoundationCoverageIsNotExecutionAuthorization(t *testing.T) {
	e := NewEngine()
	res := resource.Resource{ID: "test", Type: resource.TypeKubernetesDeployment, Environment: resource.EnvProduction}
	coverage := e.Coverage(res)
	if coverage.Status != "covered" || len(coverage.PolicyIDs) != 1 || coverage.PolicyIDs[0] != "oss-foundation-v1" {
		t.Fatal(coverage)
	}
	var unavailable *Engine
	if unavailable.Coverage(res).Status != "unknown" || e.Coverage(resource.Resource{}).Status != "unknown" {
		t.Fatal("unavailable coverage reported as a confirmed gap")
	}
}
