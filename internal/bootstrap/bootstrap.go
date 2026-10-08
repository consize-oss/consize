package bootstrap

import (
	"context"
	"fmt"
	"time"

	"github.com/consize-oss/consize/internal/auth"
	"github.com/consize-oss/consize/pkg/plugin"
	"github.com/consize-oss/consize/pkg/plugin/marketplace"
	"github.com/consize-oss/consize/pkg/plugins/kubernetes"
	"github.com/consize-oss/consize/pkg/plugins/pricing"
	"github.com/consize-oss/consize/pkg/plugins/prometheus"
)

type Config struct {
	ExternalPlugins []marketplace.ExternalConfig `json:"external_plugins,omitempty"`
	AllowedOrigins  []string                     `json:"allowed_origins"`
	DatabaseURL     string                       `json:"database_url,omitempty"`
	StatePath       string                       `json:"state_path"`
	Kubernetes      KubernetesConfig             `json:"kubernetes"`
	Prometheus      PrometheusConfig             `json:"prometheus"`
	Recommender     RecommenderConfig            `json:"recommender"`
	Verification    VerificationConfig           `json:"verification"`
	Audit           AuditConfig                  `json:"audit"`
	Auth            auth.Config                  `json:"auth"`
	Pricing         PricingConfig                `json:"pricing"`
}

type PricingConfig struct {
	Enabled          bool    `json:"enabled"`
	PluginID         string  `json:"plugin_id,omitempty"`
	Currency         string  `json:"currency"`
	Source           string  `json:"source"`
	MemoryGiBMonthly float64 `json:"memory_gib_monthly"`
	CPUCoreMonthly   float64 `json:"cpu_core_monthly"`
}

func (c PricingConfig) EffectivePluginID() string {
	if c.PluginID != "" {
		return c.PluginID
	}
	return pricing.ID
}

type KubernetesConfig struct {
	Enabled    bool   `json:"enabled"`
	Kubeconfig string `json:"kubeconfig"`
	ClusterID  string `json:"cluster_id"`
	Location   string `json:"location"`
}

type PrometheusConfig struct {
	Enabled bool              `json:"enabled"`
	BaseURL string            `json:"base_url"`
	Window  string            `json:"window"`
	Step    string            `json:"step"`
	Queries map[string]string `json:"queries"`
}

type RecommenderConfig struct {
	ConfidenceProfile string  `json:"confidence_profile,omitempty"`
	Enabled           bool    `json:"enabled"`
	MetricsPluginID   string  `json:"metrics_plugin_id"`
	ActionPluginID    string  `json:"action_plugin_id"`
	ActionType        string  `json:"action_type"`
	ResourceKind      string  `json:"resource_kind"`
	MetricKey         string  `json:"metric_key"`
	CurrentRequestKey string  `json:"current_request_key"`
	CurrentLimitKey   string  `json:"current_limit_key"`
	HeadroomRatio     float64 `json:"headroom_ratio"`
	MinReductionRatio float64 `json:"min_reduction_ratio"`
	MaxReductionRatio float64 `json:"max_reduction_ratio"`
}

type VerificationConfig struct {
	Checks                    []VerificationCheck `json:"checks,omitempty"`
	RollbackOnFailure         bool                `json:"rollback_on_failure"`
	RollbackOnTimeout         bool                `json:"rollback_on_timeout"`
	Timeout                   string              `json:"timeout"`
	Isolation                 string              `json:"isolation"`
	Enabled                   bool                `json:"enabled"`
	MetricsPluginID           string              `json:"metrics_plugin_id"`
	Wait                      string              `json:"wait"`
	MaxRestartIncrease        float64             `json:"max_restart_increase"`
	MaxMemoryP95IncreaseRatio float64             `json:"max_memory_p95_increase_ratio"`
	MaxCPUP95IncreaseRatio    float64             `json:"max_cpu_p95_increase_ratio"`
}

// VerificationCheck binds a plugin signal to an explicit safety threshold.
type VerificationCheck struct {
	Signal    string  `json:"signal"`
	Statistic string  `json:"statistic"` // p95, p99 or max
	Mode      string  `json:"mode"`      // absolute or ratio
	Threshold float64 `json:"threshold"`
}

type AuditConfig struct {
	Path string `json:"path"`
}

func RegisterConfiguredPlugins(_ context.Context, m *plugin.Manager, cfg Config) error {
	if cfg.Recommender.ConfidenceProfile != "" && cfg.Recommender.ConfidenceProfile != "standard" && cfg.Recommender.ConfidenceProfile != "lab" {
		return fmt.Errorf("confidence_profile must be standard or lab")
	}
	for _, external := range cfg.ExternalPlugins {
		if err := marketplace.Register(m, external); err != nil {
			return fmt.Errorf("configure external plugin: %w", err)
		}
	}
	if cfg.Kubernetes.Enabled {
		p, err := kubernetes.New(kubernetes.Config{
			Kubeconfig: cfg.Kubernetes.Kubeconfig,
			ClusterID:  cfg.Kubernetes.ClusterID,
			Location:   cfg.Kubernetes.Location,
		})
		if err != nil {
			return fmt.Errorf("configure kubernetes plugin: %w", err)
		}
		if err := m.RegisterAction(p); err != nil {
			return err
		}
		if err := m.RegisterDiscovery(p); err != nil {
			return err
		}
	}
	if cfg.Prometheus.Enabled {
		window, err := parseOptionalDuration(cfg.Prometheus.Window)
		if err != nil {
			return fmt.Errorf("prometheus window: %w", err)
		}
		step, err := parseOptionalDuration(cfg.Prometheus.Step)
		if err != nil {
			return fmt.Errorf("prometheus step: %w", err)
		}
		p, err := prometheus.New(prometheus.Config{
			BaseURL: cfg.Prometheus.BaseURL,
			Window:  window,
			Step:    step,
			Queries: cfg.Prometheus.Queries,
		})
		if err != nil {
			return fmt.Errorf("configure prometheus plugin: %w", err)
		}
		if err := m.RegisterMetrics(p); err != nil {
			return err
		}
	}
	if cfg.Pricing.Enabled {
		p, err := pricing.New(pricing.Config{Currency: cfg.Pricing.Currency, Source: cfg.Pricing.Source, MemoryGiBMonthly: cfg.Pricing.MemoryGiBMonthly, CPUCoreMonthly: cfg.Pricing.CPUCoreMonthly})
		if err != nil {
			return fmt.Errorf("configure pricing plugin: %w", err)
		}
		if cfg.Pricing.EffectivePluginID() != p.ID() {
			return fmt.Errorf("pricing plugin_id must be %q", p.ID())
		}
		if err := m.RegisterCost(p); err != nil {
			return err
		}
	}
	return nil
}

func parseOptionalDuration(raw string) (time.Duration, error) {
	if raw == "" {
		return 0, nil
	}
	return time.ParseDuration(raw)
}
