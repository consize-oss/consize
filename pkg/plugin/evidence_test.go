package plugin_test

import (
	"github.com/consize-oss/consize/pkg/evidence"
	"github.com/consize-oss/consize/pkg/plugin"
	"testing"
	"time"
)

func TestMetricProvenanceRefusesMissingStaleAndMismatchedValues(t *testing.T) {
	now := time.Now().UTC()
	r, err := evidence.Seal(evidence.Record{ResourceID: "one", PluginID: "metrics", Signal: "memory_p95", Query: "synthetic query", Kind: evidence.Aggregated, Aggregation: "p95", Unit: "bytes", OriginalUnit: "bytes", Scale: 1, Value: evidence.Number(0), SampleCount: 10, WindowStart: now.Add(-time.Hour), WindowEnd: now, CollectedAt: now, FreshUntil: now.Add(time.Minute), Quality: evidence.Valid})
	if err != nil {
		t.Fatal(err)
	}
	s := plugin.MetricsSnapshot{ResourceID: "one", PluginID: "metrics", CollectedAt: now, Window: "1h", Signals: map[string]any{"memory_p95": 0.}, Records: []evidence.Record{r}}
	if _, err := s.Measurement("memory_p95", "bytes", now); err != nil {
		t.Fatal("measured zero rejected", err)
	}
	for _, mutate := range []func(*plugin.MetricsSnapshot){func(s *plugin.MetricsSnapshot) { s.Records = nil }, func(s *plugin.MetricsSnapshot) { s.ResourceID = "other" }, func(s *plugin.MetricsSnapshot) { s.PluginID = "other" }, func(s *plugin.MetricsSnapshot) { s.Records = append(s.Records, s.Records[0]) }, func(s *plugin.MetricsSnapshot) { s.Signals = map[string]any{"memory_p95": 1.} }} {
		bad := s
		mutate(&bad)
		if _, err := bad.Measurement("memory_p95", "bytes", now); err == nil {
			t.Fatal("invalid provenance accepted")
		}
	}
	if _, err := s.Measurement("memory_p95", "cores", now); err == nil {
		t.Fatal("unit mismatch accepted")
	}
	if _, err := s.Measurement("memory_p95", "bytes", now.Add(2*time.Minute)); err == nil {
		t.Fatal("stale evidence accepted")
	}
}
