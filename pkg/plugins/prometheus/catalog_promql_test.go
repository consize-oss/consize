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

func TestCatalogPromQLNormalizationAndCounterResets(t *testing.T) {
	binary := os.Getenv("CONSIZE_PROMETHEUS_BIN")
	if binary == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("CI requires pinned Prometheus/promtool")
		}
		t.Skip("set CONSIZE_PROMETHEUS_BIN from the pinned installer")
	}
	definitions, err := QueryCatalog(MappingConfig{}, "fixture", "payments")
	if err != nil {
		t.Fatal(err)
	}
	queries := map[string]string{}
	for _, def := range definitions {
		queries[def.Signal] = def.Query
	}
	labels := `cluster="fixture",namespace="payments",pod="app-rs-pod",uid="pod-uid",container="app",deployment="app",deployment_uid="deployment-uid"`
	fixture := struct {
		EvaluationInterval string     `json:"evaluation_interval"`
		Tests              []promTest `json:"tests"`
	}{EvaluationInterval: "30s"}
	add := func(name, signal, metric, extra, values, evaluation string, want float64, raw bool) {
		all := labels + extra
		expected := "{" + all + "}"
		if raw {
			expected = `{__name__="` + metric + `",` + all + `}`
		}
		fixture.Tests = append(fixture.Tests, promTest{Name: name, Interval: "30s", Series: []promSeries{{metric + "{" + all + "}", values}}, Expressions: []promExpression{{Expr: queries[signal], EvalTime: evaluation, Samples: []promSample{{Labels: expected, Value: want}}}}})
	}
	add("CPU counter seconds become cores", "cpu_cores", "container_cpu_usage_seconds_total", "", "0+15x20", "10m", .5, false)
	add("memory working set remains bytes", "memory_working_set_bytes", "container_memory_working_set_bytes", "", "1048576+0x20", "10m", 1048576, true)
	add("current CPU request is cores", "cpu_request_cores", "kube_pod_container_resource_requests", `,resource="cpu",unit="core"`, "0.25+0x20", "10m", .25, true)
	add("current memory limit is bytes", "memory_limit_bytes", "kube_pod_container_resource_limits", `,resource="memory",unit="byte"`, "536870912+0x20", "10m", 536870912, true)
	add("restart delta with no reset", "restarts_30m", "kube_pod_container_status_restarts_total", "", "1+1x60", "30m", 60, false)
	add("restart delta survives counter reset", "restarts_30m", "kube_pod_container_status_restarts_total", "", "1+1x29 1+1x30", "30m", 60, false)
	raw, err := json.Marshal(fixture)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "catalog-rules.json")
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, filepath.Join(filepath.Dir(binary), "promtool"), "test", "rules", path)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("catalog PromQL fixtures: %v\n%s", err, output)
	}
}
