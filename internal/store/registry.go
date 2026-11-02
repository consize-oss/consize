package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/consize-oss/consize/pkg/resource"
)

func normalizeObservation(in resource.Resource, now time.Time) (resource.Resource, error) {
	in.LifecycleObservedAt = time.Time{}
	in.FirstSeenAt = time.Time{}
	in.LastSeenAt = time.Time{}
	in.CreatedAt = time.Time{}
	in.UpdatedAt = time.Time{}
	return in.Normalize(now)
}

func prepareResource(in resource.Resource, existing *resource.Resource, now time.Time, createOnly bool) (resource.Resource, error) {
	r, err := normalizeObservation(in, now)
	if err != nil {
		return resource.Resource{}, err
	}
	// Observation time is authoritative; callers cannot forge lifecycle clocks.
	r.LifecycleObservedAt = r.ObservedAt
	if existing != nil {
		if !resource.SameIdentity(*existing, r) {
			return resource.Resource{}, fmt.Errorf("%w: ID belongs to another provider resource", resource.ErrIdentityConflict)
		}
		if createOnly {
			return resource.Resource{}, resource.ErrAlreadyExists
		}
		if existing.ID != r.ID {
			if !resource.IsCanonicalID(existing.ID) && resource.IsCanonicalID(r.ID) {
				r.ID = existing.ID
			} else {
				return resource.Resource{}, fmt.Errorf("%w: identity already registered", resource.ErrIdentityConflict)
			}
		}
		if r.ObservedAt.Before(existing.LastSeenAt) || r.ObservedAt.Before(existing.LifecycleObservedAt) {
			return resource.Resource{}, resource.ErrStaleObservation
		}
		if !resource.ValidLifecycleTransition(existing.LifecycleState, r.LifecycleState) {
			return resource.Resource{}, resource.ErrInvalidTransition
		}
		if existing.SourcePluginID != "" && r.SourcePluginID != "" && existing.SourcePluginID != r.SourcePluginID {
			return resource.Resource{}, resource.ErrScopeMismatch
		}
		if r.SourcePluginID == "" {
			r.SourcePluginID = existing.SourcePluginID
		}
		r.FirstSeenAt = existing.FirstSeenAt
		r.CreatedAt = existing.CreatedAt
	}
	r.LastSeenAt = r.ObservedAt
	r.UpdatedAt = now.UTC()
	return r, r.Validate()
}

func scopeMatches(r resource.Resource, s resource.Scope) bool {
	return r.Provider == s.Provider && r.Account == s.Account && r.Region == s.Region && r.Type == s.Type && r.SourcePluginID == s.SourcePluginID
}
func validateScope(s resource.Scope) error {
	for _, v := range []string{s.Provider, s.Account, s.Region, s.Type, s.SourcePluginID} {
		if strings.TrimSpace(v) == "" || len(v) > 512 {
			return fmt.Errorf("%w: complete provider/account/region/type/source scope required", resource.ErrScopeMismatch)
		}
	}
	return nil
}
func lifecycleChange(r resource.Resource, state string, at, now time.Time) (resource.Resource, error) {
	if at.IsZero() || at.After(now.Add(time.Minute)) {
		return resource.Resource{}, fmt.Errorf("%w: observation time required", resource.ErrInvalidResource)
	}
	if at.Before(r.LastSeenAt) || at.Before(r.LifecycleObservedAt) {
		return resource.Resource{}, resource.ErrStaleObservation
	}
	if !resource.ValidLifecycleTransition(r.LifecycleState, state) {
		return resource.Resource{}, resource.ErrInvalidTransition
	}
	// Reappearance must include a fresh provider observation via UpsertResource.
	if state == resource.LifecycleActive && r.LifecycleState != state {
		return resource.Resource{}, fmt.Errorf("%w: reactivate with a fresh upsert", resource.ErrInvalidTransition)
	}
	r.LifecycleState = state
	r.LifecycleObservedAt = at.UTC()
	r.UpdatedAt = now.UTC()
	return r, r.Validate()
}

func planReconciliation(resources []resource.Resource, req resource.Reconciliation, now time.Time) ([]resource.Resource, resource.ReconciliationResult, error) {
	result := resource.ReconciliationResult{Stale: []string{}, Deleted: []string{}}
	if err := validateScope(req.Scope); err != nil {
		return nil, result, err
	}
	if req.ObservedAt.IsZero() || req.ObservedAt.After(now.Add(time.Minute)) {
		return nil, result, fmt.Errorf("%w: scan observation time required", resource.ErrInvalidResource)
	}
	scoped := map[string]resource.Resource{}
	for _, r := range resources {
		if scopeMatches(r, req.Scope) {
			scoped[r.ID] = r
		}
	}
	observed := map[string]bool{}
	deleted := map[string]bool{}
	for _, id := range req.ObservedIDs {
		if _, ok := scoped[id]; !ok {
			return nil, result, fmt.Errorf("%w: observed ID is missing or outside scan scope", resource.ErrScopeMismatch)
		}
		if observed[id] {
			return nil, result, fmt.Errorf("%w: duplicate observed ID", resource.ErrScopeMismatch)
		}
		observed[id] = true
	}
	for _, id := range req.ConfirmedDeletedIDs {
		if _, ok := scoped[id]; !ok || observed[id] || deleted[id] {
			return nil, result, fmt.Errorf("%w: invalid or conflicting deletion confirmation", resource.ErrScopeMismatch)
		}
		deleted[id] = true
	}
	changes := []resource.Resource{}
	for _, r := range scoped {
		if req.ObservedAt.Before(r.LastSeenAt) || req.ObservedAt.Before(r.LifecycleObservedAt) {
			continue
		}
		state := resource.LifecycleStale
		if deleted[r.ID] {
			state = resource.LifecycleDeleted
		} else if observed[r.ID] {
			if r.LifecycleState != resource.LifecycleActive {
				return nil, result, fmt.Errorf("%w: fresh upsert required before observing inactive resource", resource.ErrInvalidTransition)
			}
			r.LastSeenAt = req.ObservedAt.UTC()
			r.LifecycleObservedAt = req.ObservedAt.UTC()
			r.UpdatedAt = now.UTC()
			changes = append(changes, r)
			continue
		} else if r.LifecycleState == resource.LifecycleDeleted {
			continue
		}
		updated, err := lifecycleChange(r, state, req.ObservedAt, now)
		if err != nil {
			return nil, result, err
		}
		changes = append(changes, updated)
		if r.LifecycleState != state {
			if state == resource.LifecycleDeleted {
				result.Deleted = append(result.Deleted, r.ID)
			} else {
				result.Stale = append(result.Stale, r.ID)
			}
		}
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].ID < changes[j].ID })
	sort.Strings(result.Stale)
	sort.Strings(result.Deleted)
	return changes, result, nil
}

type resourceCursor struct {
	After  string `json:"after"`
	Filter string `json:"filter"`
}

func parseResourceQuery(q resource.Query) (resource.Query, string, error) {
	if q.Limit == 0 {
		q.Limit = 100
	}
	if q.Limit < 1 || q.Limit > 200 {
		return q, "", fmt.Errorf("%w: limit must be 1..200", resource.ErrInvalidQuery)
	}
	for _, v := range []string{q.Provider, q.Account, q.Region, q.Type, q.SourcePluginID, q.Environment, q.LifecycleState, q.SupportStatus, q.Owner} {
		if len(v) > 512 || strings.ContainsAny(v, "\r\n\x00") {
			return q, "", resource.ErrInvalidQuery
		}
	}
	if q.LifecycleState != "" && q.LifecycleState != resource.LifecycleActive && q.LifecycleState != resource.LifecycleStale && q.LifecycleState != resource.LifecycleDeleted {
		return q, "", resource.ErrInvalidQuery
	}
	if q.Environment != "" && q.Environment != resource.EnvProduction && q.Environment != resource.EnvStaging && q.Environment != resource.EnvDevelopment {
		return q, "", resource.ErrInvalidQuery
	}
	if q.SupportStatus != "" && q.SupportStatus != resource.SupportFull && q.SupportStatus != resource.SupportModelOnly && q.SupportStatus != resource.SupportUnsupported {
		return q, "", resource.ErrInvalidQuery
	}
	b, _ := json.Marshal(q)
	hash := fmt.Sprintf("%x", sha256.Sum256(b))
	if q.Cursor == "" {
		return q, "", nil
	}
	if len(q.Cursor) > 8192 {
		return q, "", resource.ErrInvalidQuery
	}
	b, err := base64.RawURLEncoding.DecodeString(q.Cursor)
	if err != nil {
		return q, "", resource.ErrInvalidQuery
	}
	var cursor resourceCursor
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&cursor); err != nil {
		return q, "", resource.ErrInvalidQuery
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF || cursor.Filter != hash || cursor.After == "" {
		return q, "", fmt.Errorf("%w: cursor does not match filters", resource.ErrInvalidQuery)
	}
	return q, cursor.After, nil
}
func queryMatches(r resource.Resource, q resource.Query) bool {
	for _, pair := range [][2]string{{q.Provider, r.Provider}, {q.Account, r.Account}, {q.Region, r.Region}, {q.Type, r.Type}, {q.SourcePluginID, r.SourcePluginID}, {q.Environment, r.Environment}, {q.LifecycleState, r.LifecycleState}, {q.SupportStatus, r.SupportStatus}, {q.Owner, r.Owner}} {
		if pair[0] != "" && pair[0] != pair[1] {
			return false
		}
	}
	return true
}
func resourcePage(rows []resource.Resource, q resource.Query) resource.Page {
	page := resource.Page{Resources: rows, HasMore: len(rows) > q.Limit}
	if page.HasMore {
		page.Resources = rows[:q.Limit]
		filter, _ := json.Marshal(q)
		cursor, _ := json.Marshal(resourceCursor{After: page.Resources[len(page.Resources)-1].ID, Filter: fmt.Sprintf("%x", sha256.Sum256(filter))})
		page.NextCursor = base64.RawURLEncoding.EncodeToString(cursor)
	}
	return page
}
func (m *Memory) ListResourcePage(ctx context.Context, q resource.Query) (resource.Page, error) {
	q, after, err := parseResourceQuery(q)
	if err != nil {
		return resource.Page{}, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if err := ctx.Err(); err != nil {
		return resource.Page{}, err
	}
	rows := []resource.Resource{}
	for _, r := range m.resources {
		if r.ID > after && queryMatches(r, q) {
			rows = append(rows, r)
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	return clone(resourcePage(rows, q)), nil
}
func (m *Memory) UpdateResourceLifecycle(ctx context.Context, req resource.LifecycleUpdate) (resource.Resource, error) {
	if err := validateScope(req.Scope); err != nil {
		return resource.Resource{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return resource.Resource{}, err
	}
	previous, ok := m.resources[req.ID]
	if !ok {
		return resource.Resource{}, resource.ErrNotFound
	}
	if !scopeMatches(previous, req.Scope) {
		return resource.Resource{}, resource.ErrScopeMismatch
	}
	updated, err := lifecycleChange(previous, req.State, req.ObservedAt, m.now())
	if err != nil {
		return resource.Resource{}, err
	}
	m.resources[req.ID] = updated
	if err = m.persistLocked(); err != nil {
		m.resources[req.ID] = previous
		return resource.Resource{}, err
	}
	return clone(updated), nil
}
func (m *Memory) ReconcileResources(ctx context.Context, req resource.Reconciliation) (resource.ReconciliationResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return resource.ReconciliationResult{}, err
	}
	rows := make([]resource.Resource, 0, len(m.resources))
	for _, r := range m.resources {
		rows = append(rows, r)
	}
	changes, result, err := planReconciliation(rows, req, m.now())
	if err != nil {
		return result, err
	}
	originals := map[string]resource.Resource{}
	for _, r := range changes {
		originals[r.ID] = m.resources[r.ID]
		m.resources[r.ID] = r
	}
	if err = m.persistLocked(); err != nil {
		for id, r := range originals {
			m.resources[id] = r
		}
		return resource.ReconciliationResult{}, err
	}
	return result, nil
}
