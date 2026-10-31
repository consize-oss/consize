package plugin

import (
	"context"
	"time"

	"github.com/consize-oss/consize/pkg/evidence"
	"github.com/consize-oss/consize/pkg/resource"
)

const (
	CategoryAction     = "action"
	CategoryDataSource = "data_source"
	CategoryMetrics    = "metrics"
	CategoryCost       = "cost"
	CategoryAlgorithm  = "algorithm"
)

const (
	CapabilityActionPlan       = "action.plan"
	CapabilityActionExecute    = "action.execute"
	CapabilityActionPreflight  = "action.preflight"
	CapabilityMetricsRead      = "metrics.read"
	CapabilityCostRead         = "cost.read"
	CapabilityResourceDiscover = "resource.discover"
)

type Manifest struct {
	ID                      string   `json:"id"`
	DisplayName             string   `json:"display_name"`
	Version                 string   `json:"version"`
	Category                string   `json:"category"`
	SupportedResourceTypes  []string `json:"supported_resource_types"`
	SupportedActionTypes    []string `json:"supported_action_types"`
	Capabilities            []string `json:"capabilities"`
	CanMutateInfrastructure bool     `json:"can_mutate_infrastructure"`
	RequiresApproval        bool     `json:"requires_approval"`
}

type Health struct {
	Status    string    `json:"status"`
	Message   string    `json:"message"`
	CheckedAt time.Time `json:"checked_at"`
}

type Plugin interface {
	ID() string
	Manifest() Manifest
	Health(ctx context.Context) Health
}

type ActionInput struct {
	Resource   resource.Resource `json:"resource"`
	ActionType string            `json:"action_type"`
	Mode       string            `json:"mode"`
	Actor      string            `json:"actor"`
	Parameters map[string]any    `json:"parameters"`
}

type ActionPlan struct {
	Preflight            []PreflightCheck `json:"preflight,omitempty"`
	PluginArtifactDigest string           `json:"plugin_artifact_digest,omitempty"`
	PluginVersion        string           `json:"plugin_version,omitempty"`
	OriginalState        map[string]any   `json:"original_state,omitempty"`
	AppliedState         map[string]any   `json:"applied_state,omitempty"`
	PluginID             string           `json:"plugin_id"`
	ResourceID           string           `json:"resource_id"`
	ActionType           string           `json:"action_type"`
	Summary              string           `json:"summary"`
	Diff                 map[string]any   `json:"diff"`
	RollbackAvailable    bool             `json:"rollback_available"`
	RequiresApproval     bool             `json:"requires_approval"`
}

type PreflightCheck struct {
	ID      string         `json:"id"`
	Status  string         `json:"status"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}
type PreflightActionPlugin interface {
	ActionPlugin
	Preflight(context.Context, ActionPlan) ([]PreflightCheck, error)
}

func RequirePreflight(checks []PreflightCheck) error {
	if len(checks) == 0 {
		return &PreflightError{Checks: []PreflightCheck{{ID: "preflight", Status: "unknown", Message: "preflight evidence is missing"}}}
	}
	for _, check := range checks {
		if check.Status != "passed" {
			return &PreflightError{Checks: checks}
		}
	}
	return nil
}

type PreflightError struct{ Checks []PreflightCheck }

func (e *PreflightError) Error() string {
	message := "action preflight rejected"
	for _, c := range e.Checks {
		if c.Status != "passed" {
			message += ": " + c.Message
		}
	}
	return message
}

// ArtifactIdentity pins downloaded code for the lifetime of an action.
type ArtifactIdentity interface{ ArtifactDigest() string }

type ActionResult struct {
	PluginID   string         `json:"plugin_id"`
	ResourceID string         `json:"resource_id"`
	ActionType string         `json:"action_type"`
	Applied    bool           `json:"applied"`
	Message    string         `json:"message"`
	Evidence   map[string]any `json:"evidence"`
}

type MetricsSnapshot struct {
	Records     []evidence.Record         `json:"records,omitempty"`
	Coverage    map[string]MetricCoverage `json:"coverage,omitempty"`
	PluginID    string                    `json:"plugin_id"`
	ResourceID  string                    `json:"resource_id"`
	Source      string                    `json:"source"`
	Window      string                    `json:"window"`
	Signals     map[string]any            `json:"signals"`
	Evidence    []string                  `json:"evidence"`
	CollectedAt time.Time                 `json:"collected_at"`
}

type MetricCoverage struct {
	MaxGap         time.Duration `json:"max_gap,omitempty"`
	FirstSample    time.Time     `json:"first_sample"`
	LastSample     time.Time     `json:"last_sample"`
	ExpectedPoints int           `json:"expected_points"`
	ActualPoints   int           `json:"actual_points"`
	Step           time.Duration `json:"step"`
}

type ActionPlugin interface {
	Plugin
	Plan(ctx context.Context, input ActionInput) (ActionPlan, error)
	Execute(ctx context.Context, plan ActionPlan) (ActionResult, error)
}

// RecoverableActionPlugin makes apply and rollback idempotent and rejects external drift.
type RecoverableActionPlugin interface {
	ActionPlugin
	Inspect(ctx context.Context, plan ActionPlan) (string, error)
	Rollback(ctx context.Context, plan ActionPlan) (ActionResult, error)
	Ready(ctx context.Context, plan ActionPlan) error
}

const (
	StateOriginal = "original"
	StateApplied  = "applied"
	StateDrifted  = "drifted"
)

type MetricsPlugin interface {
	Plugin
	ReadMetrics(ctx context.Context, res resource.Resource) (MetricsSnapshot, error)
}

// CostEstimate prices a proposed state. It is intentionally distinct from
// realized billing, which requires provider invoice or billing-export data.
type CostEstimate struct {
	PluginID        string         `json:"plugin_id"`
	ResourceID      string         `json:"resource_id"`
	Classification  string         `json:"classification"`
	Currency        string         `json:"currency"`
	CurrentMonthly  float64        `json:"current_monthly"`
	ProposedMonthly float64        `json:"proposed_monthly"`
	SavingsMonthly  float64        `json:"savings_monthly"`
	PricingSource   string         `json:"pricing_source"`
	EffectiveAt     time.Time      `json:"effective_at"`
	CollectedAt     time.Time      `json:"collected_at"`
	Evidence        map[string]any `json:"evidence,omitempty"`
}

type CostPlugin interface {
	Plugin
	Estimate(ctx context.Context, res resource.Resource, proposed map[string]any) (CostEstimate, error)
}

// ProviderObservation is the immutable boundary between provider discovery and
// the universal registry. Provider plugins own collection; core owns identity,
// persistence, lifecycle, and policy-visible normalization.
type ProviderObservation struct {
	PluginID   string            `json:"plugin_id"`
	Provider   string            `json:"provider"`
	ObservedAt time.Time         `json:"observed_at"`
	Resource   resource.Resource `json:"resource"`
}

type DiscoveryPlugin interface {
	Plugin
	Discover(context.Context) ([]ProviderObservation, error)
}

type WindowedMetricsPlugin interface {
	MetricsPlugin
	ReadMetricsBetween(ctx context.Context, res resource.Resource, start, end time.Time) (MetricsSnapshot, error)
}

func Supports(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
