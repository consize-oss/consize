package store

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/consize-oss/consize/internal/audit"
	"github.com/google/uuid"
	"sort"
	"time"

	"github.com/consize-oss/consize/pkg/plugin"
)

func (m *Memory) SavePluginStatus(ctx context.Context, status plugin.InstanceStatus) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := plugin.ValidateInstanceStatus(status); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.poison != nil {
		return m.poison
	}
	old, exists := m.pluginStatuses[status.Identity.InstanceID]
	if exists {
		status = mergePluginStatus(old, status)
	}
	var event *audit.Event
	if !exists || pluginStatusChanged(old, status) {
		built, err := pluginRuntimeEvent(status, m.now().UTC())
		if err != nil {
			return err
		}
		event = &built
		m.auditEvents[built.ID] = built
		m.auditIdempotencyIndex[built.IdempotencyKey] = built.ID
	}
	m.pluginStatuses[status.Identity.InstanceID] = clone(status)
	if err := m.persistLocked(); err != nil {
		if exists {
			m.pluginStatuses[status.Identity.InstanceID] = old
		} else {
			delete(m.pluginStatuses, status.Identity.InstanceID)
		}
		if event != nil {
			delete(m.auditEvents, event.ID)
			delete(m.auditIdempotencyIndex, event.IdempotencyKey)
		}
		return err
	}
	return nil
}
func (m *Memory) LoadPluginStatuses(ctx context.Context) ([]plugin.InstanceStatus, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.poison != nil {
		return nil, m.poison
	}
	out := make([]plugin.InstanceStatus, 0, len(m.pluginStatuses))
	for _, status := range m.pluginStatuses {
		out = append(out, clone(status))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Identity.InstanceID < out[j].Identity.InstanceID })
	return out, nil
}
func (p *Postgres) SavePluginStatus(ctx context.Context, status plugin.InstanceStatus) error {
	if err := plugin.ValidateInstanceStatus(status); err != nil {
		return err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,47))`, status.Identity.InstanceID); err != nil {
		return err
	}
	old, oldErr := getJSONTx[plugin.InstanceStatus](ctx, tx, nil, `SELECT payload FROM plugin_runtime WHERE instance_id=$1`, status.Identity.InstanceID)
	if oldErr != nil && !errors.Is(oldErr, ErrNotFound) {
		return oldErr
	}
	if oldErr == nil {
		if plugin.ValidateInstanceStatus(old) != nil {
			return errors.New("invalid persisted plugin runtime status")
		}
		status = mergePluginStatus(old, status)
	}
	raw, err := json.Marshal(status)
	if err != nil {
		return errors.New("invalid plugin runtime status")
	}
	if errors.Is(oldErr, ErrNotFound) || pluginStatusChanged(old, status) {
		event, err := pluginRuntimeEvent(status, time.Now().UTC())
		if err != nil {
			return err
		}
		if err = insertAuditEvent(ctx, tx, event); err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, `INSERT INTO plugin_runtime(instance_id,payload) VALUES($1,$2) ON CONFLICT(instance_id) DO UPDATE SET payload=excluded.payload`, status.Identity.InstanceID, raw)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (p *Postgres) LoadPluginStatuses(ctx context.Context) ([]plugin.InstanceStatus, error) {
	rows, err := p.pool.Query(ctx, `SELECT payload FROM plugin_runtime ORDER BY instance_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []plugin.InstanceStatus{}
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var status plugin.InstanceStatus
		if json.Unmarshal(raw, &status) != nil || plugin.ValidateInstanceStatus(status) != nil {
			return nil, errors.New("invalid persisted plugin runtime status")
		}
		out = append(out, status)
	}
	return out, rows.Err()
}

func pluginStatusChanged(a, b plugin.InstanceStatus) bool {
	if (a.ProviderFailure == nil) != (b.ProviderFailure == nil) {
		return true
	}
	if a.ProviderFailure != nil && *a.ProviderFailure != *b.ProviderFailure {
		return true
	}
	return a.Identity != b.Identity || a.State != b.State || a.Failure != b.Failure
}
func pluginRuntimeEvent(status plugin.InstanceStatus, now time.Time) (audit.Event, error) {
	result, severity := audit.ResultPending, audit.SeverityInfo
	switch status.State {
	case plugin.StateReady, plugin.StateStopped:
		result = audit.ResultSucceeded
	case plugin.StateDegraded:
		result = audit.ResultInconclusive
		severity = audit.SeverityWarning
	case plugin.StateFailed, plugin.StateUnavailable, plugin.StateIncompatible:
		result = audit.ResultFailed
		severity = audit.SeverityError
	}
	details, redaction, err := audit.SanitizeDetails(map[string]any{"target": map[string]any{"identity": status.Identity, "manifest": status.Manifest}, "status": status.State, "error_code": status.Failure, "reason": status.ProviderFailure}, audit.DefaultMaxDetailsBytes)
	if err != nil {
		return audit.Event{}, err
	}
	id := uuid.NewString()
	event := audit.Event{ID: id, IdempotencyKey: id, Type: "plugin.runtime.changed", Family: audit.FamilyPlugin, Kind: audit.KindObservation, SchemaVersion: audit.SchemaVersion, Timestamp: now, RecordedAt: now, Actor: audit.Actor{Type: "service", ID: "plugin-manager"}, Source: "plugin-runtime", CorrelationID: status.Identity.InstanceID, Workflow: audit.WorkflowRefs{PluginID: status.Identity.InstanceID}, Result: result, Severity: severity, Visibility: audit.VisibilitySummary, ReasonCode: "plugin_runtime_changed", Summary: "Plugin runtime state changed", Details: details, Redaction: redaction}
	return event, event.Validate()
}

// Independent API/worker processes must not erase each other's last timestamps.
func mergePluginStatus(old, next plugin.InstanceStatus) plugin.InstanceStatus {
	if old.Identity != next.Identity {
		return next
	}
	later := func(a, b *time.Time) *time.Time {
		if a != nil && (b == nil || a.After(*b)) {
			v := *a
			return &v
		}
		return b
	}
	next.LastHealth = later(old.LastHealth, next.LastHealth)
	next.LastSuccess = later(old.LastSuccess, next.LastSuccess)
	if old.LastFailure != nil && (next.LastFailure == nil || old.LastFailure.After(*next.LastFailure)) {
		next.LastFailure = later(old.LastFailure, next.LastFailure)
		next.Failure = old.Failure
		if old.ProviderFailure != nil {
			v := *old.ProviderFailure
			next.ProviderFailure = &v
		}
	}
	return next
}
