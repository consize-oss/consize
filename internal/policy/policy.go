package policy

import (
	"context"
	"fmt"
	"strings"

	"github.com/consize-oss/consize/pkg/plugin"
	"github.com/consize-oss/consize/pkg/resource"
)

const (
	DecisionBlocked          = "blocked"
	DecisionRecommendOnly    = "recommend_only"
	DecisionApprovalRequired = "approval_required"
	DecisionAutoApply        = "auto_apply"
	DecisionApproved         = "approved"
)

type Decision struct {
	PolicyID string   `json:"policy_id"`
	Decision string   `json:"decision"`
	Reasons  []string `json:"reasons"`
}

type Engine struct {
	AutoApplyNonProduction bool
}

func NewEngine() *Engine {
	return &Engine{}
}

func (e *Engine) Evaluate(_ context.Context, res resource.Resource, manifest plugin.Manifest, mode, actor string) Decision {
	reasons := []string{}
	if res.ID == "" {
		return Decision{PolicyID: "oss-foundation-v1", Decision: DecisionBlocked, Reasons: []string{"resource id is required"}}
	}
	if res.Type == "" {
		return Decision{PolicyID: "oss-foundation-v1", Decision: DecisionBlocked, Reasons: []string{"resource type is required"}}
	}
	if !plugin.Supports(manifest.SupportedResourceTypes, res.Type) {
		return Decision{PolicyID: "oss-foundation-v1", Decision: DecisionBlocked, Reasons: []string{fmt.Sprintf("plugin %s does not support %s", manifest.ID, res.Type)}}
	}
	if strings.TrimSpace(res.Owner) == "" || strings.EqualFold(strings.TrimSpace(res.Owner), resource.OwnerUnassigned) {
		reasons = append(reasons, "resource has no owner")
	}
	if res.Environment != resource.EnvProduction && res.Environment != resource.EnvStaging && res.Environment != resource.EnvDevelopment {
		reasons = append(reasons, "resource environment is unknown")
	}
	if res.Environment == resource.EnvProduction {
		reasons = append(reasons, "resource is production")
	}
	if res.Criticality == resource.CriticalityHigh {
		reasons = append(reasons, "resource criticality is high")
	}
	if manifest.CanMutateInfrastructure || manifest.RequiresApproval {
		reasons = append(reasons, "plugin can mutate infrastructure or requires approval")
	}
	switch mode {
	case "dry_run":
		return Decision{PolicyID: "oss-foundation-v1", Decision: DecisionRecommendOnly, Reasons: append(reasons, "dry run requested")}
	case "approved":
		if actor == "" {
			return Decision{PolicyID: "oss-foundation-v1", Decision: DecisionBlocked, Reasons: []string{"approved mode requires actor"}}
		}
		return Decision{PolicyID: "oss-foundation-v1", Decision: DecisionApproved, Reasons: append(reasons, "explicit approval supplied by actor")}
	case "auto":
		if len(reasons) == 0 && e.AutoApplyNonProduction && (res.Environment == resource.EnvStaging || res.Environment == resource.EnvDevelopment) {
			return Decision{PolicyID: "oss-foundation-v1", Decision: DecisionAutoApply, Reasons: []string{"non-production auto-apply is allowed"}}
		}
		return Decision{PolicyID: "oss-foundation-v1", Decision: DecisionApprovalRequired, Reasons: append(reasons, "auto mode is not allowed by current policy")}
	default:
		return Decision{PolicyID: "oss-foundation-v1", Decision: DecisionBlocked, Reasons: []string{"mode must be dry_run, approved, or auto"}}
	}
}
