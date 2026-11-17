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
	s, err := p.ReadMetrics(context.Background(), mappingTestResource(resource.Resource{ID: "test", Type: resource.TypeKubernetesDeployment, Name: "test", Metadata: map[string]any{"namespace": "test"}}))
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
	if start.Equal(end) {
		return []Series{{Metric: mappingTestLabelsFromQuery(query), Points: []Point{{Timestamp: end, Value: 1}}}}, nil
	}
	values := []float64{1, 2, 3}
	if strings.Contains(query, "kube_pod_container_info") {
		values = []float64{1, 1, 1}
	}
	if strings.Contains(query, "container_cpu_cfs") {
		values = []float64{.1, .2, .3}
	}
	points := []Point{}
	index := 0
	for at := start; !at.After(end); at = at.Add(step) {
		v := values[index]
		points = append(points, Point{Timestamp: at, Value: v})
		if index < 2 {
			index++
		}
	}
	return []Series{{Metric: mappingTestLabelsFromQuery(query), Points: points}}, nil
}

func TestReadMetricsQueriesPrometheusForDeploymentSignals(t *testing.T) {
	client := &fakeClient{}
	p := NewWithClient(client, Config{Window: time.Hour, Step: time.Minute})
	snapshot, err := p.ReadMetrics(context.Background(), mappingTestResource(resource.Resource{
		ID:       "k8s:prod:checkout-api",
		Type:     resource.TypeKubernetesDeployment,
		Name:     "checkout-api",
		Metadata: map[string]any{"namespace": "prod", "name": "checkout-api"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.PluginID != ID {
		t.Fatalf("plugin id = %q", snapshot.PluginID)
	}
	if got := snapshot.Signals["cpu_cores_p95"]; got != float64(3) {
		t.Fatalf("cpu p95 = %#v", got)
	}
	if len(client.queries) != 12 {
		t.Fatalf("queries = %d, want 12", len(client.queries))
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
	end := time.Now().UTC().Truncate(time.Millisecond)
	s, err := p.ReadMetricsBetween(context.Background(), mappingTestResource(resource.Resource{ID: "test", Type: resource.TypeKubernetesDeployment, Name: "test", Metadata: map[string]any{"namespace": "test"}}), end.Add(-2*time.Minute), end)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ValidateRecords(); err != nil {
		t.Fatal(err)
	}
	if len(s.Records) != 28 {
		t.Fatal("incomplete provenance", len(s.Records))
	}
	for _, r := range s.Records {
		if !strings.Contains(r.Query, `namespace="test"`) || (r.Aggregation != "last" && r.SampleCount != 3 || r.Aggregation == "last" && r.SampleCount != 1) || r.Kind != "aggregated" || r.Quality != "valid" || !r.WindowEnd.Equal(end) {
			t.Fatal("incorrect provenance", r.Signal)
		}
	}
	if _, err = s.Measurement("memory_working_set_bytes_p95", "bytes", s.CollectedAt); err != nil {
		t.Fatal(err)
	}
}

func TestCustomSignalRequiresUnitProvenance(t *testing.T) {
	res := mappingTestResource(resource.Resource{ID: "test", Type: resource.TypeKubernetesDeployment, Name: "test", Metadata: map[string]any{"namespace": "test"}})
	end := time.Now().UTC().Truncate(time.Millisecond)
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

func mappingTestResource(r resource.Resource) resource.Resource {
	r.Provider = resource.ProviderKubernetes
	r.Account = "fixture-cluster"
	if r.Metadata == nil {
		r.Metadata = map[string]any{}
	}
	r.Metadata["uid"] = "fixture-deployment-uid"
	r.Metadata["mapping"] = map[string]any{"version": "kubernetes-deployment-mapper-v1", "containers": []any{map[string]any{"name": "app", "kind": "container"}}}
	return r
}
func mappingTestLabels(namespace, name string) map[string]string {
	return map[string]string{"cluster": "fixture-cluster", "namespace": namespace, "pod": name + "-rs-pod", "uid": "fixture-pod-uid", "deployment": name, "deployment_uid": "fixture-deployment-uid", "container": "app"}
}
func mappingTestLabelsFromQuery(query string) map[string]string {
	ns, name := "test", "test"
	if strings.Contains(query, `namespace="prod"`) {
		ns, name = "prod", "checkout-api"
	}
	return mappingTestLabels(ns, name)
}
