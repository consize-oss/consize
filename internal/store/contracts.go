package store

import (
	"errors"
	"fmt"
	"time"

	"github.com/consize-oss/consize/internal/policy"
	"github.com/consize-oss/consize/pkg/plugin"
)

const ContractVersion = 1

type RecommendationStatus string

const (
	RecommendationPending            RecommendationStatus = "pending"
	RecommendationPlanned            RecommendationStatus = "planned"
	RecommendationApproved           RecommendationStatus = "approved"
	RecommendationExecuting          RecommendationStatus = "executing"
	RecommendationVerified           RecommendationStatus = "verified"
	RecommendationRejected           RecommendationStatus = "rejected"
	RecommendationExpired            RecommendationStatus = "expired"
	RecommendationSuperseded         RecommendationStatus = "superseded"
	RecommendationFailed             RecommendationStatus = "failed"
	RecommendationRolledBack         RecommendationStatus = "rolled_back"
	RecommendationManualIntervention RecommendationStatus = "manual_intervention"
)

type ActionStatus string

const (
	ActionRequested          ActionStatus = "requested"
	ActionPlanning           ActionStatus = "planning"
	ActionPlanned            ActionStatus = "planned"
	ActionApproved           ActionStatus = "approved"
	ActionExecuting          ActionStatus = "executing"
	ActionVerifying          ActionStatus = "verifying"
	ActionSucceeded          ActionStatus = "succeeded"
	ActionFailed             ActionStatus = "failed"
	ActionRollbackPending    ActionStatus = "rollback_pending"
	ActionRollingBack        ActionStatus = "rolling_back"
	ActionRolledBack         ActionStatus = "rolled_back"
	ActionManualIntervention ActionStatus = "manual_intervention"
	ActionCancelled          ActionStatus = "cancelled"
)

type SavingsClassification string

const (
	SavingsEstimated           SavingsClassification = "estimated"
	SavingsOperationalVerified SavingsClassification = "operationally_verified"
	SavingsFinancialRealized   SavingsClassification = "financially_realized"
)

type SavingsEstimate struct {
	Classification SavingsClassification `json:"classification"`
	AmountMonthly  float64               `json:"amount_monthly"`
	Currency       string                `json:"currency,omitempty"`
	Source         string                `json:"source,omitempty"`
	CalculatedAt   time.Time             `json:"calculated_at,omitempty"`
}

type PlanResult struct {
	Plan      plugin.ActionPlan `json:"plan"`
	CreatedAt time.Time         `json:"created_at"`
}

type ExecutionResult struct {
	Result     plugin.ActionResult `json:"result"`
	StartedAt  time.Time           `json:"started_at,omitempty"`
	FinishedAt time.Time           `json:"finished_at,omitempty"`
}

type Action struct {
	SchemaVersion    int              `json:"schema_version"`
	ID               int64            `json:"id"`
	RecommendationID int64            `json:"recommendation_id"`
	ResourceID       string           `json:"resource_id"`
	RemediationPath  string           `json:"remediation_path"`
	PluginID         string           `json:"plugin_id"`
	PluginVersion    string           `json:"plugin_version,omitempty"`
	ActionType       string           `json:"action_type"`
	Mode             string           `json:"mode"`
	IdempotencyKey   string           `json:"idempotency_key"`
	CorrelationID    string           `json:"correlation_id,omitempty"`
	RequestedBy      string           `json:"requested_by"`
	ApprovedBy       string           `json:"approved_by,omitempty"`
	PolicyDecision   policy.Decision  `json:"policy_decision"`
	Status           ActionStatus     `json:"status"`
	Parameters       map[string]any   `json:"parameters"`
	PlanResult       *PlanResult      `json:"plan_result,omitempty"`
	ExecutionResult  *ExecutionResult `json:"execution_result,omitempty"`
	FailureCode      string           `json:"failure_code,omitempty"`
	FailureMessage   string           `json:"failure_message,omitempty"`
	RequestedAt      time.Time        `json:"requested_at"`
	ApprovedAt       time.Time        `json:"approved_at,omitempty"`
	StartedAt        time.Time        `json:"started_at,omitempty"`
	FinishedAt       time.Time        `json:"finished_at,omitempty"`
	UpdatedAt        time.Time        `json:"updated_at"`
}

var (
	ErrInvalidTransition  = errors.New("invalid state transition")
	ErrRecommendationGone = errors.New("recommendation is expired or superseded")
)

var recommendationTransitions = map[RecommendationStatus]map[RecommendationStatus]bool{
	RecommendationManualIntervention: {RecommendationExecuting: true},
	RecommendationPending:            {RecommendationPlanned: true, RecommendationApproved: true, RecommendationRejected: true, RecommendationExpired: true, RecommendationSuperseded: true},
	RecommendationPlanned:            {RecommendationApproved: true, RecommendationRejected: true, RecommendationExpired: true, RecommendationSuperseded: true},
	RecommendationApproved:           {RecommendationManualIntervention: true, RecommendationExecuting: true, RecommendationRejected: true, RecommendationExpired: true, RecommendationSuperseded: true},
	RecommendationExecuting:          {RecommendationVerified: true, RecommendationFailed: true, RecommendationRolledBack: true, RecommendationManualIntervention: true},
}

var actionTransitions = map[ActionStatus]map[ActionStatus]bool{
	ActionManualIntervention: {ActionRollbackPending: true},
	ActionRequested:          {ActionPlanning: true, ActionPlanned: true, ActionApproved: true, ActionFailed: true, ActionCancelled: true},
	ActionPlanning:           {ActionPlanned: true, ActionFailed: true, ActionCancelled: true},
	ActionPlanned:            {ActionApproved: true, ActionCancelled: true, ActionFailed: true},
	ActionApproved:           {ActionManualIntervention: true, ActionExecuting: true, ActionCancelled: true, ActionFailed: true},
	ActionExecuting:          {ActionCancelled: true, ActionVerifying: true, ActionSucceeded: true, ActionFailed: true, ActionRollbackPending: true, ActionManualIntervention: true},
	ActionVerifying:          {ActionSucceeded: true, ActionFailed: true, ActionRollbackPending: true, ActionManualIntervention: true},
	ActionFailed:             {ActionRollbackPending: true, ActionManualIntervention: true},
	ActionRollbackPending:    {ActionRollingBack: true, ActionManualIntervention: true},
	ActionRollingBack:        {ActionRolledBack: true, ActionManualIntervention: true},
}

func ValidRecommendationTransition(from, to RecommendationStatus) bool {
	return from != to && recommendationTransitions[from][to]
}

func ValidActionTransition(from, to ActionStatus) bool {
	return from != to && actionTransitions[from][to]
}

func RecommendationTerminal(status RecommendationStatus) bool {
	switch status {
	case RecommendationVerified, RecommendationRejected, RecommendationExpired, RecommendationSuperseded, RecommendationFailed, RecommendationRolledBack, RecommendationManualIntervention:
		return true
	default:
		return false
	}
}

func ActionTerminal(status ActionStatus) bool {
	switch status {
	case ActionSucceeded, ActionRolledBack, ActionManualIntervention, ActionCancelled:
		return true
	default:
		return false
	}
}

func (a Action) Validate() error {
	if a.RecommendationID < 1 || a.ResourceID == "" || a.PluginID == "" || a.ActionType == "" || a.IdempotencyKey == "" || a.RequestedBy == "" {
		return errors.New("recommendation_id, resource_id, plugin_id, action_type, idempotency_key, and requested_by are required")
	}
	if a.Mode != "dry_run" && a.Mode != "approved" {
		return fmt.Errorf("unsupported action mode %q", a.Mode)
	}
	if a.RemediationPath == "" {
		return errors.New("remediation_path is required")
	}
	return nil
}
