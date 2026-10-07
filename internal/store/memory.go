package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"sort"
	"sync"
	"time"

	"github.com/consize-oss/consize/pkg/resource"
)

var ErrNotFound = errors.New("not found")

type Memory struct {
	path                    string
	lockFile                *os.File
	poison                  error
	diagnostics             StorageDiagnostics
	jobs                    map[int64]Job
	mu                      sync.RWMutex
	now                     func() time.Time
	resources               map[string]resource.Resource
	recs                    map[int64]Recommendation
	actionRecords           map[int64]Action
	actions                 map[int64]ActionEvent
	nextActionID            int64
	nextEventID             int64
	nextRecID               int64
	resourceIdentityIndex   map[string]string
	actionIdempotencyIndex  map[string]int64
	actionsByRecommendation map[int64][]int64
	activeJobByResource     map[string]int64
}

func NewMemory() *Memory {
	return &Memory{
		jobs:                    map[int64]Job{},
		now:                     time.Now,
		resources:               map[string]resource.Resource{},
		recs:                    map[int64]Recommendation{},
		actionRecords:           map[int64]Action{},
		actions:                 map[int64]ActionEvent{},
		nextActionID:            1,
		nextEventID:             1,
		nextRecID:               1,
		resourceIdentityIndex:   map[string]string{},
		actionIdempotencyIndex:  map[string]int64{},
		actionsByRecommendation: map[int64][]int64{},
		activeJobByResource:     map[string]int64{},
		diagnostics:             StorageDiagnostics{Backend: "memory", CurrentVersion: currentStateVersion, TargetVersion: currentStateVersion, MigrationStatus: MigrationCurrent, Indexes: RequiredIndexes()},
	}
}

func (m *Memory) Health(context.Context) error { m.mu.RLock(); defer m.mu.RUnlock(); return m.poison }

func (m *Memory) StorageDiagnostics(context.Context) StorageDiagnostics {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := m.diagnostics
	out.Healthy = m.poison == nil
	if m.poison != nil {
		out.Error = m.poison.Error()
	}
	out.EntityCounts = map[string]int{
		"resources": len(m.resources), "recommendations": len(m.recs), "actions": len(m.actionRecords),
		"audit_events": len(m.actions), "jobs": len(m.jobs),
	}
	return out
}

func (m *Memory) UpsertResource(_ context.Context, res resource.Resource) (resource.Resource, error) {
	now := m.now().UTC()
	normalized, err := res.Normalize(now)
	if err != nil {
		return resource.Resource{}, fmt.Errorf("normalize resource: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	identity := resourceIdentityKey(normalized)
	if id, exists := m.resourceIdentityIndex[identity]; exists && id != normalized.ID {
		if !resource.IsCanonicalID(id) && resource.IsCanonicalID(normalized.ID) {
			// Preserve references created before canonical IDs were introduced.
			normalized.ID = id
		} else {
			return resource.Resource{}, fmt.Errorf("%w: identity already registered as %q", resource.ErrIdentityConflict, id)
		}
	}

	if existing, ok := m.resources[normalized.ID]; ok {
		if !resource.SameIdentity(existing, normalized) {
			return resource.Resource{}, fmt.Errorf("%w: id %q belongs to a different provider resource", resource.ErrIdentityConflict, normalized.ID)
		}
		if normalized.ObservedAt.Before(existing.LastSeenAt) {
			return resource.Resource{}, fmt.Errorf("stale resource observation: observed_at %s precedes last_seen_at %s", normalized.ObservedAt, existing.LastSeenAt)
		}
		if !resource.ValidLifecycleTransition(existing.LifecycleState, normalized.LifecycleState) {
			return resource.Resource{}, fmt.Errorf("invalid resource lifecycle transition from %q to %q", existing.LifecycleState, normalized.LifecycleState)
		}
		normalized.FirstSeenAt = existing.FirstSeenAt
		normalized.CreatedAt = existing.CreatedAt
	}
	normalized.LastSeenAt = normalized.ObservedAt
	normalized.UpdatedAt = now
	if err := normalized.Validate(); err != nil {
		return resource.Resource{}, fmt.Errorf("validate resource update: %w", err)
	}
	m.resources[normalized.ID] = clone(normalized)
	m.resourceIdentityIndex[identity] = normalized.ID
	return clone(normalized), m.persistLocked()
}

func (m *Memory) GetResource(_ context.Context, id string) (resource.Resource, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	res, ok := m.resources[id]
	if !ok {
		return resource.Resource{}, ErrNotFound
	}
	return clone(res), nil
}

func (m *Memory) ListResources(context.Context) ([]resource.Resource, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]resource.Resource, 0, len(m.resources))
	for _, res := range m.resources {
		out = append(out, res)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return clone(out), nil
}

func (m *Memory) CreateRecommendation(_ context.Context, rec Recommendation) (Recommendation, error) {
	if rec.ResourceID == "" || rec.PluginID == "" || rec.ActionType == "" || rec.AlgorithmID == "" || len(rec.EvidenceRefs) == 0 {
		return Recommendation{}, errors.New("resource_id, plugin_id, action_type, algorithm_id, and evidence_refs are required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.resources[rec.ResourceID]; !ok {
		return Recommendation{}, errors.New("recommendation resource does not exist")
	}
	now := m.now().UTC()
	if !rec.ExpiresAt.IsZero() && !rec.ExpiresAt.After(now) {
		return Recommendation{}, errors.New("recommendation expiry must be in the future")
	}
	rec.ID = m.nextRecID
	m.nextRecID++
	rec.SchemaVersion = ContractVersion
	if rec.AlgorithmVersion == "" {
		rec.AlgorithmVersion = "1"
	}
	if rec.RecommendationType == "" {
		rec.RecommendationType = rec.ActionType
	}
	if rec.Current == nil {
		rec.Current = map[string]any{}
	}
	if rec.Proposed == nil {
		rec.Proposed = map[string]any{}
	}
	if rec.Parameters == nil {
		rec.Parameters = map[string]any{}
	}
	if rec.Status == "" {
		rec.Status = RecommendationPending
	}
	if rec.Status != RecommendationPending {
		return Recommendation{}, errors.New("new recommendation must start pending")
	}
	if rec.SavingsEstimate.Classification == "" {
		rec.SavingsEstimate = SavingsEstimate{Classification: SavingsEstimated, AmountMonthly: rec.EstimatedSavingsMonthly, CalculatedAt: now}
	}
	rec.CreatedAt = now
	rec.UpdatedAt = now
	m.recs[rec.ID] = clone(rec)
	return clone(rec), m.persistLocked()
}

func (m *Memory) GetRecommendation(_ context.Context, id int64) (Recommendation, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	rec, ok := m.recs[id]
	if !ok {
		return Recommendation{}, ErrNotFound
	}
	return clone(rec), nil
}

func (m *Memory) ListRecommendations(context.Context) ([]Recommendation, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Recommendation, 0, len(m.recs))
	for _, rec := range m.recs {
		out = append(out, rec)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return clone(out), nil
}

func (m *Memory) TransitionRecommendation(_ context.Context, id int64, status RecommendationStatus, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.recs[id]
	if !ok {
		return ErrNotFound
	}
	if !ValidRecommendationTransition(rec.Status, status) {
		return fmt.Errorf("%w: recommendation %s -> %s", ErrInvalidTransition, rec.Status, status)
	}
	rec.Status = status
	rec.StatusReason = reason
	rec.UpdatedAt = m.now().UTC()
	m.recs[id] = rec
	return m.persistLocked()
}

func (m *Memory) SupersedeRecommendation(_ context.Context, id, replacementID int64, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.recs[id]
	if !ok {
		return ErrNotFound
	}
	replacement, ok := m.recs[replacementID]
	if !ok {
		return errors.New("replacement recommendation does not exist")
	}
	if rec.ResourceID != replacement.ResourceID || id == replacementID {
		return errors.New("replacement must be a different recommendation for the same resource")
	}
	if !ValidRecommendationTransition(rec.Status, RecommendationSuperseded) {
		return fmt.Errorf("%w: recommendation %s -> %s", ErrInvalidTransition, rec.Status, RecommendationSuperseded)
	}
	rec.Status = RecommendationSuperseded
	rec.SupersededBy = replacementID
	rec.StatusReason = reason
	rec.UpdatedAt = m.now().UTC()
	m.recs[id] = rec
	return m.persistLocked()
}

func (m *Memory) CreateAction(_ context.Context, action Action) (Action, error) {
	if err := action.Validate(); err != nil {
		return Action{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.recs[action.RecommendationID]
	if !ok {
		return Action{}, ErrNotFound
	}
	now := m.now().UTC()
	if rec.Status == RecommendationExpired || rec.Status == RecommendationSuperseded || (!rec.ExpiresAt.IsZero() && !rec.ExpiresAt.After(now)) {
		return Action{}, ErrRecommendationGone
	}
	if rec.ResourceID != action.ResourceID || rec.PluginID != action.PluginID || rec.ActionType != action.ActionType {
		return Action{}, errors.New("action does not match its recommendation")
	}
	if existingID, ok := m.actionIdempotencyIndex[action.IdempotencyKey]; ok {
		existing := m.actionRecords[existingID]
		if existing.RecommendationID != action.RecommendationID || existing.ResourceID != action.ResourceID || existing.PluginID != action.PluginID || existing.ActionType != action.ActionType || existing.Mode != action.Mode {
			return Action{}, errors.New("idempotency key is already bound to a different action request")
		}
		return clone(existing), nil
	}
	action.SchemaVersion = ContractVersion
	action.ID = m.nextActionID
	m.nextActionID++
	action.Status = ActionRequested
	action.RequestedAt = now
	action.UpdatedAt = now
	if action.Parameters == nil {
		action.Parameters = map[string]any{}
	}
	m.actionRecords[action.ID] = clone(action)
	m.actionIdempotencyIndex[action.IdempotencyKey] = action.ID
	m.actionsByRecommendation[action.RecommendationID] = append(m.actionsByRecommendation[action.RecommendationID], action.ID)
	return clone(action), m.persistLocked()
}

func (m *Memory) GetAction(_ context.Context, id int64) (Action, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	action, ok := m.actionRecords[id]
	if !ok {
		return Action{}, ErrNotFound
	}
	return clone(action), nil
}

func (m *Memory) ListActions(context.Context) ([]Action, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Action, 0, len(m.actionRecords))
	for _, action := range m.actionRecords {
		out = append(out, clone(action))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (m *Memory) TransitionAction(_ context.Context, id int64, status ActionStatus, mutate func(*Action) error) (Action, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	action, ok := m.actionRecords[id]
	if !ok {
		return Action{}, ErrNotFound
	}
	if !ValidActionTransition(action.Status, status) {
		return Action{}, fmt.Errorf("%w: action %s -> %s", ErrInvalidTransition, action.Status, status)
	}
	original := clone(action)
	if mutate != nil {
		if err := mutate(&action); err != nil {
			return Action{}, err
		}
	}
	if action.RecommendationID != original.RecommendationID || action.ResourceID != original.ResourceID || action.RemediationPath != original.RemediationPath || action.PluginID != original.PluginID || action.PluginVersion != original.PluginVersion || action.ActionType != original.ActionType || action.Mode != original.Mode || action.IdempotencyKey != original.IdempotencyKey || action.RequestedBy != original.RequestedBy || !reflect.DeepEqual(action.Parameters, original.Parameters) || !reflect.DeepEqual(action.PolicyDecision, original.PolicyDecision) {
		return Action{}, errors.New("immutable action fields cannot be changed")
	}
	now := m.now().UTC()
	action.Status = status
	action.UpdatedAt = now
	if status == ActionApproved {
		action.ApprovedAt = now
		if action.ApprovedBy == "" {
			action.ApprovedBy = action.RequestedBy
		}
	}
	if status == ActionExecuting && action.StartedAt.IsZero() {
		action.StartedAt = now
	}
	if ActionTerminal(status) {
		action.FinishedAt = now
	}
	m.actionRecords[id] = clone(action)
	return clone(action), m.persistLocked()
}

func (m *Memory) CreateActionEvent(_ context.Context, event ActionEvent) (ActionEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if event.RecommendationID > 0 {
		if _, ok := m.recs[event.RecommendationID]; !ok {
			return ActionEvent{}, errors.New("audit recommendation does not exist")
		}
	}
	if event.ActionID > 0 {
		action, ok := m.actionRecords[event.ActionID]
		if !ok || (event.RecommendationID > 0 && action.RecommendationID != event.RecommendationID) {
			return ActionEvent{}, errors.New("audit action does not exist or does not match its recommendation")
		}
	}
	event.ID = m.nextEventID
	m.nextEventID++
	event.CreatedAt = m.now().UTC()
	if event.Parameters == nil {
		event.Parameters = map[string]any{}
	}
	m.actions[event.ID] = clone(event)
	return clone(event), m.persistLocked()
}

func (m *Memory) ListActionEvents(context.Context) ([]ActionEvent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]ActionEvent, 0, len(m.actions))
	for _, event := range m.actions {
		out = append(out, event)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return clone(out), nil
}
