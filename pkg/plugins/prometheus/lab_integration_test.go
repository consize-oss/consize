package prometheus_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/consize-oss/consize/internal/bootstrap"
	"github.com/consize-oss/consize/internal/verification"
	"github.com/consize-oss/consize/pkg/plugins/prometheus"
	"github.com/consize-oss/consize/pkg/resource"
)

func TestLiveLabVerificationEvidence(t *testing.T) {
	if os.Getenv("CONSIZE_LAB_METRICS") != "1" {
		t.Skip("set CONSIZE_LAB_METRICS=1 with local Prometheus forwarded on 9090")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	p, err := prometheus.New(prometheus.Config{BaseURL: "http://127.0.0.1:9090", Window: 30 * time.Minute, Step: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := p.ReadMetrics(ctx, resource.Resource{ID: "k8s:local:consize-demo:checkout-api", Type: resource.TypeKubernetesDeployment, Name: "checkout-api", Metadata: map[string]any{"namespace": "consize-demo"}})
	if err != nil {
		t.Fatal(err)
	}
	for signal, coverage := range snapshot.Coverage {
		t.Logf("%s: %d/%d samples; latest age=%s; largest gap=%s", signal, coverage.ActualPoints, coverage.ExpectedPoints, snapshot.CollectedAt.Sub(coverage.LastSample), coverage.MaxGap)
	}
	cfg := bootstrap.VerificationConfig{Checks: []bootstrap.VerificationCheck{{Signal: "cpu_throttling_ratio", Statistic: "p95", Mode: "absolute", Threshold: .1}, {Signal: "oom_events_30m", Statistic: "max", Mode: "absolute", Threshold: 0}, {Signal: "evicted_pods", Statistic: "max", Mode: "absolute", Threshold: 0}}}
	result := verification.New(cfg).Verify(snapshot, snapshot)
	if result.Status != verification.StatusPassed {
		t.Fatalf("baseline did not qualify: %+v", result.Reasons)
	}
	for _, signal := range verification.RequiredSignals(cfg) {
		coverage := snapshot.Coverage[signal]
		if coverage.ActualPoints < 2 || coverage.ExpectedPoints < 2 {
			t.Fatalf("%s missing actual evidence", signal)
		}
		t.Logf("%s: %d/%d samples", signal, coverage.ActualPoints, coverage.ExpectedPoints)
	}
}
