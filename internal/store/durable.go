package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/consize-oss/consize/pkg/resource"
)

type diskState struct {
	Version         int                          `json:"version"`
	Migrations      []MigrationRecord            `json:"migrations,omitempty"`
	Resources       map[string]resource.Resource `json:"resources"`
	Recommendations map[int64]Recommendation     `json:"recommendations"`
	ActionRecords   map[int64]Action             `json:"action_records"`
	Actions         map[int64]ActionEvent        `json:"actions"`
	Jobs            map[int64]Job                `json:"jobs"`
	NextActionID    int64                        `json:"next_action_id"`
	NextEventID     int64                        `json:"next_event_id"`
	NextRecID       int64                        `json:"next_recommendation_id"`
}

const (
	minimumStateVersion = 1
	currentStateVersion = 5
)

// OpenDurable owns one local state file for its entire lifetime; other processes fail closed.
func OpenDurable(path string) (*Memory, error) {
	if path == "" {
		return nil, errors.New("state_path is required")
	}
	lock, err := acquireStateLock(path)
	if err != nil {
		return nil, err
	}
	m := NewMemory()
	m.path = path
	m.lockFile = lock
	m.diagnostics = StorageDiagnostics{Backend: "json-file", Durable: true, Healthy: true, CurrentVersion: currentStateVersion, LoadedVersion: currentStateVersion, TargetVersion: currentStateVersion, MigrationStatus: MigrationCurrent, Indexes: RequiredIndexes()}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := m.persistLocked(); err != nil {
			m.Close()
			return nil, err
		}
		return m, nil
	}
	if err != nil {
		m.Close()
		return nil, err
	}
	state, err := decodeDiskState(data)
	if err != nil {
		m.Close()
		return nil, err
	}
	originalVersion := state.Version
	migrated, err := migrateState(&state)
	if err != nil {
		m.Close()
		return nil, fmt.Errorf("state schema cannot start: %w; restore a compatible backup or run `consize storage status -state %s`", err, path)
	}
	if state.Resources == nil || state.Recommendations == nil || state.ActionRecords == nil || state.Actions == nil || state.Jobs == nil || state.NextActionID < 1 || state.NextEventID < 1 || state.NextRecID < 1 {
		m.Close()
		return nil, errors.New("invalid state schema")
	}
	if err := validateState(state); err != nil {
		m.Close()
		return nil, err
	}
	m.resources = state.Resources
	m.recs = state.Recommendations
	m.actionRecords = state.ActionRecords
	m.actions = state.Actions
	m.jobs = state.Jobs
	m.nextActionID = state.NextActionID
	m.nextEventID = state.NextEventID
	m.nextRecID = state.NextRecID
	if err := m.rebuildIndexesLocked(); err != nil {
		m.Close()
		return nil, fmt.Errorf("rebuild storage indexes: %w", err)
	}
	m.diagnostics.CurrentVersion = state.Version
	m.diagnostics.LoadedVersion = originalVersion
	m.diagnostics.Migrations = append([]MigrationRecord(nil), state.Migrations...)
	if migrated {
		m.diagnostics.MigrationStatus = MigrationApplied
		m.diagnostics.CurrentVersion = currentStateVersion
	}
	if migrated {
		if err := m.persistLocked(); err != nil {
			m.Close()
			return nil, fmt.Errorf("persist migrated state: %w", err)
		}
	}
	return m, nil
}

func migrateState(state *diskState) (bool, error) {
	migrated := false
	if state.Version < minimumStateVersion || state.Version > currentStateVersion {
		return false, fmt.Errorf("unsupported state schema version %d (supported %d through %d)", state.Version, minimumStateVersion, currentStateVersion)
	}
	if state.Version == 1 {
		// v2 adds discovery provenance to Resource. The fields are optional for
		// manually registered v1 resources and are populated on rediscovery.
		state.Version = 2
		state.Migrations = append(state.Migrations, MigrationRecord{FromVersion: 1, ToVersion: 2, Name: "resource-discovery-provenance"})
		migrated = true
	}
	if state.Version == 2 {
		// v3 adds explicit model, support, and lifecycle versions. Immutable
		// provider identity is never guessed during migration.
		for id, res := range state.Resources {
			res.SchemaVersion = resource.CurrentSchemaVersion
			if res.LifecycleState == "" {
				res.LifecycleState = resource.LifecycleActive
			}
			res.SupportStatus = resource.SupportForType(res.Type)
			if res.ObservedAt.IsZero() {
				res.ObservedAt = firstStoredTime(res.UpdatedAt, res.CreatedAt)
			}
			if res.FirstSeenAt.IsZero() {
				res.FirstSeenAt = firstStoredTime(res.CreatedAt, res.ObservedAt)
			}
			if res.LastSeenAt.IsZero() {
				res.LastSeenAt = firstStoredTime(res.ObservedAt, res.UpdatedAt, res.CreatedAt)
			}
			state.Resources[id] = res
		}
		state.Version = 3
		state.Migrations = append(state.Migrations, MigrationRecord{FromVersion: 2, ToVersion: 3, Name: "resource-lifecycle-contract"})
		migrated = true
	}
	if state.Version == 3 {
		// v4 separates durable actions from append-only action events. Existing
		// event IDs stay intact; new action IDs begin in their own sequence.
		state.ActionRecords = map[int64]Action{}
		state.NextEventID = state.NextActionID
		state.NextActionID = 1
		for id, rec := range state.Recommendations {
			rec.SchemaVersion = ContractVersion
			if rec.AlgorithmVersion == "" {
				rec.AlgorithmVersion = "1"
			}
			if rec.RecommendationType == "" {
				rec.RecommendationType = rec.ActionType
			}
			if rec.AlgorithmID == "" {
				rec.AlgorithmID = "legacy-unknown"
			}
			if len(rec.EvidenceRefs) == 0 {
				rec.EvidenceRefs = []string{fmt.Sprintf("legacy:recommendation:%d", id)}
			}
			if rec.SavingsEstimate.Classification == "" {
				rec.SavingsEstimate = SavingsEstimate{Classification: SavingsEstimated, AmountMonthly: rec.EstimatedSavingsMonthly, CalculatedAt: rec.CreatedAt}
			}
			state.Recommendations[id] = rec
		}
		state.Version = 4
		state.Migrations = append(state.Migrations, MigrationRecord{FromVersion: 3, ToVersion: 4, Name: "separate-action-contract"})
		migrated = true
	}
	if state.Version == 4 {
		// v5 records deterministic migration history. Runtime query indexes are
		// derived from canonical entities and rebuilt after validation.
		for id, rec := range state.Recommendations {
			if rec.AlgorithmID == "" {
				rec.AlgorithmID = "legacy-unknown"
			}
			if len(rec.EvidenceRefs) == 0 {
				rec.EvidenceRefs = []string{fmt.Sprintf("legacy:recommendation:%d", id)}
			}
			state.Recommendations[id] = rec
		}
		state.Migrations = append(state.Migrations, MigrationRecord{FromVersion: 4, ToVersion: 5, Name: "migration-history-and-derived-indexes"})
		state.Version = 5
		migrated = true
	}
	if state.Version != currentStateVersion {
		return false, fmt.Errorf("unsupported state schema version %d", state.Version)
	}
	return migrated, nil
}

func firstStoredTime(values ...time.Time) time.Time {
	for _, value := range values {
		if !value.IsZero() {
			return value.UTC()
		}
	}
	return time.Time{}
}

func validateState(state diskState) error {
	if state.Version != currentStateVersion {
		return fmt.Errorf("state validation requires schema version %d, got %d", currentStateVersion, state.Version)
	}
	if err := validateMigrationHistory(state.Migrations); err != nil {
		return err
	}
	for id, res := range state.Resources {
		if id == "" || res.ID != id {
			return errors.New("invalid resource key in durable state")
		}
		if err := res.Validate(); err != nil {
			return fmt.Errorf("invalid durable resource %q: %w", id, err)
		}
		for otherID, other := range state.Resources {
			if otherID != id && resource.SameIdentity(res, other) {
				return fmt.Errorf("duplicate resource identity in durable state: %q and %q", id, otherID)
			}
		}
	}
	for id, rec := range state.Recommendations {
		if id < 1 || rec.ID != id || id >= state.NextRecID {
			return errors.New("invalid recommendation sequence in durable state")
		}
		if _, ok := state.Resources[rec.ResourceID]; !ok {
			return errors.New("missing recommendation resource in durable state")
		}
		if rec.AlgorithmID == "" || len(rec.EvidenceRefs) == 0 {
			return errors.New("recommendation is missing algorithm or evidence provenance")
		}
	}
	for id, event := range state.Actions {
		if id < 1 || event.ID != id || id >= state.NextEventID {
			return errors.New("invalid action sequence in durable state")
		}
		if event.RecommendationID > 0 {
			if _, ok := state.Recommendations[event.RecommendationID]; !ok {
				return errors.New("missing audit recommendation in durable state")
			}
		}
		if event.ActionID > 0 {
			action, ok := state.ActionRecords[event.ActionID]
			if !ok || (event.RecommendationID > 0 && action.RecommendationID != event.RecommendationID) {
				return errors.New("missing or mismatched audit action in durable state")
			}
		}
	}
	for id, action := range state.ActionRecords {
		if id < 1 || action.ID != id || id >= state.NextActionID {
			return errors.New("invalid action sequence in durable state")
		}
		rec, ok := state.Recommendations[action.RecommendationID]
		if !ok || rec.ResourceID != action.ResourceID {
			return errors.New("invalid action recommendation link in durable state")
		}
		if err := action.Validate(); err != nil {
			return fmt.Errorf("invalid durable action %d: %w", id, err)
		}
	}
	seenIdempotency := map[string]int64{}
	for id, action := range state.ActionRecords {
		if existing, ok := seenIdempotency[action.IdempotencyKey]; ok && existing != id {
			return errors.New("duplicate action idempotency key in durable state")
		}
		seenIdempotency[action.IdempotencyKey] = id
	}
	for id, job := range state.Jobs {
		recommendationID := job.RecommendationID
		if recommendationID == 0 {
			recommendationID = id
		}
		rec, ok := state.Recommendations[recommendationID]
		if !ok || job.ID != id || job.Resource.ID != rec.ResourceID || job.Plan.ResourceID != rec.ResourceID || job.Plan.PluginID != rec.PluginID || job.Actor == "" {
			return errors.New("invalid safety job identity in durable state")
		}
		if job.ActionID > 0 {
			action, ok := state.ActionRecords[job.ActionID]
			if !ok || action.RecommendationID != recommendationID {
				return errors.New("invalid safety job action link in durable state")
			}
		}
		switch job.State {
		case "prepared", "applying", "waiting_rollout", "verifying", "rollback_pending", "rolling_back", "rollback_verifying", "verified", "rolled_back", "manual_intervention", "cancelled":
		default:
			return errors.New("unknown safety state")
		}
		if (job.State == "verifying" || job.State == "rollback_verifying") && job.WindowStart.IsZero() {
			return errors.New("missing observation window in durable state")
		}
		if !Terminal(job.State) && job.Deadline.IsZero() {
			return errors.New("missing action deadline in durable state")
		}
	}
	return nil
}

func validateMigrationHistory(records []MigrationRecord) error {
	for i, record := range records {
		if record.FromVersion < minimumStateVersion || record.ToVersion != record.FromVersion+1 || record.ToVersion > currentStateVersion || record.Name == "" {
			return fmt.Errorf("invalid migration history entry %d", i)
		}
		if i > 0 && records[i-1].ToVersion != record.FromVersion {
			return fmt.Errorf("non-contiguous migration history at entry %d", i)
		}
	}
	return nil
}

func (m *Memory) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.lockFile == nil {
		return nil
	}
	err := m.lockFile.Close()
	m.lockFile = nil
	m.poison = errors.New("store closed")
	return err
}

func (m *Memory) Durable() bool { return m.path != "" }

func (m *Memory) persistLocked() (err error) {
	if m.poison != nil {
		return m.poison
	}
	if m.path == "" {
		return nil
	}
	defer func() {
		if err != nil {
			m.poison = fmt.Errorf("durable store unavailable: %w", err)
		}
	}()
	state := diskState{
		Version: currentStateVersion, Migrations: append([]MigrationRecord(nil), m.diagnostics.Migrations...), Resources: m.resources,
		Recommendations: m.recs, ActionRecords: m.actionRecords, Actions: m.actions, Jobs: m.jobs,
		NextActionID: m.nextActionID, NextEventID: m.nextEventID, NextRecID: m.nextRecID,
	}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Dir(m.path)); err != nil {
		return err
	}
	return atomicWrite(m.path, data, 0600)
}

func clone[T any](value T) T {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	var out T
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&out); err != nil {
		panic(err)
	}
	return out
}
