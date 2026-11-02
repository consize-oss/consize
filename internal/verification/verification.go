package verification

import (
	"fmt"
	"math"
	"time"

	"github.com/consize-oss/consize/internal/bootstrap"
	"github.com/consize-oss/consize/pkg/plugin"
)

const (
	StatusPassed       = "passed"
	StatusFailed       = "failed"
	StatusInconclusive = "inconclusive"
)

type Result struct {
	Status     string                 `json:"status"`
	Reasons    []string               `json:"reasons"`
	Before     plugin.MetricsSnapshot `json:"before"`
	After      plugin.MetricsSnapshot `json:"after"`
	CheckedAt  time.Time              `json:"checked_at"`
	Thresholds map[string]any         `json:"thresholds"`
}

type Service struct {
	cfg bootstrap.VerificationConfig
}

func New(cfg bootstrap.VerificationConfig) *Service {
	return &Service{cfg: withDefaults(cfg)}
}

func ValidateChecks(cfg bootstrap.VerificationConfig) error {
	seen := map[string]bool{}
	for _, check := range cfg.Checks {
		if check.Signal == "" || seen[check.Signal] || (check.Statistic != "p95" && check.Statistic != "p99" && check.Statistic != "max") || (check.Mode != "absolute" && check.Mode != "ratio") || math.IsNaN(check.Threshold) || math.IsInf(check.Threshold, 0) || check.Threshold < 0 || (check.Mode == "ratio" && check.Threshold < 1) {
			return fmt.Errorf("invalid or duplicate verification check: %q", check.Signal)
		}
		seen[check.Signal] = true
	}
	return nil
}

func RequiredSignals(cfg bootstrap.VerificationConfig) []string {
	keys := []string{"memory_working_set_bytes", "cpu_cores", "restarts_30m"}
	seen := map[string]bool{}
	for _, key := range keys {
		seen[key] = true
	}
	for _, check := range cfg.Checks {
		if !seen[check.Signal] {
			keys = append(keys, check.Signal)
			seen[check.Signal] = true
		}
	}
	return keys
}

func (s *Service) Verify(before, after plugin.MetricsSnapshot) Result {
	reasons := []string{}
	if err := ValidateChecks(s.cfg); err != nil {
		return Result{Status: StatusInconclusive, Reasons: []string{err.Error()}, Before: before, After: after, CheckedAt: time.Now().UTC()}
	}
	for _, snapshot := range []plugin.MetricsSnapshot{before, after} {
		for _, key := range RequiredSignals(s.cfg) {
			if signal(snapshot, key+"_points") < 2 {
				reasons = append(reasons, "insufficient samples for "+key)
			}
			if coverage, ok := snapshot.Coverage[key]; ok {
				if coverage.ExpectedPoints < 2 || coverage.ActualPoints < 2 || float64(coverage.ActualPoints)/float64(coverage.ExpectedPoints) < 0.8 || coverage.LastSample.IsZero() || coverage.Step <= 0 || snapshot.CollectedAt.Sub(coverage.LastSample) > 3*coverage.Step {
					reasons = append(reasons, "insufficient or stale coverage for "+key)
				}
			}
			metric := key + "_p95"
			if key == "restarts_30m" {
				metric = key + "_max"
			}
			for _, check := range s.cfg.Checks {
				if check.Signal == key {
					metric = key + "_" + check.Statistic
				}
			}
			unit := "count"
			if key == "memory_working_set_bytes" {
				unit = "bytes"
			}
			if key == "cpu_cores" {
				unit = "cores"
			}
			if key == "cpu_throttling_ratio" {
				unit = "ratio"
			}
			if key == "error_rate" {
				unit = "ratio"
			}
			if key == "latency_seconds" {
				unit = "seconds"
			}
			for _, check := range s.cfg.Checks {
				if check.Signal == key && check.Unit != "" {
					unit = check.Unit
				}
			}
			if _, err := snapshot.Measurement(metric, unit, snapshot.CollectedAt); err != nil {
				reasons = append(reasons, "missing or untrustworthy provenance: "+metric)
			}
			value, exists := numericSignal(snapshot, metric)
			if !exists || value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
				reasons = append(reasons, "missing or invalid signal: "+metric)
			}
		}
	}
	if len(reasons) > 0 {
		return Result{Status: StatusInconclusive, Reasons: reasons, Before: before, After: after, CheckedAt: time.Now().UTC()}
	}
	for _, check := range s.cfg.Checks {
		key := check.Signal + "_" + check.Statistic
		failed := signal(after, key) > check.Threshold
		if check.Mode == "ratio" {
			failed = exceededRatio(before, after, key, check.Threshold)
		}
		if failed {
			reasons = append(reasons, fmt.Sprintf("%s %s exceeded %s threshold %.4g", check.Signal, check.Statistic, check.Mode, check.Threshold))
		}
	}
	if exceededRatio(before, after, "memory_working_set_bytes_p95", s.cfg.MaxMemoryP95IncreaseRatio) {
		reasons = append(reasons, fmt.Sprintf("memory p95 increased above %.2fx threshold", s.cfg.MaxMemoryP95IncreaseRatio))
	}
	if exceededRatio(before, after, "cpu_cores_p95", s.cfg.MaxCPUP95IncreaseRatio) {
		reasons = append(reasons, fmt.Sprintf("cpu p95 increased above %.2fx threshold", s.cfg.MaxCPUP95IncreaseRatio))
	}
	restartIncrease := signal(after, "restarts_30m_max")
	if restartIncrease > s.cfg.MaxRestartIncrease {
		reasons = append(reasons, fmt.Sprintf("restart increase %.2f exceeded %.2f", restartIncrease, s.cfg.MaxRestartIncrease))
	}
	status := StatusPassed
	if len(reasons) > 0 {
		status = StatusFailed
	}
	return Result{
		Status:    status,
		Reasons:   reasons,
		Before:    before,
		After:     after,
		CheckedAt: time.Now().UTC(),
		Thresholds: map[string]any{
			"checks":                        s.cfg.Checks,
			"max_memory_p95_increase_ratio": s.cfg.MaxMemoryP95IncreaseRatio,
			"max_cpu_p95_increase_ratio":    s.cfg.MaxCPUP95IncreaseRatio,
			"max_restart_increase":          s.cfg.MaxRestartIncrease,
		},
	}
}

func withDefaults(cfg bootstrap.VerificationConfig) bootstrap.VerificationConfig {
	if cfg.MetricsPluginID == "" {
		cfg.MetricsPluginID = "prometheus-metrics"
	}
	if cfg.MaxMemoryP95IncreaseRatio == 0 {
		cfg.MaxMemoryP95IncreaseRatio = 1.25
	}
	if cfg.MaxCPUP95IncreaseRatio == 0 {
		cfg.MaxCPUP95IncreaseRatio = 1.50
	}
	if cfg.MaxRestartIncrease == 0 {
		cfg.MaxRestartIncrease = 0
	}
	return cfg
}

func exceededRatio(before, after plugin.MetricsSnapshot, key string, maxRatio float64) bool {
	b := signal(before, key)
	a := signal(after, key)
	if b <= 0 {
		return a > 0
	}
	return a/b > maxRatio
}

func signal(snapshot plugin.MetricsSnapshot, key string) float64 {
	n, _ := numericSignal(snapshot, key)
	return n
}

func numericSignal(snapshot plugin.MetricsSnapshot, key string) (float64, bool) {
	v, ok := snapshot.Signals[key]
	if !ok {
		return 0, false
	}
	switch t := v.(type) {
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case float64:
		return t, true
	case jsonNumber:
		n, err := t.Float64()
		return n, err == nil
	default:
		return 0, false
	}
}

type jsonNumber interface {
	Float64() (float64, error)
}
