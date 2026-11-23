package pricing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/consize-oss/consize/pkg/plugin"
	"github.com/consize-oss/consize/pkg/resource"
)

const ID = "configured-rate-card"

type Config struct {
	Currency         string  `json:"currency"`
	Source           string  `json:"source"`
	MemoryGiBMonthly float64 `json:"memory_gib_monthly"`
	CPUCoreMonthly   float64 `json:"cpu_core_monthly"`
}

type Plugin struct {
	cfg Config
	now func() time.Time
}

func New(cfg Config) (*Plugin, error) {
	if cfg.Currency == "" || cfg.Source == "" {
		return nil, errors.New("pricing currency and source are required")
	}
	if cfg.MemoryGiBMonthly < 0 || cfg.CPUCoreMonthly < 0 || (cfg.MemoryGiBMonthly == 0 && cfg.CPUCoreMonthly == 0) {
		return nil, errors.New("at least one non-negative pricing rate is required")
	}
	return &Plugin{cfg: cfg, now: time.Now}, nil
}

func (p *Plugin) ID() string { return ID }

func (p *Plugin) Manifest() plugin.Manifest {
	return plugin.WithBuiltinRuntime(plugin.Manifest{
		ID:                     ID,
		DisplayName:            "Configured Rate Card",
		Version:                "0.3.0-alpha",
		Category:               plugin.CategoryCost,
		SupportedResourceTypes: []string{resource.TypeKubernetesDeployment},
		Capabilities:           []string{plugin.CapabilityCostRead},
	}, "kubernetes")
}

func (p *Plugin) Health(context.Context) plugin.Health {
	return plugin.Health{Status: "healthy", Message: "configured pricing rates available", CheckedAt: p.now().UTC()}
}

func (p *Plugin) Estimate(_ context.Context, res resource.Resource, proposed map[string]any) (plugin.CostEstimate, error) {
	currentMemory, err := number(res.CurrentState["memory_request_bytes"])
	if err != nil {
		return plugin.CostEstimate{}, fmt.Errorf("current memory request: %w", err)
	}
	currentCPU, _ := optionalNumber(res.CurrentState["cpu_request_cores"])
	proposedMemory, proposedCPU := currentMemory, currentCPU
	resourceKind, _ := proposed["resource"].(string)
	request, err := number(proposed["request"])
	if err != nil {
		return plugin.CostEstimate{}, fmt.Errorf("proposed request: %w", err)
	}
	switch resourceKind {
	case "memory":
		proposedMemory = request
	case "cpu":
		proposedCPU = request
	default:
		return plugin.CostEstimate{}, fmt.Errorf("unsupported proposed resource %q", resourceKind)
	}
	current := monthly(currentMemory, currentCPU, p.cfg)
	proposal := monthly(proposedMemory, proposedCPU, p.cfg)
	now := p.now().UTC()
	return plugin.CostEstimate{
		PluginID:        ID,
		ResourceID:      res.ID,
		Classification:  "estimate",
		Currency:        p.cfg.Currency,
		CurrentMonthly:  round(current),
		ProposedMonthly: round(proposal),
		SavingsMonthly:  round(math.Max(0, current-proposal)),
		PricingSource:   p.cfg.Source,
		EffectiveAt:     now,
		CollectedAt:     now,
		Evidence: map[string]any{
			"memory_gib_monthly": p.cfg.MemoryGiBMonthly,
			"cpu_core_monthly":   p.cfg.CPUCoreMonthly,
			"billing_realized":   false,
		},
	}, nil
}

func monthly(memoryBytes, cpuCores float64, cfg Config) float64 {
	return memoryBytes/(1024*1024*1024)*cfg.MemoryGiBMonthly + cpuCores*cfg.CPUCoreMonthly
}

func number(value any) (float64, error) {
	n, ok := optionalNumber(value)
	if !ok || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 {
		return 0, errors.New("value must be a finite non-negative number")
	}
	return n, nil
}

func optionalNumber(value any) (float64, bool) {
	switch v := value.(type) {
	case json.Number:
		n, err := v.Float64()
		return n, err == nil
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case int32:
		return float64(v), true
	default:
		return 0, false
	}
}

func round(value float64) float64 { return math.Round(value*100) / 100 }
