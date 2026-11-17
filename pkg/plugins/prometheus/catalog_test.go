package prometheus

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/consize-oss/consize/pkg/evidence"
	"github.com/consize-oss/consize/pkg/plugin"
	"github.com/consize-oss/consize/pkg/resource"
)

var collectionAt = time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)

type evidenceFixtureClient struct {
	mutate func(string, []Series) []Series
}

func (f evidenceFixtureClient) QueryRange(_ context.Context, query string, start, end time.Time, step time.Duration) ([]Series, error) {
	value := 1.
	switch {
	case strings.Contains(query, "kube_pod_container_info"):
		value = 1
	case strings.Contains(query, "fixture_millicores"):
		value = 500
	case strings.Contains(query, "fixture_mebibytes"):
		value = 2
	case strings.Contains(query, "container_cpu_cfs"):
		value = .1
	case strings.Contains(query, "cpu_usage"):
		value = .5
	case strings.Contains(query, "memory_working"):
		value = 2 * 1024 * 1024
	case strings.Contains(query, "resource_requests") && strings.Contains(query, `resource="cpu"`):
		value = .25
	case strings.Contains(query, "resource_limits") && strings.Contains(query, `resource="cpu"`):
		value = 1
	case strings.Contains(query, "resource_requests"):
		value = 128 * 1024 * 1024
	case strings.Contains(query, "resource_limits"):
		value = 256 * 1024 * 1024
	case strings.Contains(query, "restarts_total"):
		value = 2
	case strings.Contains(query, "oom_events") || strings.Contains(query, "status_reason"):
		value = 0
	}
	points := []Point{}
	for at := start; !at.After(end); at = at.Add(step) {
		points = append(points, Point{Timestamp: at, Value: value})
	}
	rows := []Series{{Metric: mappingTestLabels("test", "test"), Points: points}}
	if f.mutate != nil {
		rows = f.mutate(query, rows)
	}
	return rows, nil
}
func collectionResource() resource.Resource {
	return mappingTestResource(resource.Resource{ID: "fixture", Type: resource.TypeKubernetesDeployment, Name: "test", Metadata: map[string]any{"namespace": "test"}})
}
func collectFixture(t *testing.T, client Client, cfg Config) *Plugin {
	t.Helper()
	cfg.Step = time.Minute
	p := NewWithClient(client, cfg)
	p.now = func() time.Time { return collectionAt.Add(time.Second) }
	return p
}
func collectWindow(t *testing.T, p *Plugin) plugin.MetricsSnapshot {
	t.Helper()
	snapshot, err := p.ReadMetricsBetween(context.Background(), collectionResource(), collectionAt.Add(-2*time.Minute), collectionAt)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}
func findRecord(t *testing.T, s plugin.MetricsSnapshot, signal string) evidence.Record {
	t.Helper()
	for _, r := range s.Records {
		if r.Signal == signal {
			return r
		}
	}
	t.Fatal("missing record", signal)
	return evidence.Record{}
}

func TestQueryCatalogGolden(t *testing.T) {
	definitions, err := QueryCatalog(MappingConfig{}, "fixture-cluster", "payments")
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.MarshalIndent(definitions, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	expected, err := os.ReadFile("testdata/query-catalog-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var a, b any
	if json.Unmarshal(got, &a) != nil || json.Unmarshal(expected, &b) != nil || !reflect.DeepEqual(a, b) {
		t.Fatalf("query catalog changed:\n%s", got)
	}
	if len(definitions) != 10 {
		t.Fatal("catalog missing required evidence", len(definitions))
	}
}
func TestNormalizedUsageAndCurrentConfiguration(t *testing.T) {
	p := collectFixture(t, evidenceFixtureClient{}, Config{Queries: map[string]string{"cpu_cores": "fixture_millicores{{scope}}", "memory_working_set_bytes": "fixture_mebibytes{{scope}}"}, Units: map[string]string{"cpu_cores": "millicores", "memory_working_set_bytes": "MiB"}})
	snapshot := collectWindow(t, p)
	for _, tc := range []struct {
		signal, unit, source string
		value, scale         float64
	}{{"cpu_cores_p95", "cores", "millicores", .5, .001}, {"memory_working_set_bytes_p95", "bytes", "MiB", 2097152, 1048576}, {"cpu_request_cores_current", "cores", "cores", .25, 1}, {"memory_limit_bytes_current", "bytes", "bytes", 268435456, 1}} {
		r := findRecord(t, snapshot, tc.signal)
		if r.Value == nil || *r.Value != tc.value || r.Unit != tc.unit || r.OriginalUnit != tc.source || r.Scale != tc.scale {
			t.Fatal("normalization or configuration incorrect", r)
		}
		if r.Extensions["query_version"] == "" || len(r.Extensions["query_digest"]) != 64 || r.Extensions["aggregation_version"] != AggregationVersion || r.Extensions["evaluation_at"] != collectionAt.Format(time.RFC3339Nano) {
			t.Fatal("query provenance incomplete", r.Extensions)
		}
	}
	current := findRecord(t, snapshot, "cpu_request_cores_current")
	if current.Aggregation != "last" || current.SampleCount != 1 || current.Extensions["sample_kind"] != "current_configuration" || current.Extensions["minimum_samples"] != "1" {
		t.Fatal("configuration treated as usage history", current)
	}
	if _, exists := snapshot.Signals["cpu_request_cores_p95"]; exists {
		t.Fatal("configuration exposed as a usage percentile")
	}
	again := collectWindow(t, p)
	a, _ := json.Marshal(snapshot)
	b, _ := json.Marshal(again)
	if string(a) != string(b) {
		t.Fatal("identical data/window/clock changed provenance")
	}
}
func TestMissingLimitsAreUnavailableNotZero(t *testing.T) {
	p := collectFixture(t, evidenceFixtureClient{mutate: func(query string, rows []Series) []Series {
		if strings.Contains(query, "resource_limits") {
			return nil
		}
		return rows
	}}, Config{})
	snapshot := collectWindow(t, p)
	for _, key := range []string{"cpu_limit_cores_current", "memory_limit_bytes_current"} {
		r := findRecord(t, snapshot, key)
		if r.Quality != evidence.Unavailable || r.Value != nil {
			t.Fatal("missing limit fabricated", r)
		}
		if _, ok := snapshot.Signals[key]; ok {
			t.Fatal("missing limit projected as zero")
		}
	}
}
func TestPercentileAndMinimumSampleConfiguration(t *testing.T) {
	values := make([]float64, 100)
	for i := range values {
		values[i] = float64(i + 1)
	}
	if percentile(values, 7) != 7 || percentile(values, 95) != 95 {
		t.Fatal("nearest-rank rounding changed")
	}
	p := collectFixture(t, evidenceFixtureClient{}, Config{Percentile: 7, MinimumSamples: 4})
	snapshot := collectWindow(t, p)
	r := findRecord(t, snapshot, "cpu_cores_p7")
	if r.Quality != evidence.Partial || r.SampleCount != 3 || r.Extensions["configured_percentile"] != "7" {
		t.Fatal("minimum sample requirement ignored", r)
	}
	if findRecord(t, snapshot, "cpu_cores_p95").Value == nil {
		t.Fatal("required p95 removed by configured percentile")
	}
	for _, bad := range []Config{{Percentile: 100}, {Percentile: -1}, {MinimumSamples: 1}, {MinimumSamples: 100001}, {Units: map[string]string{"cpu_cores": "bytes"}}, {Units: map[string]string{"cpu_cores": "millicores"}}} {
		bad.BaseURL = "http://127.0.0.1:9090"
		if _, err := New(bad); err == nil {
			t.Fatal("invalid normalization/settings accepted", bad)
		}
	}
}
func TestNaNInfinityAndUnderflowNeverProduceZero(t *testing.T) {
	for _, bad := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), math.MaxFloat64} {
		p := collectFixture(t, evidenceFixtureClient{mutate: func(query string, rows []Series) []Series {
			if strings.Contains(query, "fixture_mebibytes") {
				for i := range rows[0].Points {
					rows[0].Points[i].Value = bad
				}
			}
			return rows
		}}, Config{Queries: map[string]string{"memory_working_set_bytes": "fixture_mebibytes{{scope}}"}, Units: map[string]string{"memory_working_set_bytes": "MiB"}})
		r := findRecord(t, collectWindow(t, p), "memory_working_set_bytes_p95")
		if r.Quality != evidence.Invalid || r.Value != nil {
			t.Fatal("invalid source became numeric", r)
		}
	}
	source := []Series{{Points: []Point{{Value: math.SmallestNonzeroFloat64}}}}
	if out := normalizeSeries(source, .001); !math.IsNaN(out[0].Points[0].Value) {
		t.Fatal("underflow became measured zero")
	}
}
func TestInvalidPodCannotDisappearFromWorkloadAggregate(t *testing.T) {
	c, _ := resolveMapping(MappingConfig{})
	out, discarded := reduceMapped("cpu_cores", []Series{mapSeries("pod-a", "app", math.NaN()), mapSeries("pod-b", "app", 0)}, c, 1)
	if len(out) != 0 || discarded != 1 {
		t.Fatal("invalid pod masked by another pod's zero", out, discarded)
	}
	out, discarded = reduceMapped("memory_working_set_bytes", []Series{mapSeries("pod-a", "app", math.MaxFloat64), mapSeries("pod-a", "sidecar", math.MaxFloat64)}, c, 2)
	if len(out) != 0 || discarded != 1 {
		t.Fatal("aggregate overflow not explicit")
	}
}
func TestCollectionCompletionControlsFreshness(t *testing.T) {
	p := collectFixture(t, evidenceFixtureClient{}, Config{})
	calls := 0
	p.now = func() time.Time {
		calls++
		if calls < 3 {
			return collectionAt.Add(time.Second)
		}
		return collectionAt.Add(4 * time.Minute)
	}
	snapshot := collectWindow(t, p)
	r := findRecord(t, snapshot, "cpu_cores_p95")
	if r.Quality != evidence.Stale || !r.CollectedAt.Equal(collectionAt.Add(4*time.Minute)) || !snapshot.CollectedAt.Equal(r.CollectedAt) {
		t.Fatal("slow collection concealed stale source", r)
	}
}

func TestReplicaAndSidecarAggregationPrecedesPercentiles(t *testing.T) {
	c, _ := resolveMapping(MappingConfig{})
	a := mapSeries("pod-a", "app", 1)
	a.Points = []Point{{Timestamp: collectionAt, Value: 1}, {Timestamp: collectionAt.Add(time.Minute), Value: 5}}
	sidecar := mapSeries("pod-a", "sidecar", 9)
	sidecar.Points = []Point{{Timestamp: collectionAt, Value: 9}, {Timestamp: collectionAt.Add(time.Minute), Value: 1}}
	b := mapSeries("pod-b", "app", 2)
	b.Points[0].Timestamp = collectionAt.Add(time.Minute)
	bs := mapSeries("pod-b", "sidecar", 2)
	bs.Points[0].Timestamp = collectionAt.Add(time.Minute)
	rows := []Series{a, sidecar, b, bs}
	reduced := aggregateMapped("cpu_cores", rows, c, 2)
	if len(reduced) != 1 || len(reduced[0].Points) != 2 || reduced[0].Points[0].Value != 10 || reduced[0].Points[1].Value != 6 {
		t.Fatal("replica/container reduction changed", reduced)
	}
	if percentile([]float64{10, 6}, 95) != 10 {
		t.Fatal("percentiles were summed per container")
	}
	for i := 0; i < 50; i++ {
		rows = append(rows[1:], rows[0])
		actual := aggregateMapped("cpu_cores", rows, c, 2)
		if !reflect.DeepEqual(actual, reduced) {
			t.Fatal("input ordering changed the result")
		}
	}
}
func TestCompensatedDeterministicSumAndCustomSeries(t *testing.T) {
	c, _ := resolveMapping(MappingConfig{})
	input := []Series{mapSeries("pod", "a", 1e16), mapSeries("pod", "b", 1), mapSeries("pod", "c", 1)}
	expected := aggregateMapped("memory_working_set_bytes", input, c, 3)
	if expected[0].Points[0].Value != 10000000000000002 {
		t.Fatal("small contributors lost", expected)
	}
	input[0], input[2] = input[2], input[0]
	if !reflect.DeepEqual(aggregateMapped("memory_working_set_bytes", input, c, 3), expected) {
		t.Fatal("ordering changed floating-point reduction")
	}
}

func TestConfiguredWindowRetainsExactHistoryAtFractionalClock(t *testing.T) {
	p := NewWithClient(evidenceFixtureClient{}, Config{Window: 2 * time.Minute, Step: time.Minute})
	p.now = func() time.Time { return collectionAt.Add(123456789 * time.Nanosecond) }
	snapshot, err := p.ReadMetrics(context.Background(), collectionResource())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Window != "2m0s" || snapshot.Coverage["cpu_cores"].ExpectedPoints != 3 || snapshot.Coverage["cpu_cores"].ActualPoints != 3 {
		t.Fatal("configured history lost to rounding", snapshot.Window, snapshot.Coverage["cpu_cores"])
	}
	for _, bad := range []Config{{Step: 1500 * time.Microsecond}, {Window: time.Hour + time.Nanosecond}} {
		bad.BaseURL = "http://127.0.0.1:9090"
		if _, err = New(bad); err == nil {
			t.Fatal("sub-millisecond configuration silently rounded")
		}
	}
}
