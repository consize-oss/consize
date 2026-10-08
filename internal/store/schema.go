package store

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"

	"github.com/consize-oss/consize/pkg/resource"
)

const (
	MigrationCurrent = "current"
	MigrationApplied = "migrated"
	MigrationPending = "pending"
	MigrationInvalid = "invalid"
)

type MigrationRecord struct {
	FromVersion int    `json:"from_version"`
	ToVersion   int    `json:"to_version"`
	Name        string `json:"name"`
}

type StorageDiagnostics struct {
	Backend         string            `json:"backend"`
	Durable         bool              `json:"durable"`
	Healthy         bool              `json:"healthy"`
	CurrentVersion  int               `json:"current_version"`
	LoadedVersion   int               `json:"loaded_version"`
	TargetVersion   int               `json:"target_version"`
	MigrationStatus string            `json:"migration_status"`
	Migrations      []MigrationRecord `json:"migrations,omitempty"`
	Indexes         []string          `json:"indexes"`
	EntityCounts    map[string]int    `json:"entity_counts,omitempty"`
	Error           string            `json:"error,omitempty"`
}

var (
	credentialURLPattern      = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://[^:/@\s]+:)([^@\s]+)(@)`)
	jsonPasswordPattern       = regexp.MustCompile(`(?i)(["'](?:password|passwd|pwd)["']\s*:\s*)(["'][^"']*["'])`)
	assignmentPasswordPattern = regexp.MustCompile(`(?i)(\b(?:password|passwd|pwd)\b\s*[=:]\s*)([^\s,;&]+)`)
)

// Redacted returns diagnostics that are safe to serialize or display. Error is
// the only free-form field; sanitize it defensively in case a database driver
// includes connection details in a future error message.
func (d StorageDiagnostics) Redacted() StorageDiagnostics {
	d.Error = redactDiagnosticText(d.Error)
	return d
}

// MarshalJSON makes redaction the default for every JSON output path, including
// diagnostics nested in maps or API responses.
func (d StorageDiagnostics) MarshalJSON() ([]byte, error) {
	type storageDiagnosticsAlias StorageDiagnostics
	return json.Marshal(storageDiagnosticsAlias(d.Redacted()))
}

func redactDiagnosticText(value string) string {
	value = jsonPasswordPattern.ReplaceAllString(value, `${1}"[REDACTED]"`)
	value = credentialURLPattern.ReplaceAllString(value, `${1}[REDACTED]${3}`)
	return assignmentPasswordPattern.ReplaceAllString(value, `${1}[REDACTED]`)
}

type DiagnosticsStore interface {
	StorageDiagnostics(context.Context) StorageDiagnostics
}

type RuntimeStore interface {
	JobStore
	DiagnosticsStore
	Close() error
}

func RequiredIndexes() []string {
	return []string{
		"resources(provider,account_id,region,provider_resource_id) unique",
		"recommendations(resource_id,status)",
		"actions(idempotency_key) unique",
		"actions(recommendation_id)",
		"action_events(action_id,created_at)",
		"jobs(action_id) unique",
		"jobs(resource_id) active unique",
		"jobs(state,next_run)",
	}
}

func resourceIdentityKey(res resource.Resource) string {
	key, err := resource.BuildID(res.Identity())
	if err != nil {
		return fmt.Sprintf("invalid:%s", res.ID)
	}
	return key
}

func (m *Memory) rebuildIndexesLocked() error {
	m.resourceIdentityIndex = map[string]string{}
	m.actionIdempotencyIndex = map[string]int64{}
	m.actionsByRecommendation = map[int64][]int64{}
	m.activeJobByResource = map[string]int64{}

	for id, res := range m.resources {
		key := resourceIdentityKey(res)
		if existing, ok := m.resourceIdentityIndex[key]; ok && existing != id {
			return fmt.Errorf("duplicate resource provider identity: %q and %q", existing, id)
		}
		m.resourceIdentityIndex[key] = id
	}
	for id, action := range m.actionRecords {
		if existing, ok := m.actionIdempotencyIndex[action.IdempotencyKey]; ok && existing != id {
			return fmt.Errorf("duplicate action idempotency key %q", action.IdempotencyKey)
		}
		m.actionIdempotencyIndex[action.IdempotencyKey] = id
		m.actionsByRecommendation[action.RecommendationID] = append(m.actionsByRecommendation[action.RecommendationID], id)
	}
	for recommendationID := range m.actionsByRecommendation {
		sort.Slice(m.actionsByRecommendation[recommendationID], func(i, j int) bool {
			return m.actionsByRecommendation[recommendationID][i] < m.actionsByRecommendation[recommendationID][j]
		})
	}
	for id, job := range m.jobs {
		if Terminal(job.State) && job.State != "manual_intervention" {
			continue
		}
		if existing, ok := m.activeJobByResource[job.Resource.ID]; ok && existing != id {
			return fmt.Errorf("resource %q has multiple active or unresolved jobs", job.Resource.ID)
		}
		m.activeJobByResource[job.Resource.ID] = id
	}
	return nil
}
