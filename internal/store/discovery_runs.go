package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"k8s.io/apimachinery/pkg/labels"
	validation "k8s.io/apimachinery/pkg/util/validation"
	"sort"
	"strings"
	"time"

	"github.com/consize-oss/consize/internal/audit"
	"github.com/consize-oss/consize/pkg/plugin"
	"github.com/consize-oss/consize/pkg/resource"
)

// DiscoveryRun is a registry operation, never an action or metrics job.
type DiscoveryRun struct {
	ID             string                        `json:"id"`
	PluginID       string                        `json:"plugin_id"`
	Scope          map[string]string             `json:"scope"`
	ScopeDigest    string                        `json:"scope_digest"`
	ScopeKey       string                        `json:"scope_key"`
	RequestedAt    time.Time                     `json:"requested_at"`
	Actor          string                        `json:"actor"`
	Trigger        string                        `json:"trigger"`
	Health         string                        `json:"health"`
	State          string                        `json:"state"`
	Attempts       int                           `json:"attempts"`
	MaxAttempts    int                           `json:"max_attempts"`
	Fence          uint64                        `json:"fence"`
	Lease          *JobLease                     `json:"lease,omitempty"`
	History        []DiscoveryAttempt            `json:"history"`
	NextRun        time.Time                     `json:"next_run"`
	CreatedAt      time.Time                     `json:"created_at"`
	UpdatedAt      time.Time                     `json:"updated_at"`
	FinishedAt     time.Time                     `json:"finished_at,omitempty"`
	ErrorCode      string                        `json:"error_code,omitempty"`
	StaleAfter     string                        `json:"stale_after"`
	TombstoneAfter string                        `json:"tombstone_after"`
	Scan           *plugin.DiscoveryScan         `json:"scan,omitempty"`
	ResourceIDs    []string                      `json:"resource_ids"`
	Reconciliation resource.ReconciliationResult `json:"reconciliation"`
}
type DiscoveryAttempt struct {
	Number     int       `json:"number"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at,omitempty"`
	Outcome    string    `json:"outcome,omitempty"`
}

type DiscoveryCompletion struct {
	Scan      *plugin.DiscoveryScan
	Resources []resource.Resource
	ErrorCode string
	RetryAt   time.Time
}
type DiscoveryRunQuery struct {
	PluginID string
	Due      bool
	Limit    int
}
type DiscoveryRunStore interface {
	EnqueueDiscovery(context.Context, DiscoveryRun) (DiscoveryRun, error)
	GetDiscoveryRun(context.Context, string) (DiscoveryRun, error)
	ListDiscoveryRuns(context.Context, DiscoveryRunQuery) ([]DiscoveryRun, error)
	ClaimDiscovery(context.Context, string, string, time.Duration) (DiscoveryRun, error)
	CompleteDiscovery(context.Context, DiscoveryRun, DiscoveryCompletion) (DiscoveryRun, error)
}

var ErrDiscoveryInvalid = errors.New("invalid discovery result")
var ErrDiscoveryBusy = errors.New("discovery scope already has pending work")

func DiscoveryScopeIdentity(pluginID string, scope map[string]string) (string, string) {
	raw, _ := json.Marshal(scope)
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	sum = sha256.Sum256([]byte(pluginID + "\x00" + digest))
	return digest, hex.EncodeToString(sum[:])
}
func discoveryRunID(r DiscoveryRun) string {
	sum := sha256.Sum256([]byte(r.ScopeKey + "\x00" + r.RequestedAt.UTC().Format(time.RFC3339Nano)))
	return hex.EncodeToString(sum[:])
}
func DiscoveryTerminal(s string) bool { return s == "succeeded" || s == "failed" }
func prepareDiscovery(r DiscoveryRun, now time.Time) (DiscoveryRun, error) {
	if err := ValidateDiscoverySelection(r.Scope); err != nil {
		return DiscoveryRun{}, err
	}
	stale, e1 := time.ParseDuration(r.StaleAfter)
	tomb, e2 := time.ParseDuration(r.TombstoneAfter)
	if r.PluginID == "" || len(r.PluginID) > 128 || len(r.Scope) != 4 || r.Scope["cluster_id"] == "" || r.Scope["location"] == "" || r.Scope["namespaces"] == "" || r.RequestedAt.IsZero() || r.RequestedAt.After(now) || r.Actor == "" || len(r.Actor) > 256 || (r.Trigger != "manual" && r.Trigger != "scheduled") || r.MaxAttempts < 1 || r.MaxAttempts > 10 || e1 != nil || e2 != nil || stale < 0 || tomb <= stale || tomb > 365*24*time.Hour {
		return DiscoveryRun{}, errors.New("invalid discovery operation")
	}
	for k, v := range r.Scope {
		if len(k) > 64 || len(v) > 4096 {
			return DiscoveryRun{}, errors.New("invalid discovery scope")
		}
	}
	r.ScopeDigest, r.ScopeKey = DiscoveryScopeIdentity(r.PluginID, r.Scope)
	r.ID = discoveryRunID(r)
	r.State = "pending"
	r.Health = "pending"
	r.Attempts = 0
	r.Fence = 0
	r.Lease = nil
	r.History = []DiscoveryAttempt{}
	r.Scan = nil
	r.ResourceIDs = []string{}
	r.Reconciliation = resource.ReconciliationResult{Stale: []string{}, Deleted: []string{}}
	r.CreatedAt = now
	r.UpdatedAt = now
	r.FinishedAt = time.Time{}
	r.ErrorCode = ""
	if r.NextRun.IsZero() || r.NextRun.Before(now) {
		r.NextRun = now
	}
	if r.NextRun.After(now.Add(time.Hour)) {
		return DiscoveryRun{}, errors.New("invalid discovery due time")
	}
	return r, nil
}
func claimDiscovery(r DiscoveryRun, owner string, ttl time.Duration, now time.Time) (DiscoveryRun, error) {
	if err := validateLeaseOptions(owner, ttl); err != nil {
		return DiscoveryRun{}, err
	}
	if DiscoveryTerminal(r.State) || r.NextRun.After(now) {
		return DiscoveryRun{}, ErrJobChanged
	}
	if r.Lease != nil && now.Before(r.Lease.Until) {
		return DiscoveryRun{}, ErrJobBusy
	}
	if n := len(r.History); n > 0 && r.History[n-1].FinishedAt.IsZero() {
		r.History[n-1].FinishedAt = now
		r.History[n-1].Outcome = "interrupted"
	}
	if r.Attempts >= r.MaxAttempts {
		r.State = "failed"
		r.Health = "failed"
		r.ErrorCode = "attempts_exhausted"
		r.Lease = nil
		r.FinishedAt = now
		r.UpdatedAt = now
		return r, nil
	}
	r.Fence++
	r.Attempts++
	r.State = "running"
	r.Health = "running"
	r.Lease = &JobLease{Owner: owner, Epoch: r.Fence, Until: now.Add(ttl)}
	r.UpdatedAt = now
	r.History = append(r.History, DiscoveryAttempt{Number: r.Attempts, StartedAt: now})
	return r, nil
}
func discoveryOwns(r, c DiscoveryRun, now time.Time) bool {
	return r.State == "running" && r.Lease != nil && c.Lease != nil && r.Lease.Owner == c.Lease.Owner && r.Lease.Epoch == c.Lease.Epoch && now.Before(r.Lease.Until)
}
func discoveryError(code string) bool {
	switch code {
	case "scope_changed", "incomplete_scan", "invalid_observation", "configuration", "access", "timeout", "unavailable", "provider_failure", "attempts_exhausted", "registry_conflict":
		return true
	}
	return false
}
func finishDiscovery(r DiscoveryRun, c DiscoveryCompletion, now time.Time) (DiscoveryRun, error) {
	r.Lease = nil
	r.UpdatedAt = now
	r.FinishedAt = now
	r.ErrorCode = c.ErrorCode
	r.State = "succeeded"
	if c.ErrorCode != "" {
		if !discoveryError(c.ErrorCode) {
			return DiscoveryRun{}, errors.New("invalid discovery error code")
		}
		r.State = "failed"
		if !c.RetryAt.IsZero() && r.Attempts < r.MaxAttempts {
			if !c.RetryAt.After(now) || c.RetryAt.After(now.Add(time.Hour)) {
				return DiscoveryRun{}, errors.New("invalid discovery retry")
			}
			r.State = "retry_wait"
			r.FinishedAt = time.Time{}
			r.NextRun = c.RetryAt
		}
	}
	r.Health = discoveryRunHealth(r.State)
	n := len(r.History) - 1
	r.History[n].FinishedAt = now
	r.History[n].Outcome = r.State
	if r.ErrorCode != "" {
		r.History[n].Outcome = r.ErrorCode
	}
	return r, nil
}
func discoveryEvent(r DiscoveryRun) audit.Event {
	id := fmt.Sprintf("discovery-%s-%d-%s", r.ID, r.Fence, r.State)
	result := audit.ResultPending
	severity := audit.SeverityInfo
	if r.State == "succeeded" {
		result = audit.ResultSucceeded
	}
	if r.State == "failed" {
		result = audit.ResultFailed
		severity = audit.SeverityError
	}
	return audit.Event{ID: id, IdempotencyKey: id, Type: "discovery.run." + r.State, Family: audit.FamilyDiscovery, Kind: audit.KindObservation, SchemaVersion: audit.SchemaVersion, Timestamp: r.UpdatedAt, RecordedAt: r.UpdatedAt, Actor: audit.Actor{Type: "actor", ID: r.Actor}, Source: "discovery-scheduler", CorrelationID: r.ID, Workflow: audit.WorkflowRefs{PluginID: r.PluginID}, Result: result, Severity: severity, Visibility: audit.VisibilitySummary, ReasonCode: "discovery_" + r.State, Summary: "Discovery run state changed", Details: map[string]any{"operation_id": r.ID, "scope_digest": r.ScopeDigest, "attempt": r.Attempts, "error_code": r.ErrorCode, "observed": len(r.ResourceIDs), "stale": len(r.Reconciliation.Stale), "deleted": len(r.Reconciliation.Deleted)}}
}
func (m *Memory) writeDiscoveryLocked(r DiscoveryRun) (DiscoveryRun, error) {
	old, exists := m.discoveryRuns[r.ID]
	event := discoveryEvent(r)
	if err := event.Validate(); err != nil {
		return DiscoveryRun{}, err
	}
	m.discoveryRuns[r.ID] = clone(r)
	m.auditEvents[event.ID] = event
	m.auditIdempotencyIndex[event.IdempotencyKey] = event.ID
	if err := m.persistLocked(); err != nil {
		if exists {
			m.discoveryRuns[r.ID] = old
		} else {
			delete(m.discoveryRuns, r.ID)
		}
		delete(m.auditEvents, event.ID)
		delete(m.auditIdempotencyIndex, event.IdempotencyKey)
		return DiscoveryRun{}, err
	}
	return clone(r), nil
}
func (m *Memory) EnqueueDiscovery(ctx context.Context, r DiscoveryRun) (DiscoveryRun, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return DiscoveryRun{}, err
	}
	if m.poison != nil {
		return DiscoveryRun{}, m.poison
	}
	r, err := prepareDiscovery(r, m.now().UTC())
	if err != nil {
		return DiscoveryRun{}, err
	}
	if prior, ok := m.discoveryRuns[r.ID]; ok {
		return clone(prior), nil
	}
	for _, prior := range m.discoveryRuns {
		if prior.ScopeKey == r.ScopeKey && !DiscoveryTerminal(prior.State) {
			return DiscoveryRun{}, ErrDiscoveryBusy
		}
	}
	return m.writeDiscoveryLocked(r)
}
func (m *Memory) GetDiscoveryRun(ctx context.Context, id string) (DiscoveryRun, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if err := ctx.Err(); err != nil {
		return DiscoveryRun{}, err
	}
	if m.poison != nil {
		return DiscoveryRun{}, m.poison
	}
	r, ok := m.discoveryRuns[id]
	if !ok {
		return DiscoveryRun{}, ErrNotFound
	}
	return clone(r), nil
}
func discoveryDue(r DiscoveryRun, now time.Time) bool {
	return !DiscoveryTerminal(r.State) && !r.NextRun.After(now) && (r.Lease == nil || !now.Before(r.Lease.Until))
}
func (m *Memory) ListDiscoveryRuns(ctx context.Context, q DiscoveryRunQuery) ([]DiscoveryRun, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if m.poison != nil {
		return nil, m.poison
	}
	out := []DiscoveryRun{}
	for _, r := range m.discoveryRuns {
		if (q.PluginID == "" || q.PluginID == r.PluginID) && (!q.Due || discoveryDue(r, m.now())) {
			out = append(out, r)
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
func (m *Memory) ClaimDiscovery(ctx context.Context, id, owner string, ttl time.Duration) (DiscoveryRun, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return DiscoveryRun{}, err
	}
	if m.poison != nil {
		return DiscoveryRun{}, m.poison
	}
	r, ok := m.discoveryRuns[id]
	if !ok {
		return DiscoveryRun{}, ErrNotFound
	}
	r, err := claimDiscovery(clone(r), owner, ttl, m.now().UTC())
	if err != nil {
		return DiscoveryRun{}, err
	}
	return m.writeDiscoveryLocked(r)
}
func (m *Memory) CompleteDiscovery(ctx context.Context, candidate DiscoveryRun, c DiscoveryCompletion) (DiscoveryRun, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return DiscoveryRun{}, err
	}
	if m.poison != nil {
		return DiscoveryRun{}, m.poison
	}
	r, ok := m.discoveryRuns[candidate.ID]
	if !ok {
		return DiscoveryRun{}, ErrNotFound
	}
	now := m.now().UTC()
	if !discoveryOwns(r, candidate, now) {
		return DiscoveryRun{}, ErrLeaseLost
	}
	priorResources := m.resources
	priorIndex := m.resourceIdentityIndex
	if c.ErrorCode == "" {
		next, ids, result, err := planDiscovery(m.resources, r, c, now)
		if err != nil {
			return DiscoveryRun{}, fmt.Errorf("%w: %v", ErrDiscoveryInvalid, err)
		}
		m.resources = next
		m.resourceIdentityIndex = map[string]string{}
		for _, row := range next {
			m.resourceIdentityIndex[resourceIdentityKey(row)] = row.ID
		}
		r.ResourceIDs = ids
		r.Reconciliation = result
		r.Scan = c.Scan
	}
	next, err := finishDiscovery(r, c, now)
	if err != nil {
		m.resources = priorResources
		m.resourceIdentityIndex = priorIndex
		return DiscoveryRun{}, err
	}
	out, err := m.writeDiscoveryLocked(next)
	if err != nil {
		m.resources = priorResources
		m.resourceIdentityIndex = priorIndex
	}
	return out, err
}

func ValidateDiscoverySelection(scope map[string]string) error {
	invalid := errors.New("invalid discovery selection")
	if len(scope) != 4 {
		return invalid
	}
	for _, key := range []string{"cluster_id", "location", "namespaces", "label_selector"} {
		v, ok := scope[key]
		if !ok || len(v) > 4096 || strings.ContainsAny(v, "\r\n\x00") {
			return invalid
		}
	}
	if _, err := resource.BuildID(resource.Identity{Provider: resource.ProviderKubernetes, Account: scope["cluster_id"], Location: scope["location"], Type: resource.TypeKubernetesDeployment, ProviderResourceID: "scope/probe"}); err != nil {
		return invalid
	}
	if _, err := labels.Parse(scope["label_selector"]); err != nil {
		return invalid
	}
	if scope["namespaces"] == "*" {
		return nil
	}
	seen := map[string]bool{}
	for _, ns := range strings.Split(scope["namespaces"], ",") {
		if len(validation.IsDNS1123Label(ns)) != 0 || seen[ns] {
			return invalid
		}
		seen[ns] = true
	}
	if len(seen) > 128 {
		return invalid
	}
	return nil
}
func validateDiscoveryRun(r DiscoveryRun) error {
	invalid := errors.New("invalid durable discovery run")
	digest, key := DiscoveryScopeIdentity(r.PluginID, r.Scope)
	stale, e1 := time.ParseDuration(r.StaleAfter)
	tomb, e2 := time.ParseDuration(r.TombstoneAfter)
	if e1 != nil || e2 != nil || stale < 0 || tomb <= stale || r.Health != discoveryRunHealth(r.State) {
		return invalid
	}
	if ValidateDiscoverySelection(r.Scope) != nil || r.ScopeDigest != digest || r.ScopeKey != key || r.ID != discoveryRunID(r) || r.PluginID == "" || r.MaxAttempts < 1 || r.MaxAttempts > 10 || r.Attempts < 0 || r.Attempts > r.MaxAttempts || len(r.History) != r.Attempts || r.Actor == "" || r.RequestedAt.IsZero() || r.CreatedAt.IsZero() || r.UpdatedAt.IsZero() || r.NextRun.IsZero() {
		return invalid
	}
	for i, h := range r.History {
		if h.Number != i+1 || h.StartedAt.IsZero() {
			return invalid
		}
	}
	switch r.State {
	case "pending", "retry_wait":
		if r.Lease != nil || !r.FinishedAt.IsZero() {
			return invalid
		}
	case "running":
		if r.Lease == nil || r.Lease.Epoch != r.Fence || r.Lease.Owner == "" || r.Attempts == 0 || !r.FinishedAt.IsZero() {
			return invalid
		}
	case "succeeded", "failed":
		if r.Lease != nil || r.FinishedAt.IsZero() {
			return invalid
		}
	default:
		return invalid
	}
	if r.State == "succeeded" && (r.Scan == nil || !r.Scan.Complete || r.Scan.ScopeDigest != r.ScopeDigest || r.Scan.PluginID != r.PluginID) {
		return invalid
	}
	if r.ErrorCode != "" && !discoveryError(r.ErrorCode) {
		return invalid
	}
	return nil
}

func discoveryRunHealth(state string) string {
	switch state {
	case "succeeded":
		return "healthy"
	case "retry_wait":
		return "degraded"
	default:
		return state
	}
}
