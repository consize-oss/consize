package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/consize-oss/consize/internal/audit"
	"github.com/consize-oss/consize/pkg/evidence"
	"github.com/consize-oss/consize/pkg/evidence/quality"
	"github.com/consize-oss/consize/pkg/plugin"
	"github.com/consize-oss/consize/pkg/resource"
)

// Collections are read-only operations, independent of infrastructure action jobs.
type Collection struct {
	ID             string                  `json:"id"`
	Resource       resource.Resource       `json:"resource"`
	PluginID       string                  `json:"plugin_id"`
	InstanceDigest string                  `json:"instance_digest"`
	Signals        []string                `json:"signals"`
	WindowStart    time.Time               `json:"window_start"`
	WindowEnd      time.Time               `json:"window_end"`
	Actor          string                  `json:"actor"`
	Trigger        string                  `json:"trigger"`
	State          string                  `json:"state"`
	Attempts       int                     `json:"attempts"`
	MaxAttempts    int                     `json:"max_attempts"`
	Lease          *JobLease               `json:"lease,omitempty"`
	Fence          uint64                  `json:"fence"`
	NextRun        time.Time               `json:"next_run"`
	CreatedAt      time.Time               `json:"created_at"`
	UpdatedAt      time.Time               `json:"updated_at"`
	FinishedAt     time.Time               `json:"finished_at,omitempty"`
	ErrorCode      string                  `json:"error_code,omitempty"`
	History        []CollectionAttempt     `json:"history"`
	Snapshot       *plugin.MetricsSnapshot `json:"snapshot,omitempty"`
	Assessment     *quality.Assessment     `json:"assessment,omitempty"`
	Pruned         bool                    `json:"pruned"`
}
type CollectionAttempt struct {
	Number     int       `json:"number"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at,omitempty"`
	Outcome    string    `json:"outcome,omitempty"`
}
type CollectionResult struct {
	Snapshot   *plugin.MetricsSnapshot
	Assessment *quality.Assessment
	ErrorCode  string
	RetryAt    time.Time
}
type CollectionQuery struct {
	ResourceID string
	Due        bool
	Limit      int
}
type CollectionStore interface {
	EnqueueCollection(context.Context, Collection) (Collection, error)
	GetCollection(context.Context, string) (Collection, error)
	ListCollections(context.Context, CollectionQuery) ([]Collection, error)
	ClaimCollection(context.Context, string, string, time.Duration) (Collection, error)
	FinishCollection(context.Context, Collection, CollectionResult) (Collection, error)
	PruneCollections(context.Context, time.Time) (int, error)
}

var ErrCollectionOverlap = errors.New("an overlapping collection is pending")

func CollectionTerminal(state string) bool {
	return state == "succeeded" || state == "degraded" || state == "failed"
}
func CollectionID(c Collection) string {
	// Bind the discovered incarnation and mapping shape, but not rediscovery timestamps.
	raw, _ := json.Marshal([]any{"collection-v1", ResourceCollectionScope(c.Resource), c.PluginID, c.InstanceDigest, c.Signals, c.WindowStart.UTC(), c.WindowEnd.UTC()})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func prepareCollection(c Collection, now time.Time) (Collection, error) {
	if c.Resource.Validate() != nil || c.Resource.Type != resource.TypeKubernetesDeployment || c.PluginID == "" || len(c.PluginID) > 128 || len(c.InstanceDigest) != 64 || c.Actor == "" || len(c.Actor) > 256 || (c.Trigger != "manual" && c.Trigger != "scheduled") || c.MaxAttempts < 1 || c.MaxAttempts > 10 || c.WindowStart.IsZero() || c.WindowStart.Year() < 1 || c.WindowEnd.Year() > 9999 || !c.WindowEnd.After(c.WindowStart) || c.WindowEnd.After(now) || c.WindowEnd.Sub(c.WindowStart) > 365*24*time.Hour || c.WindowStart.Nanosecond()%1000000 != 0 || c.WindowEnd.Nanosecond()%1000000 != 0 || len(c.Signals) == 0 || len(c.Signals) > 256 {
		return Collection{}, errors.New("invalid collection request")
	}
	if _, err := hex.DecodeString(c.InstanceDigest); err != nil {
		return Collection{}, errors.New("invalid collection instance")
	}
	c.Signals = append([]string(nil), c.Signals...)
	sort.Strings(c.Signals)
	for i, s := range c.Signals {
		if s == "" || len(s) > 128 || i > 0 && s == c.Signals[i-1] {
			return Collection{}, errors.New("invalid collection signals")
		}
	}
	c.ID = CollectionID(c)
	c.State = "pending"
	c.Attempts = 0
	c.Lease = nil
	c.Fence = 0
	c.History = []CollectionAttempt{}
	c.Snapshot = nil
	c.Assessment = nil
	c.ErrorCode = ""
	c.FinishedAt = time.Time{}
	c.Pruned = false
	c.CreatedAt = now
	c.UpdatedAt = now
	if c.NextRun.IsZero() || c.NextRun.Before(now) {
		c.NextRun = now
	}
	if c.NextRun.After(now.Add(24 * time.Hour)) {
		return Collection{}, errors.New("invalid collection due time")
	}
	return c, nil
}
func overlappingCollection(a, b Collection) bool {
	return !CollectionTerminal(a.State) && a.Resource.ID == b.Resource.ID && ResourceCollectionScope(a.Resource) == ResourceCollectionScope(b.Resource) && a.PluginID == b.PluginID && a.InstanceDigest == b.InstanceDigest && a.WindowStart.Before(b.WindowEnd) && b.WindowStart.Before(a.WindowEnd)
}
func claimCollection(c Collection, owner string, ttl time.Duration, now time.Time) (Collection, error) {
	if err := validateLeaseOptions(owner, ttl); err != nil {
		return Collection{}, err
	}
	if CollectionTerminal(c.State) || c.NextRun.After(now) {
		return Collection{}, ErrJobChanged
	}
	if c.Lease != nil && now.Before(c.Lease.Until) {
		return Collection{}, ErrJobBusy
	}
	if len(c.History) > 0 && c.History[len(c.History)-1].FinishedAt.IsZero() {
		c.History[len(c.History)-1].FinishedAt = now
		c.History[len(c.History)-1].Outcome = "interrupted"
	}
	if c.Attempts >= c.MaxAttempts {
		c.State = "failed"
		c.ErrorCode = "attempts_exhausted"
		c.FinishedAt = now
		c.Lease = nil
		c.UpdatedAt = now
		return c, nil
	}
	c.Fence++
	c.Attempts++
	c.Lease = &JobLease{Owner: owner, Epoch: c.Fence, Until: now.Add(ttl)}
	c.State = "running"
	c.UpdatedAt = now
	c.History = append(c.History, CollectionAttempt{Number: c.Attempts, StartedAt: now})
	return c, nil
}
func finishCollection(existing, candidate Collection, result CollectionResult, now time.Time) (Collection, error) {
	if existing.Lease == nil || candidate.Lease == nil || existing.State != "running" || existing.Lease.Owner != candidate.Lease.Owner || existing.Lease.Epoch != candidate.Lease.Epoch || !now.Before(existing.Lease.Until) {
		return Collection{}, ErrLeaseLost
	}
	c := existing
	if result.Snapshot != nil {
		s := result.Snapshot
		raw, marshalErr := json.Marshal(s)
		if marshalErr != nil || len(raw) > 4<<20 {
			return Collection{}, errors.New("collection evidence exceeds storage limit")
		}
		if result.ErrorCode != "" || s.ValidateRecords() != nil || s.ResourceID != c.Resource.ID || s.PluginID != c.PluginID || result.Assessment == nil || result.Assessment.ResourceID != c.Resource.ID || result.Assessment.PluginID != c.PluginID {
			return Collection{}, errors.New("invalid collection evidence")
		}
		for _, r := range s.Records {
			if !r.WindowStart.Equal(c.WindowStart) || !r.WindowEnd.Equal(c.WindowEnd) {
				return Collection{}, errors.New("collection evidence window mismatch")
			}
		}
		seen := map[string]bool{}
		complete := result.Assessment.Eligible
		for _, r := range s.Records {
			seen[r.Signal] = true
			if r.Quality != evidence.Valid || r.Value == nil {
				complete = false
			}
		}
		for _, key := range c.Signals {
			if !seen[key] {
				complete = false
			}
		}
		c.State = "degraded"
		if complete {
			c.State = "succeeded"
		}
		c.Snapshot = s
		c.Assessment = result.Assessment
		c.ErrorCode = ""
		c.FinishedAt = now
	} else {
		// Only a closed set of classifications may reach persistence or public output.
		if !collectionErrorCode(result.ErrorCode) {
			return Collection{}, errors.New("invalid collection error classification")
		}
		c.ErrorCode = result.ErrorCode
		c.State = "failed"
		c.FinishedAt = now
		if !result.RetryAt.IsZero() && c.Attempts < c.MaxAttempts {
			if !result.RetryAt.After(now) || result.RetryAt.After(now.Add(time.Hour)) {
				return Collection{}, errors.New("invalid collection retry time")
			}
			c.State = "retry_wait"
			c.NextRun = result.RetryAt
			c.FinishedAt = time.Time{}
		}
	}
	c.Lease = nil
	c.UpdatedAt = now
	n := len(c.History) - 1
	c.History[n].FinishedAt = now
	c.History[n].Outcome = c.State
	if c.ErrorCode != "" {
		c.History[n].Outcome = c.ErrorCode
	}
	return c, nil
}
func collectionErrorCode(code string) bool {
	switch code {
	case "provider_unavailable", "provider_timeout", "provider_query", "provider_access", "provider_configuration", "provider_failure", "invalid_evidence", "instance_changed", "resource_unavailable", "attempts_exhausted":
		return true
	}
	return false
}
func collectionEvent(c Collection) audit.Event {
	id := fmt.Sprintf("collection-%s-%d-%s", c.ID, c.Fence, c.State)
	result := audit.ResultPending
	severity := audit.SeverityInfo
	if c.State == "succeeded" {
		result = audit.ResultSucceeded
	}
	if c.State == "failed" {
		result = audit.ResultFailed
		severity = audit.SeverityError
	}
	if c.State == "degraded" {
		result = audit.ResultInconclusive
		severity = audit.SeverityWarning
	}
	return audit.Event{ID: id, IdempotencyKey: id, Type: "evidence.collection." + c.State, Family: audit.FamilyEvidence, Kind: audit.KindObservation, SchemaVersion: audit.SchemaVersion, Timestamp: c.UpdatedAt, RecordedAt: c.UpdatedAt, Actor: audit.Actor{Type: "actor", ID: c.Actor}, Source: "evidence-collection", Resource: &audit.ResourceRef{ID: c.Resource.ID}, CorrelationID: c.ID, Workflow: audit.WorkflowRefs{PluginID: c.PluginID}, Result: result, Severity: severity, Visibility: audit.VisibilitySummary, ReasonCode: "collection_" + c.State, Summary: "Evidence collection state changed", Details: map[string]any{"operation_id": c.ID, "attempt": c.Attempts, "state": c.State, "error_code": c.ErrorCode}}
}
func collectionLimit(n int) int {
	if n <= 0 {
		return 100
	}
	if n > 500 {
		return 500
	}
	return n
}
func collectionDue(c Collection, now time.Time) bool {
	return !CollectionTerminal(c.State) && !c.NextRun.After(now) && (c.Lease == nil || !now.Before(c.Lease.Until))
}
func (m *Memory) writeCollectionLocked(c Collection) (Collection, error) {
	old, exists := m.collections[c.ID]
	e := collectionEvent(c)
	if err := e.Validate(); err != nil {
		return Collection{}, err
	}
	m.collections[c.ID] = clone(c)
	m.auditEvents[e.ID] = e
	m.auditIdempotencyIndex[e.IdempotencyKey] = e.ID
	if err := m.persistLocked(); err != nil {
		if exists {
			m.collections[c.ID] = old
		} else {
			delete(m.collections, c.ID)
		}
		delete(m.auditEvents, e.ID)
		delete(m.auditIdempotencyIndex, e.IdempotencyKey)
		return Collection{}, err
	}
	return clone(c), nil
}
func (m *Memory) EnqueueCollection(ctx context.Context, c Collection) (Collection, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Collection{}, err
	}
	if m.poison != nil {
		return Collection{}, m.poison
	}
	c, err := prepareCollection(c, m.now().UTC())
	if err != nil {
		return Collection{}, err
	}
	if prior, ok := m.collections[c.ID]; ok {
		return clone(prior), nil
	}
	if _, ok := m.resources[c.Resource.ID]; !ok {
		return Collection{}, ErrNotFound
	}
	for _, prior := range m.collections {
		if overlappingCollection(prior, c) {
			return Collection{}, ErrCollectionOverlap
		}
	}
	return m.writeCollectionLocked(c)
}
func (m *Memory) GetCollection(ctx context.Context, id string) (Collection, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if err := ctx.Err(); err != nil {
		return Collection{}, err
	}
	if m.poison != nil {
		return Collection{}, m.poison
	}
	c, ok := m.collections[id]
	if !ok {
		return Collection{}, ErrNotFound
	}
	return clone(c), nil
}
func (m *Memory) ListCollections(ctx context.Context, q CollectionQuery) ([]Collection, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if m.poison != nil {
		return nil, m.poison
	}
	out := []Collection{}
	for _, c := range m.collections {
		if (q.ResourceID == "" || q.ResourceID == c.Resource.ID) && (!q.Due || collectionDue(c, m.now())) {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if q.Due && !out[i].NextRun.Equal(out[j].NextRun) {
			return out[i].NextRun.Before(out[j].NextRun)
		}
		if !q.Due && !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	if len(out) > collectionLimit(q.Limit) {
		out = out[:collectionLimit(q.Limit)]
	}
	return clone(out), nil
}
func (m *Memory) ClaimCollection(ctx context.Context, id, owner string, ttl time.Duration) (Collection, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Collection{}, err
	}
	if m.poison != nil {
		return Collection{}, m.poison
	}
	c, ok := m.collections[id]
	if !ok {
		return Collection{}, ErrNotFound
	}
	c, err := claimCollection(clone(c), owner, ttl, m.now().UTC())
	if err != nil {
		return Collection{}, err
	}
	return m.writeCollectionLocked(c)
}
func (m *Memory) FinishCollection(ctx context.Context, c Collection, r CollectionResult) (Collection, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Collection{}, err
	}
	if m.poison != nil {
		return Collection{}, m.poison
	}
	prior, ok := m.collections[c.ID]
	if !ok {
		return Collection{}, ErrNotFound
	}
	next, err := finishCollection(clone(prior), c, r, m.now().UTC())
	if err != nil {
		return Collection{}, err
	}
	return m.writeCollectionLocked(next)
}

// Preserve all collections for a resource referenced by a workflow. This is
// deliberately conservative until fine-grained reference-aware retention exists.
func (m *Memory) PruneCollections(ctx context.Context, before time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if m.poison != nil {
		return 0, m.poison
	}
	referenced := map[string]bool{}
	for _, r := range m.recs {
		referenced[r.ResourceID] = true
	}
	for _, a := range m.actionRecords {
		referenced[a.ResourceID] = true
	}
	old := map[string]Collection{}
	for id, c := range m.collections {
		if CollectionTerminal(c.State) && c.FinishedAt.Before(before) && !c.Pruned && !referenced[c.Resource.ID] {
			old[id] = c
			c.Snapshot = nil
			c.Assessment = nil
			c.Pruned = true
			m.collections[id] = c
		}
	}
	if err := m.persistLocked(); err != nil {
		for id, c := range old {
			m.collections[id] = c
		}
		return 0, err
	}
	return len(old), nil
}

// ResourceCollectionScope binds the workload incarnation and expected membership
// while excluding changing telemetry, discovery times and ownership labels.
func ResourceCollectionScope(r resource.Resource) string {
	raw, _ := json.Marshal([]any{r.ID, r.Metadata["uid"], r.Metadata["mapping"]})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func validateStoredCollection(c Collection) error {
	invalid := errors.New("invalid durable collection")
	if c.ID != CollectionID(c) || c.Resource.Validate() != nil || len(c.InstanceDigest) != 64 || c.MaxAttempts < 1 || c.MaxAttempts > 10 || c.Attempts < 0 || c.Attempts > c.MaxAttempts || len(c.History) != c.Attempts || c.CreatedAt.IsZero() || c.UpdatedAt.IsZero() || c.NextRun.IsZero() || !c.WindowEnd.After(c.WindowStart) || len(c.Signals) == 0 || len(c.Signals) > 256 || c.Actor == "" || c.PluginID == "" {
		return invalid
	}
	for i, key := range c.Signals {
		if key == "" || i > 0 && key <= c.Signals[i-1] {
			return invalid
		}
	}
	for i, h := range c.History {
		if h.Number != i+1 || h.StartedAt.IsZero() {
			return invalid
		}
	}
	switch c.State {
	case "pending", "retry_wait":
		if c.Lease != nil || !c.FinishedAt.IsZero() {
			return invalid
		}
	case "running":
		if c.Lease == nil || c.Lease.Owner == "" || c.Lease.Epoch != c.Fence || c.Attempts == 0 || !c.FinishedAt.IsZero() {
			return invalid
		}
	case "succeeded", "degraded", "failed":
		if c.Lease != nil || c.FinishedAt.IsZero() {
			return invalid
		}
	default:
		return invalid
	}
	if c.Snapshot != nil {
		if c.Pruned || (c.State != "succeeded" && c.State != "degraded") || c.Snapshot.ValidateRecords() != nil || c.Snapshot.ResourceID != c.Resource.ID || c.Snapshot.PluginID != c.PluginID || c.Assessment == nil || c.Assessment.ResourceID != c.Resource.ID || c.Assessment.PluginID != c.PluginID {
			return invalid
		}
		for _, r := range c.Snapshot.Records {
			if !r.WindowStart.Equal(c.WindowStart) || !r.WindowEnd.Equal(c.WindowEnd) {
				return invalid
			}
		}
	} else if !c.Pruned && (c.State == "succeeded" || c.State == "degraded") {
		return invalid
	}
	if c.ErrorCode != "" && !collectionErrorCode(c.ErrorCode) {
		return invalid
	}
	return nil
}
