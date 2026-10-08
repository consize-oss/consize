package store

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/consize-oss/consize/internal/bootstrap"
	"github.com/consize-oss/consize/internal/policy"
	"github.com/consize-oss/consize/internal/verification"
	"github.com/consize-oss/consize/pkg/plugin"
	"github.com/consize-oss/consize/pkg/resource"
)

type Job struct {
	Result           *verification.Result         `json:"verification_result,omitempty"`
	PluginResult     *plugin.ActionResult         `json:"plugin_result,omitempty"`
	ID               int64                        `json:"id"`
	ActionID         int64                        `json:"action_id"`
	RecommendationID int64                        `json:"recommendation_id"`
	Resource         resource.Resource            `json:"resource"`
	Plan             plugin.ActionPlan            `json:"plan"`
	Baseline         plugin.MetricsSnapshot       `json:"baseline"`
	Verification     bootstrap.VerificationConfig `json:"verification"`
	Policy           policy.Decision              `json:"policy"`
	Actor            string                       `json:"actor"`
	State            string                       `json:"state"`
	Attempts         int                          `json:"attempts"`
	NextRun          time.Time                    `json:"next_run"`
	WindowStart      time.Time                    `json:"window_start"`
	Deadline         time.Time                    `json:"deadline"`
	LastError        string                       `json:"last_error,omitempty"`
	UpdatedAt        time.Time                    `json:"updated_at"`
}

func Terminal(state string) bool {
	return state == "verified" || state == "rolled_back" || state == "manual_intervention" || state == "cancelled"
}

type JobStore interface {
	Store
	Durable() bool
	CreateJob(context.Context, Job) (Job, error)
	SaveJob(context.Context, Job, string) error
	ListJobs(context.Context) ([]Job, error)
}

func (m *Memory) CreateJob(_ context.Context, job Job) (Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.poison != nil {
		return Job{}, m.poison
	}
	if existing, ok := m.jobs[job.ID]; ok {
		return clone(existing), nil
	}
	if job.RecommendationID == 0 {
		job.RecommendationID = job.ID
	}
	rec, ok := m.recs[job.RecommendationID]
	if !ok {
		return Job{}, ErrNotFound
	}
	if rec.Status != RecommendationPending && rec.Status != RecommendationPlanned {
		return Job{}, errors.New("recommendation is not actionable")
	}
	action, ok := m.actionRecords[job.ActionID]
	if !ok || action.RecommendationID != job.RecommendationID {
		return Job{}, errors.New("job action does not exist or belongs to another recommendation")
	}
	if job.Resource.ID != rec.ResourceID || job.Plan.ResourceID != rec.ResourceID || job.Plan.PluginID != rec.PluginID || job.Plan.ActionType != rec.ActionType {
		return Job{}, errors.New("job resource or plan does not match its recommendation")
	}
	if _, exists := m.activeJobByResource[job.Resource.ID]; exists {
		return Job{}, errors.New("resource already has an active or unresolved action")
	}
	job.State = "prepared"
	job.UpdatedAt = m.now().UTC()
	job.NextRun = job.UpdatedAt
	m.jobs[job.ID] = clone(job)
	m.activeJobByResource[job.Resource.ID] = job.ID
	m.jobEventLocked(job, "Durable action prepared before mutation")
	return clone(job), m.persistLocked()
}

func (m *Memory) SaveJob(_ context.Context, job Job, message string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.poison != nil {
		return m.poison
	}
	if _, ok := m.jobs[job.ID]; !ok {
		return ErrNotFound
	}
	existing := m.jobs[job.ID]
	if job.ID != existing.ID || job.ActionID != existing.ActionID || job.RecommendationID != existing.RecommendationID || job.Resource.ID != existing.Resource.ID {
		return errors.New("immutable job identity cannot be changed")
	}
	job.UpdatedAt = m.now().UTC()
	m.jobs[job.ID] = clone(job)
	if Terminal(job.State) && job.State != "manual_intervention" {
		delete(m.activeJobByResource, job.Resource.ID)
	} else {
		m.activeJobByResource[job.Resource.ID] = job.ID
	}
	m.jobEventLocked(job, message)
	return m.persistLocked()
}

func (m *Memory) jobEventLocked(job Job, message string) {
	event := ActionEvent{ID: m.nextEventID, ActionID: job.ActionID, RecommendationID: job.RecommendationID, ResourceID: job.Resource.ID, PluginID: job.Plan.PluginID, Actor: job.Actor, Mode: "approved", Result: job.State, Message: message, PolicyDecision: job.Policy, CreatedAt: m.now().UTC(), Plan: &job.Plan}
	event.VerificationResult = job.Result
	m.actions[event.ID] = clone(event)
	m.nextEventID++
	rec := m.recs[job.RecommendationID]
	if next := recommendationStatusForJob(job.State); next != "" && ValidRecommendationTransition(rec.Status, next) {
		rec.Status = next
	}
	rec.PolicyID = job.Policy.PolicyID
	rec.UpdatedAt = m.now().UTC()
	m.recs[job.RecommendationID] = rec
	if action, ok := m.actionRecords[job.ActionID]; ok {
		if next := actionStatusForJob(job.State); next != "" && ValidActionTransition(action.Status, next) {
			action.Status = next
			action.UpdatedAt = m.now().UTC()
			if next == ActionExecuting && action.StartedAt.IsZero() {
				action.StartedAt = action.UpdatedAt
			}
			if ActionTerminal(next) {
				action.FinishedAt = action.UpdatedAt
			}
			if next == ActionFailed || next == ActionManualIntervention {
				action.FailureMessage = message
			}
			if job.PluginResult != nil {
				action.ExecutionResult = &ExecutionResult{Result: clone(*job.PluginResult), StartedAt: action.StartedAt, FinishedAt: action.FinishedAt}
			}
			m.actionRecords[action.ID] = action
		}
	}
	if job.State == "waiting_rollout" || job.State == "verified" || job.State == "rollback_verifying" || job.State == "rolled_back" {
		res := m.resources[job.Resource.ID]
		if job.State == "rolled_back" || job.State == "rollback_verifying" {
			res.CurrentState = clone(job.Plan.OriginalState)
		} else {
			res.CurrentState = clone(job.Plan.AppliedState)
		}
		m.resources[res.ID] = res
	}
}

func recommendationStatusForJob(state string) RecommendationStatus {
	switch state {
	case "prepared":
		return RecommendationApproved
	case "applying", "waiting_rollout", "verifying", "rollback_pending", "rolling_back", "rollback_verifying":
		return RecommendationExecuting
	case "verified":
		return RecommendationVerified
	case "rolled_back":
		return RecommendationRolledBack
	case "manual_intervention":
		return RecommendationManualIntervention
	case "cancelled":
		return RecommendationRejected
	default:
		return ""
	}
}

func actionStatusForJob(state string) ActionStatus {
	switch state {
	case "prepared":
		return ActionApproved
	case "applying", "waiting_rollout":
		return ActionExecuting
	case "verifying":
		return ActionVerifying
	case "rollback_pending":
		return ActionRollbackPending
	case "rolling_back", "rollback_verifying":
		return ActionRollingBack
	case "verified":
		return ActionSucceeded
	case "rolled_back":
		return ActionRolledBack
	case "manual_intervention":
		return ActionManualIntervention
	case "cancelled":
		return ActionCancelled
	default:
		return ""
	}
}

func (m *Memory) ListJobs(context.Context) ([]Job, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.poison != nil {
		return nil, m.poison
	}
	out := make([]Job, 0, len(m.jobs))
	for _, job := range m.jobs {
		out = append(out, clone(job))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
