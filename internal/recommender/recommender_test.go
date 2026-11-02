package recommender

import (
	"context"
	"testing"
	"time"

	"github.com/consize-oss/consize/internal/bootstrap"
	"github.com/consize-oss/consize/internal/testevidence"
	"github.com/consize-oss/consize/pkg/plugin"
	"github.com/consize-oss/consize/pkg/resource"
)

func TestRecommendCreatesKubernetesPatchRecommendation(t *testing.T) {
	recs, err := New(bootstrap.RecommenderConfig{
		HeadroomRatio:     1.5,
		MinReductionRatio: 0.10,
	}).Recommend(context.Background(), Input{
		Resource: resource.Resource{
			ID:          "k8s:prod:checkout-api",
			Type:        resource.TypeKubernetesDeployment,
			Name:        "checkout-api",
			Environment: resource.EnvProduction,
			Criticality: resource.CriticalityHigh,
			CurrentState: map[string]any{
				"memory_request_bytes": float64(8 * 1024 * 1024 * 1024),
				"memory_limit_bytes":   float64(16 * 1024 * 1024 * 1024),
			},
		},
		Evidence: []plugin.MetricsSnapshot{testevidence.Attach(plugin.MetricsSnapshot{
			ResourceID: "k8s:prod:checkout-api", PluginID: "prometheus-metrics",
			Source: "prometheus",
			Window: "24h",
			Signals: map[string]any{
				"memory_working_set_bytes_p95":    float64(4 * 1024 * 1024 * 1024),
				"memory_working_set_bytes_points": float64(288),
			},
			CollectedAt: time.Now(),
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("recommendations = %d, want 1", len(recs))
	}
	rec := recs[0]
	if rec.PluginID != "kubernetes-action" {
		t.Fatalf("plugin id = %q", rec.PluginID)
	}
	if rec.AlgorithmID != BuiltInHeadroomAlgorithmID {
		t.Fatalf("algorithm id = %q", rec.AlgorithmID)
	}
	if rec.Risk != "medium" || rec.Confidence != "low" {
		t.Fatalf("risk/confidence = %q/%q", rec.Risk, rec.Confidence)
	}
	patch := rec.Parameters["patch"].(map[string]any)
	if patch["proposed_request"].(int64) != int64(6*1024*1024*1024) {
		t.Fatalf("unexpected proposed request: %#v", patch["proposed_request"])
	}
	if _, ok := rec.Parameters["verification_plan"].(map[string]any); !ok {
		t.Fatal("expected verification plan")
	}
	if patch["proposed_limit"] != patch["current_limit"] {
		t.Fatal("memory limit must remain unchanged")
	}
}

func TestLargeReductionIsCapped(t *testing.T) {
	rec, err := New(bootstrap.RecommenderConfig{}).RecommendOne(context.Background(), resource.Resource{
		ID: "local", Type: resource.TypeKubernetesDeployment, Name: "test", CurrentState: map[string]any{"memory_request_bytes": int64(512 * 1024 * 1024), "memory_limit_bytes": int64(1024 * 1024 * 1024)},
	}, testevidence.Attach(plugin.MetricsSnapshot{PluginID: "prometheus-metrics", ResourceID: "local", Signals: map[string]any{"memory_working_set_bytes_p95": float64(7 * 1024 * 1024)}}))
	if err != nil {
		t.Fatal(err)
	}
	if rec.Proposed["request"] != int64(384*1024*1024) {
		t.Fatalf("uncapped request: %#v", rec.Proposed)
	}
	if rec.Proposed["limit"] != int64(1024*1024*1024) {
		t.Fatal("limit changed")
	}
}

func TestRecommendationFreezesInputsAndRejectsUnusableProvenance(t *testing.T) {
	res := resource.Resource{ID: "local", Type: resource.TypeKubernetesDeployment, Name: "test", CurrentState: map[string]any{"memory_request_bytes": int64(512 * 1024 * 1024)}}
	s := testevidence.Attach(plugin.MetricsSnapshot{ResourceID: res.ID, PluginID: "prometheus-metrics", Signals: map[string]any{"memory_working_set_bytes_p95": 100.}})
	engine := New(bootstrap.RecommenderConfig{})
	rec, err := engine.RecommendOne(context.Background(), res, s)
	if err != nil {
		t.Fatal(err)
	}
	*s.Records[0].Value = 200
	if *rec.EvidenceSnapshot[0].Value != 100 || rec.AlgorithmVersion == "" || rec.EvidenceRefs[0] != rec.EvidenceSnapshot[0].ID {
		t.Fatal("evidence not frozen")
	}
	if _, err = engine.RecommendOne(context.Background(), res, s); err == nil {
		t.Fatal("changed input accepted")
	}
	s = testevidence.Attach(plugin.MetricsSnapshot{ResourceID: res.ID, PluginID: "prometheus-metrics", CollectedAt: time.Now().Add(-time.Hour), Signals: map[string]any{"memory_working_set_bytes_p95": 100.}})
	if _, err = engine.RecommendOne(context.Background(), res, s); err == nil {
		t.Fatal("stale inputs accepted")
	}
	s.Records = nil
	if _, err = engine.RecommendOne(context.Background(), res, s); err == nil {
		t.Fatal("missing provenance accepted")
	}
}
