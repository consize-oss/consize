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

	"github.com/consize-oss/consize/internal/audit"
	"github.com/consize-oss/consize/pkg/resource"
)

var ErrNotFound = resource.ErrNotFound

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
	auditEvents             map[string]audit.Event
	auditIdempotencyIndex   map[string]string
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
		auditEvents:             map[string]audit.Event{},
		auditIdempotencyIndex:   map[string]string{},
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
		"action_events": len(m.actions), "audit_events": len(m.auditEvents), "jobs": len(m.jobs),
	}
	return out
}

func (m *Memory) CreateResource(ctx context.Context, res resource.Resource) (resource.Resource, error) {
	return m.putResource(ctx, res, true)
}
func (m *Memory) UpsertResource(ctx context.Context, res resource.Resource) (resource.Resource, error) {
	return m.putResource(ctx, res, false)
}
func (m *Memory) putResource(ctx context.Context, res resource.Resource, createOnly bool) (resource.Resource, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return resource.Resource{}, err
	}
	now := m.now().UTC()
	normalized, err := normalizeObservation(res, now)
	if err != nil {
		return resource.Resource{}, err
	}
	if prior, ok := m.resources[normalized.ID]; ok && !resource.SameIdentity(prior, normalized) {
		return resource.Resource{}, resource.ErrIdentityConflict
	}
	identity := resourceIdentityKey(normalized)
	id := normalized.ID
	if existingID, ok := m.resourceIdentityIndex[identity]; ok {
		id = existingID
	}
	var existing *resource.Resource
	previous, ok := m.resources[id]
	if ok {
		existing = &previous
	}
	normalized, err = prepareResource(normalized, existing, now, createOnly)
	if err != nil {
		return resource.Resource{}, err
	}
	m.resources[normalized.ID] = clone(normalized)
	m.resourceIdentityIndex[identity] = normalized.ID
	if err = m.persistLocked(); err != nil {
		if existing != nil {
			m.resources[id] = previous
		} else {
			delete(m.resources, normalized.ID)
			delete(m.resourceIdentityIndex, identity)
		}
		return resource.Resource{}, err
	}
	return clone(normalized), nil
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
	if err := validateRecommendationEvidence(rec); err != nil {
		return Recommendation{}, err
	}
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
	if err := m.persistLocked(); err != nil {
		delete(m.recs, rec.ID)
		m.nextRecID--
		return Recommendation{}, err
	}
	return clone(rec), nil
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

func (m *Memory) CreateAction(ctx context.Context, action Action) (Action, error) {
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
	if action.CorrelationID == "" {
		action.CorrelationID = audit.CorrelationFromContext(ctx)
	}
	if action.CorrelationID == "" {
		action.CorrelationID = fmt.Sprintf("action-%d", action.ID)
	}
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
	if action.RecommendationID != original.RecommendationID || action.ResourceID != original.ResourceID || action.RemediationPath != original.RemediationPath || action.PluginID != original.PluginID || action.PluginVersion != original.PluginVersion || action.ActionType != original.ActionType || action.Mode != original.Mode || action.IdempotencyKey != original.IdempotencyKey || action.CorrelationID != original.CorrelationID || action.RequestedBy != original.RequestedBy || !reflect.DeepEqual(action.Parameters, original.Parameters) || !reflect.DeepEqual(action.PolicyDecision, original.PolicyDecision) {
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

func (m *Memory) CreateActionEvent(ctx context.Context, event ActionEvent) (ActionEvent, error) {
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
	previousID := ""
	correlationID := audit.CorrelationFromContext(ctx)
	if action, ok := m.actionRecords[event.ActionID]; ok && action.CorrelationID != "" {
		correlationID = action.CorrelationID
	}
	if correlationID == "" {
		correlationID = fmt.Sprintf("action-%d", event.ActionID)
	}
	for id, candidate := range m.auditEvents {
		if candidate.CorrelationID == correlationID && (previousID == "" || candidate.RecordedAt.After(m.auditEvents[previousID].RecordedAt)) {
			previousID = id
		}
	}
	auditEvent := canonicalActionAudit(event, correlationID, previousID)
	if err := auditEvent.Validate(); err != nil {
		delete(m.actions, event.ID)
		m.nextEventID--
		return ActionEvent{}, err
	}
	m.auditEvents[auditEvent.ID] = clone(auditEvent)
	m.auditIdempotencyIndex[auditEvent.IdempotencyKey] = auditEvent.ID
	if err := m.persistLocked(); err != nil {
		delete(m.actions, event.ID)
		delete(m.auditEvents, auditEvent.ID)
		delete(m.auditIdempotencyIndex, auditEvent.IdempotencyKey)
		m.nextEventID--
		return ActionEvent{}, err
	}
	return clone(event), nil
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
