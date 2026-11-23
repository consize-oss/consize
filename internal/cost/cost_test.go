package cost

import (
	"context"
	"testing"

	"github.com/consize-oss/consize/internal/store"
	"github.com/consize-oss/consize/pkg/plugin"
	"github.com/consize-oss/consize/pkg/plugins/pricing"
	"github.com/consize-oss/consize/pkg/resource"
)

func TestEnrichKeepsEstimateClassification(t *testing.T) {
	manager := plugin.NewManager()
	p, _ := pricing.New(pricing.Config{Currency: "USD", Source: "fixture", MemoryGiBMonthly: 10})
	if err := manager.RegisterCost(p); err != nil {
		t.Fatal(err)
	}
	res := resource.Resource{ID: "resource", Type: resource.TypeKubernetesDeployment, Provider: resource.ProviderKubernetes, CurrentState: map[string]any{"memory_request_bytes": int64(1024 * 1024 * 1024)}}
	rec, err := Enrich(context.Background(), manager, p.ID(), res, store.Recommendation{Proposed: map[string]any{"resource": "memory", "request": int64(512 * 1024 * 1024)}})
	if err != nil {
		t.Fatal(err)
	}
	if rec.CostEstimate == nil || rec.CostEstimate.Classification != "estimate" || rec.EstimatedSavingsMonthly != 5 {
		t.Fatalf("unexpected recommendation: %+v", rec)
	}
}
