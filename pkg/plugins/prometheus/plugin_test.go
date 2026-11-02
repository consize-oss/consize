package prometheus

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/consize-oss/consize/pkg/resource"
)

type fakeClient struct {
	queries []string
}

type unusableClient struct{}

func (unusableClient) QueryRange(_ context.Context, _ string, start, end time.Time, _ time.Duration) ([]Series, error) {
	return []Series{{Points: []Point{{Timestamp: start.Add(-time.Minute), Value: 100}, {Timestamp: end.Add(time.Minute), Value: 100}, {Timestamp: end, Value: math.NaN()}}}}, nil
}
func TestUnavailableEvidenceHasNoNumericSummary(t *testing.T) {
	p := NewWithClient(unusableClient{}, Config{Window: time.Hour, Step: time.Minute})
	s, err := p.ReadMetrics(context.Background(), resource.Resource{ID: "test", Type: resource.TypeKubernetesDeployment, Name: "test", Metadata: map[string]any{"namespace": "test"}})
	if err != nil {
		t.Fatal(err)
	}
	for key := range defaultQueries() {
		if _, ok := s.Signals[key+"_max"]; ok {
			t.Fatalf("missing %s presented as measured zero", key)
		}
		if s.Coverage[key].ActualPoints != 0 || s.Signals[key+"_points"] != 0 {
			t.Fatal("invalid/out-of-window evidence counted")
		}
	}
}

func (f *fakeClient) QueryRange(_ context.Context, query string, start, end time.Time, step time.Duration) ([]Series, error) {
	f.queries = append(f.queries, query)
	return []Series{{
		Metric: map[string]string{},
		Points: []Point{
			{Timestamp: start, Value: 1},
			{Timestamp: start.Add(step), Value: 2},
			{Timestamp: end, Value: 3},
		},
	}}, nil
}

func TestReadMetricsQueriesPrometheusForDeploymentSignals(t *testing.T) {
	client := &fakeClient{}
	p := NewWithClient(client, Config{Window: time.Hour, Step: time.Minute})
	snapshot, err := p.ReadMetrics(context.Background(), resource.Resource{
		ID:       "k8s:prod:checkout-api",
		Type:     resource.TypeKubernetesDeployment,
		Name:     "checkout-api",
		Metadata: map[string]any{"namespace": "prod", "name": "checkout-api"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.PluginID != ID {
		t.Fatalf("plugin id = %q", snapshot.PluginID)
	}
	if got := snapshot.Signals["cpu_cores_p95"]; got != float64(3) {
		t.Fatalf("cpu p95 = %#v", got)
	}
	if len(client.queries) != 6 {
		t.Fatalf("queries = %d, want 6", len(client.queries))
	}
	for _, key := range []string{"cpu_throttling_ratio_p95", "oom_events_30m_max", "evicted_pods_max", "cpu_cores_p99"} {
		if _, ok := snapshot.Signals[key]; !ok {
			t.Fatalf("missing health signal %s", key)
		}
	}
	for _, query := range client.queries {
		if !strings.Contains(query, `namespace="prod"`) {
			t.Fatalf("query missing namespace matcher: %s", query)
		}
	}
}

func TestTypedQueryProvenance(t *testing.T) {
	client := &fakeClient{}
	p := NewWithClient(client, Config{Step: time.Minute})
	end := time.Now().UTC()
	s, err := p.ReadMetricsBetween(context.Background(), resource.Resource{ID: "test", Type: resource.TypeKubernetesDeployment, Name: "test", Metadata: map[string]any{"namespace": "test"}}, end.Add(-2*time.Minute), end)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ValidateRecords(); err != nil {
		t.Fatal(err)
	}
	if len(s.Records) != 24 {
		t.Fatal("incomplete provenance", len(s.Records))
	}
	for _, r := range s.Records {
		if !strings.Contains(r.Query, `namespace="test"`) || r.SampleCount != 3 || r.Kind != "aggregated" || r.Quality != "valid" || !r.WindowEnd.Equal(end) {
			t.Fatal("incorrect provenance", r.Signal)
		}
	}
	if _, err = s.Measurement("memory_working_set_bytes_p95", "bytes", s.CollectedAt); err != nil {
		t.Fatal(err)
	}
}

func TestCustomSignalRequiresUnitProvenance(t *testing.T) {
	res := resource.Resource{ID: "test", Type: resource.TypeKubernetesDeployment, Name: "test", Metadata: map[string]any{"namespace": "test"}}
	end := time.Now().UTC()
	cfg := Config{Step: time.Minute, Queries: map[string]string{"latency_seconds": "synthetic custom query"}}
	p := NewWithClient(&fakeClient{}, cfg)
	if _, err := p.ReadMetricsBetween(context.Background(), res, end.Add(-2*time.Minute), end); err == nil {
		t.Fatal("custom unit silently guessed")
	}
	cfg.Units = map[string]string{"latency_seconds": "seconds"}
	p = NewWithClient(&fakeClient{}, cfg)
	s, err := p.ReadMetricsBetween(context.Background(), res, end.Add(-2*time.Minute), end)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Measurement("latency_seconds_p95", "seconds", s.CollectedAt); err != nil {
		t.Fatal(err)
	}
}
