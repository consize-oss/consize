package bootstrap

import (
	"context"
	"fmt"
	"time"

	"github.com/consize-oss/consize/internal/auth"
	"github.com/consize-oss/consize/pkg/evidence/quality"
	"github.com/consize-oss/consize/pkg/plugin"
	"github.com/consize-oss/consize/pkg/plugin/marketplace"
	"github.com/consize-oss/consize/pkg/plugins/kubernetes"
	"github.com/consize-oss/consize/pkg/plugins/pricing"
	"github.com/consize-oss/consize/pkg/plugins/prometheus"
)

type Config struct {
	PluginInstances []plugin.InstanceConfig      `json:"plugin_instances,omitempty"`
	PluginSecretEnv map[string]string            `json:"plugin_secret_env,omitempty"`
	Discovery       DiscoveryScheduleConfig      `json:"discovery,omitempty"`
	Collection      CollectionConfig             `json:"collection,omitempty"`
	Worker          WorkerConfig                 `json:"worker,omitempty"`
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

// WorkerConfig bounds orchestration work; verification policy remains independent.
type WorkerConfig struct {
	PollInterval      string `json:"poll_interval,omitempty"`
	LeaseDuration     string `json:"lease_duration,omitempty"`
	HeartbeatInterval string `json:"heartbeat_interval,omitempty"`
	StepTimeout       string `json:"step_timeout,omitempty"`
	MaxAttempts       int    `json:"max_attempts,omitempty"`
	MaxJobsPerTick    int    `json:"max_jobs_per_tick,omitempty"`
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
	DiscoveryAuth *kubernetes.DiscoveryAuthConfig `json:"discovery_auth,omitempty"`
	Context       string                          `json:"context,omitempty"`
	Discovery     kubernetes.DiscoveryConfig      `json:"discovery,omitempty"`
	Enabled       bool                            `json:"enabled"`
	Kubeconfig    string                          `json:"kubeconfig"`
	ClusterID     string                          `json:"cluster_id"`
	Location      string                          `json:"location"`
}

type PrometheusConfig struct {
	SourceScrapeIntervals  map[string]string          `json:"source_scrape_intervals,omitempty"`
	SourceTimestampQueries map[string]string          `json:"source_timestamp_queries,omitempty"`
	SourceScrapeInterval   string                     `json:"source_scrape_interval,omitempty"`
	Percentile             int                        `json:"percentile,omitempty"`
	MinimumSamples         int                        `json:"minimum_samples,omitempty"`
	Mapping                prometheus.MappingConfig   `json:"mapping,omitempty"`
	Transport              prometheus.TransportConfig `json:"transport,omitempty"`
	Enabled                bool                       `json:"enabled"`
	BaseURL                string                     `json:"base_url"`
	Window                 string                     `json:"window"`
	Step                   string                     `json:"step"`
	Queries                map[string]string          `json:"queries"`
	Units                  map[string]string          `json:"units"`
}

type RecommenderConfig struct {
	AlgorithmPluginID string         `json:"algorithm_plugin_id,omitempty"`
	Quality           quality.Config `json:"quality,omitempty"`
	ConfidenceProfile string         `json:"confidence_profile,omitempty"`
	Enabled           bool           `json:"enabled"`
	MetricsPluginID   string         `json:"metrics_plugin_id"`
	ActionPluginID    string         `json:"action_plugin_id"`
	ActionType        string         `json:"action_type"`
	ResourceKind      string         `json:"resource_kind"`
	MetricKey         string         `json:"metric_key"`
	CurrentRequestKey string         `json:"current_request_key"`
	CurrentLimitKey   string         `json:"current_limit_key"`
	HeadroomRatio     float64        `json:"headroom_ratio"`
	MinReductionRatio float64        `json:"min_reduction_ratio"`
	MaxReductionRatio float64        `json:"max_reduction_ratio"`
}

type VerificationConfig struct {
	Quality                   quality.Config      `json:"quality,omitempty"`
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
	Unit      string  `json:"unit,omitempty"`
	Signal    string  `json:"signal"`
	Statistic string  `json:"statistic"` // p95, p99 or max
	Mode      string  `json:"mode"`      // absolute or ratio
	Threshold float64 `json:"threshold"`
}

type AuditConfig struct {
	Path string `json:"path"`
}

func RegisterConfiguredPlugins(ctx context.Context, m *plugin.Manager, cfg Config) error {
	if cfg.Recommender.ConfidenceProfile != "" && cfg.Recommender.ConfidenceProfile != "standard" && cfg.Recommender.ConfidenceProfile != "lab" {
		return fmt.Errorf("confidence_profile must be standard or lab")
	}
	for _, external := range cfg.ExternalPlugins {
		if err := marketplace.Register(m, external); err != nil {
			return fmt.Errorf("configure external plugin: %w", err)
		}
	}
	if cfg.Pricing.Enabled && cfg.Pricing.EffectivePluginID() != pricing.ID {
		return fmt.Errorf("pricing plugin_id must be %q", pricing.ID)
	}

	if err := registerInstances(ctx, m, cfg); err != nil {
		return err
	}
	return ValidateCapabilities(m, cfg)
}

func parseOptionalDuration(raw string) (time.Duration, error) {
	if raw == "" {
		return 0, nil
	}
	return time.ParseDuration(raw)
}

type CollectionConfig struct {
	Enabled        bool   `json:"enabled"`
	PluginID       string `json:"plugin_id,omitempty"`
	Interval       string `json:"interval,omitempty"`
	Window         string `json:"window,omitempty"`
	PollInterval   string `json:"poll_interval,omitempty"`
	Timeout        string `json:"timeout,omitempty"`
	Retention      string `json:"retention,omitempty"`
	MaxAttempts    int    `json:"max_attempts,omitempty"`
	MaxJobsPerTick int    `json:"max_jobs_per_tick,omitempty"`
}

type DiscoveryScheduleConfig struct {
	Enabled        bool   `json:"enabled"`
	Interval       string `json:"interval,omitempty"`
	PollInterval   string `json:"poll_interval,omitempty"`
	Timeout        string `json:"timeout,omitempty"`
	StaleAfter     string `json:"stale_after,omitempty"`
	TombstoneAfter string `json:"tombstone_after,omitempty"`
	MaxAttempts    int    `json:"max_attempts,omitempty"`
	MaxRunsPerTick int    `json:"max_runs_per_tick,omitempty"`
}
