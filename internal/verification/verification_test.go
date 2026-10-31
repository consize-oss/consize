package verification

import (
	"testing"
	"time"

	"github.com/consize-oss/consize/internal/bootstrap"
	"github.com/consize-oss/consize/internal/testevidence"
	"github.com/consize-oss/consize/pkg/plugin"
)

func TestVerifyPassesWithinThresholds(t *testing.T) {
	result := New(bootstrap.VerificationConfig{
		MaxMemoryP95IncreaseRatio: 1.25,
		MaxCPUP95IncreaseRatio:    1.50,
		MaxRestartIncrease:        0,
	}).Verify(snapshot(100, 1, 0), snapshot(110, 1.2, 0))
	if result.Status != StatusPassed {
		t.Fatalf("status = %q, reasons = %#v", result.Status, result.Reasons)
	}
}

func TestVerifyFailsWhenRestartsIncrease(t *testing.T) {
	result := New(bootstrap.VerificationConfig{
		MaxMemoryP95IncreaseRatio: 1.25,
		MaxCPUP95IncreaseRatio:    1.50,
		MaxRestartIncrease:        0,
	}).Verify(snapshot(100, 1, 0), snapshot(110, 1.2, 1))
	if result.Status != StatusFailed {
		t.Fatalf("status = %q, want failed", result.Status)
	}
}

func snapshot(memory, cpu, restarts float64) plugin.MetricsSnapshot {
	return testevidence.Attach(plugin.MetricsSnapshot{
		PluginID:    "prometheus-metrics",
		ResourceID:  "k8s:prod:checkout-api",
		Source:      "prometheus",
		Window:      "5m",
		CollectedAt: time.Now(),
		Signals: map[string]any{
			"memory_working_set_bytes_points": 10,
			"cpu_cores_points":                10,
			"restarts_30m_points":             10,
			"memory_working_set_bytes_p95":    memory,
			"cpu_cores_p95":                   cpu,
			"restarts_30m_max":                restarts,
		},
	})
}

func TestMissingMetricsAreInconclusive(t *testing.T) {
	before := snapshot(100, 1, 0)
	delete(before.Signals, "cpu_cores_points")
	if got := New(bootstrap.VerificationConfig{}).Verify(before, snapshot(100, 1, 0)); got.Status != StatusInconclusive {
		t.Fatalf("missing metrics: %#v", got)
	}
}

func TestInvalidSignalCannotPass(t *testing.T) {
	before := snapshot(100, 1, 0)
	after := snapshot(100, 1, 0)
	after.Signals["cpu_cores_p95"] = "invalid"
	if got := New(bootstrap.VerificationConfig{}).Verify(before, after); got.Status != StatusInconclusive {
		t.Fatal("invalid signal passed")
	}
}

func TestRestartWindowCannotHideRestartsBehindHigherBaseline(t *testing.T) {
	if got := New(bootstrap.VerificationConfig{}).Verify(snapshot(100, 1, 5), snapshot(100, 1, 2)); got.Status != StatusFailed {
		t.Fatal("new restarts were hidden by baseline")
	}
}

func TestSparseCoverageIsInconclusive(t *testing.T) {
	before := snapshot(100, 1, 0)
	before.Coverage = map[string]plugin.MetricCoverage{"cpu_cores": {ExpectedPoints: 100, ActualPoints: 2, Step: time.Minute, LastSample: before.CollectedAt}}
	if got := New(bootstrap.VerificationConfig{}).Verify(before, snapshot(100, 1, 0)); got.Status != StatusInconclusive {
		t.Fatal("sparse evidence passed")
	}
}

func TestConfiguredHealthChecks(t *testing.T) {
	for _, check := range []bootstrap.VerificationCheck{
		{Signal: "oom_events_30m", Statistic: "max", Mode: "absolute", Threshold: 0},
		{Signal: "evicted_pods", Statistic: "max", Mode: "absolute", Threshold: 0},
		{Signal: "cpu_throttling_ratio", Statistic: "p95", Mode: "absolute", Threshold: .1},
		{Signal: "error_rate", Statistic: "p95", Mode: "absolute", Threshold: .01},
		{Signal: "latency_seconds", Statistic: "p95", Mode: "ratio", Threshold: 1.3},
	} {
		t.Run(check.Signal, func(t *testing.T) {
			cfg := bootstrap.VerificationConfig{Checks: []bootstrap.VerificationCheck{check}}
			before, after := snapshot(100, 1, 0), snapshot(100, 1, 0)
			if got := New(cfg).Verify(before, after); got.Status != StatusInconclusive {
				t.Fatal("missing required check passed")
			}
			key := check.Signal + "_" + check.Statistic
			for _, sample := range []*plugin.MetricsSnapshot{&before, &after} {
				sample.Signals[check.Signal+"_points"] = 10
				sample.Signals[key] = check.Threshold
			}
			if check.Mode == "ratio" {
				before.Signals[key] = 1.
				after.Signals[key] = 1.2
			}
			before = testevidence.Attach(before)
			after = testevidence.Attach(after)
			if got := New(cfg).Verify(before, after); got.Status != StatusPassed {
				t.Fatalf("healthy check: %+v", got)
			}
			after.Signals[key] = check.Threshold + 1
			after = testevidence.Attach(after)
			if got := New(cfg).Verify(before, after); got.Status != StatusFailed {
				t.Fatalf("breach: %+v", got)
			}
			after.Signals[key] = "invalid"
			if got := New(cfg).Verify(before, after); got.Status != StatusInconclusive {
				t.Fatal("invalid check passed")
			}
		})
	}
}

func TestInvalidConfiguredCheck(t *testing.T) {
	if err := ValidateChecks(bootstrap.VerificationConfig{Checks: []bootstrap.VerificationCheck{{Signal: "latency", Statistic: "p99", Mode: "ratio", Threshold: 0}}}); err == nil {
		t.Fatal("invalid check accepted")
	}
}
