package recommender

import (
	"testing"
	"time"

	"github.com/consize-oss/consize/internal/bootstrap"
	"github.com/consize-oss/consize/pkg/plugin"
	"github.com/consize-oss/consize/pkg/resource"
)

func history(now time.Time, window, step time.Duration) plugin.MetricsSnapshot {
	count := int(window/step) + 1
	return plugin.MetricsSnapshot{ResourceID: "test", PluginID: "prometheus-metrics", Window: window.String(), CollectedAt: now, Signals: map[string]any{"memory_working_set_bytes_p95": 100., "memory_working_set_bytes_max": 120.}, Coverage: map[string]plugin.MetricCoverage{"memory_working_set_bytes": {FirstSample: now.Add(-window), LastSample: now, Step: step, MaxGap: step, ActualPoints: count, ExpectedPoints: count}}}
}
func TestConfidenceRequiresActualHistory(t *testing.T) {
	now := time.Now()
	res := resource.Resource{ID: "test", Environment: resource.EnvProduction}
	for _, tc := range []struct {
		window time.Duration
		label  string
	}{{time.Hour, "low"}, {24 * time.Hour, "low"}, {7 * 24 * time.Hour, "medium"}, {14 * 24 * time.Hour, "high"}} {
		got := AssessConfidence(bootstrap.RecommenderConfig{MetricsPluginID: "prometheus-metrics", ActionPluginID: "kubernetes-action"}, res, history(now, tc.window, time.Minute), now)
		if got.Label != tc.label {
			t.Fatalf("%s: %+v", tc.window, got)
		}
	}
}
func TestConfidenceLabRestrictions(t *testing.T) {
	now := time.Now()
	cfg := bootstrap.RecommenderConfig{MetricsPluginID: "prometheus-metrics", ActionPluginID: "kubernetes-action", ConfidenceProfile: "lab"}
	for _, tc := range []struct{ environment, criticality, want string }{{resource.EnvDevelopment, resource.CriticalityLow, "medium"}, {resource.EnvProduction, resource.CriticalityLow, "low"}, {resource.EnvStaging, resource.CriticalityLow, "low"}, {resource.EnvDevelopment, resource.CriticalityHigh, "low"}} {
		res := resource.Resource{ID: "test", Environment: tc.environment, Criticality: tc.criticality}
		if got := AssessConfidence(cfg, res, history(now, 30*time.Minute, 30*time.Second), now); got.Label != tc.want {
			t.Fatalf("%+v: %+v", tc, got)
		}
	}
	if got := AssessConfidence(cfg, resource.Resource{ID: "test", Environment: resource.EnvDevelopment}, history(now, 14*24*time.Hour, time.Minute), now); got.Label != "medium" {
		t.Fatal("lab cannot receive high confidence")
	}
}
func TestConfidenceRejectsBadEvidence(t *testing.T) {
	now := time.Now()
	res := resource.Resource{ID: "test", Environment: resource.EnvProduction}
	for _, tc := range []struct {
		name   string
		mutate func(*plugin.MetricsSnapshot)
	}{
		{"stale", func(s *plugin.MetricsSnapshot) { s.CollectedAt = now.Add(-time.Hour) }},
		{"wrong resource", func(s *plugin.MetricsSnapshot) { s.ResourceID = "other" }},
		{"wrong plugin", func(s *plugin.MetricsSnapshot) { s.PluginID = "other" }},
		{"missing coverage", func(s *plugin.MetricsSnapshot) { s.Coverage = nil }},
		{"sparse", func(s *plugin.MetricsSnapshot) {
			c := s.Coverage["memory_working_set_bytes"]
			c.ActualPoints = 100
			s.Coverage["memory_working_set_bytes"] = c
		}},
		{"claimed long history", func(s *plugin.MetricsSnapshot) {
			c := s.Coverage["memory_working_set_bytes"]
			c.FirstSample = now.Add(-time.Hour)
			s.Coverage["memory_working_set_bytes"] = c
		}},
		{"large gap", func(s *plugin.MetricsSnapshot) {
			c := s.Coverage["memory_working_set_bytes"]
			c.MaxGap = time.Hour
			s.Coverage["memory_working_set_bytes"] = c
		}},
		{"unknown gap", func(s *plugin.MetricsSnapshot) {
			c := s.Coverage["memory_working_set_bytes"]
			c.MaxGap = 0
			s.Coverage["memory_working_set_bytes"] = c
		}},
		{"burst", func(s *plugin.MetricsSnapshot) { s.Signals["memory_working_set_bytes_max"] = 300. }},
		{"missing peak", func(s *plugin.MetricsSnapshot) { delete(s.Signals, "memory_working_set_bytes_max") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := history(now, 14*24*time.Hour, time.Minute)
			tc.mutate(&s)
			if got := AssessConfidence(bootstrap.RecommenderConfig{MetricsPluginID: "prometheus-metrics", ActionPluginID: "kubernetes-action"}, res, s, now); got.Label != "low" || len(got.Reasons) == 0 {
				t.Fatalf("bad evidence: %+v", got)
			}
		})
	}
}
