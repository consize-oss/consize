package prometheus

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// Execute actual PromQL, not a string-matching mock. Both tools come from the
// existing checksum-pinned distribution; no real scrape target or user cluster.
func TestMappingPromQLSemantics(t *testing.T) {
	binary := os.Getenv("CONSIZE_PROMETHEUS_BIN")
	if binary == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("CI requires pinned Prometheus/promtool")
		}
		t.Skip("set CONSIZE_PROMETHEUS_BIN from the pinned CI installer")
	}
	c, _ := resolveMapping(MappingConfig{})
	query := scopedMappingQueries(c, `cluster="fixture",namespace="payments"`)["cpu_throttling_ratio"]
	labels := `cluster="fixture",namespace="payments",pod="app-rs-pod",uid="pod-uid",container="app",deployment="app",deployment_uid="deployment-uid"`
	numerator := `container_cpu_cfs_throttled_periods_total{` + labels + `}`
	denominator := `container_cpu_cfs_periods_total{` + labels + `}`
	fixture := struct {
		EvaluationInterval string     `json:"evaluation_interval"`
		Tests              []promTest `json:"tests"`
	}{EvaluationInterval: "30s"}
	add := func(name string, series []promSeries, value *float64) {
		samples := []promSample{}
		if value != nil {
			samples = append(samples, promSample{Labels: "{" + labels + "}", Value: *value})
		}
		fixture.Tests = append(fixture.Tests, promTest{Name: name, Interval: "30s", Series: series, Expressions: []promExpression{{Expr: query, EvalTime: "10m", Samples: samples}}})
	}
	zero, ratio := 0., .2
	add("idle is measured zero", []promSeries{{numerator, "0+0x20"}, {denominator, "0+0x20"}}, &zero)
	add("active unthrottled", []promSeries{{numerator, "0+0x20"}, {denominator, "0+100x20"}}, &zero)
	add("active ratio retains identity", []promSeries{{numerator, "0+20x20"}, {denominator, "0+100x20"}}, &ratio)
	add("missing numerator unavailable", []promSeries{{denominator, "0+100x20"}}, nil)
	add("missing denominator unavailable", []promSeries{{numerator, "0+20x20"}}, nil)
	add("inconsistent idle counters unavailable", []promSeries{{numerator, "0+20x20"}, {denominator, "0+0x20"}}, nil)
	injectedScope := mappingMatcher("cluster", "=", `fixture"} or vector(1) #`) + "," + mappingMatcher("namespace", "=", "payments")
	injectedQuery := scopedMappingQueries(c, injectedScope)["memory_working_set_bytes"]
	fixture.Tests = append(fixture.Tests, promTest{Name: "quoted label value cannot inject PromQL", Interval: "30s", Series: []promSeries{{`container_memory_working_set_bytes{` + labels + `}`, "1+0x20"}}, Expressions: []promExpression{{Expr: injectedQuery, EvalTime: "10m", Samples: []promSample{}}}})
	raw, err := json.Marshal(fixture)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "mapping-rules.json")
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, filepath.Join(filepath.Dir(binary), "promtool"), "test", "rules", path)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("mapping PromQL fixtures: %v\n%s", err, output)
	}
}
