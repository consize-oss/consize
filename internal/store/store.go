package store

import (
	"context"
	"time"

	"github.com/consize-oss/consize/internal/policy"
	"github.com/consize-oss/consize/internal/verification"
	"github.com/consize-oss/consize/pkg/evidence"
	"github.com/consize-oss/consize/pkg/plugin"
	"github.com/consize-oss/consize/pkg/resource"
)

type Recommendation struct {
	EvidenceSnapshot        []evidence.Record    `json:"evidence_snapshot,omitempty"`
	SchemaVersion           int                  `json:"schema_version"`
	ID                      int64                `json:"id"`
	ResourceID              string               `json:"resource_id"`
	PluginID                string               `json:"plugin_id"`
	AlgorithmID             string               `json:"algorithm_id"`
	AlgorithmVersion        string               `json:"algorithm_version"`
	RecommendationType      string               `json:"recommendation_type"`
	ActionType              string               `json:"action_type"`
	Title                   string               `json:"title"`
	Summary                 string               `json:"summary"`
	Current                 map[string]any       `json:"current"`
	Proposed                map[string]any       `json:"proposed"`
	Parameters              map[string]any       `json:"parameters"`
	EstimatedSavingsMonthly float64              `json:"estimated_savings_monthly"`
	SavingsEstimate         SavingsEstimate      `json:"savings_estimate"`
	CostEstimate            *plugin.CostEstimate `json:"cost_estimate,omitempty"`
	Confidence              string               `json:"confidence"`
	Risk                    string               `json:"risk"`
	Evidence                []string             `json:"evidence"`
	EvidenceRefs            []string             `json:"evidence_refs"`
	PolicyID                string               `json:"policy_id"`
	Status                  RecommendationStatus `json:"status"`
	ExpiresAt               time.Time            `json:"expires_at,omitempty"`
	SupersededBy            int64                `json:"superseded_by,omitempty"`
	StatusReason            string               `json:"status_reason,omitempty"`
	CreatedAt               time.Time            `json:"created_at"`
	UpdatedAt               time.Time            `json:"updated_at"`
}

type ActionEvent struct {
	VerificationResult *verification.Result `json:"verification_result,omitempty"`
	ID                 int64                `json:"id"`
	ActionID           int64                `json:"action_id,omitempty"`
	RecommendationID   int64                `json:"recommendation_id,omitempty"`
	ResourceID         string               `json:"resource_id"`
	PluginID           string               `json:"plugin_id"`
	ActionType         string               `json:"action_type"`
	Actor              string               `json:"actor"`
	Mode               string               `json:"mode"`
	Result             string               `json:"result"`
	Message            string               `json:"message"`
	Parameters         map[string]any       `json:"parameters"`
	Plan               *plugin.ActionPlan   `json:"plan,omitempty"`
	PluginResult       *plugin.ActionResult `json:"plugin_result,omitempty"`
	PolicyDecision     policy.Decision      `json:"policy_decision"`
	CreatedAt          time.Time            `json:"created_at"`
}

type Store interface {
	resource.Registry
	Health(ctx context.Context) error
	CreateRecommendation(ctx context.Context, rec Recommendation) (Recommendation, error)
	GetRecommendation(ctx context.Context, id int64) (Recommendation, error)
	ListRecommendations(ctx context.Context) ([]Recommendation, error)
	TransitionRecommendation(ctx context.Context, id int64, status RecommendationStatus, reason string) error
	SupersedeRecommendation(ctx context.Context, id, replacementID int64, reason string) error
	CreateAction(ctx context.Context, action Action) (Action, error)
	GetAction(ctx context.Context, id int64) (Action, error)
	ListActions(ctx context.Context) ([]Action, error)
	TransitionAction(ctx context.Context, id int64, status ActionStatus, mutate func(*Action) error) (Action, error)
	CreateActionEvent(ctx context.Context, event ActionEvent) (ActionEvent, error)
	ListActionEvents(ctx context.Context) ([]ActionEvent, error)
}
