package plugin

import (
	"context"
	"time"
)

const DiscoveryScanVersion = "discovery-scan-v1"

// DiscoveryScan describes provider enumeration, not an atomic registry write or
// permission to delete absent resources. Empty is authoritative only when Complete
// is true. ScopeDigest binds the provider's immutable selection configuration.
type DiscoveryScan struct {
	Version          string            `json:"version"`
	ID               string            `json:"id"`
	PluginID         string            `json:"plugin_id"`
	Scope            map[string]string `json:"scope"`
	ScopeDigest      string            `json:"scope_digest"`
	StartedAt        time.Time         `json:"started_at"`
	FinishedAt       time.Time         `json:"finished_at"`
	DurationMillis   int64             `json:"duration_ms"`
	Complete         bool              `json:"complete"`
	Count            int               `json:"count"`
	Pages            int               `json:"pages"`
	SnapshotVersions map[string]string `json:"snapshot_versions"`
	ErrorCode        string            `json:"error_code,omitempty"`
}
type DiscoverySnapshot struct {
	Scan         DiscoveryScan         `json:"scan"`
	Observations []ProviderObservation `json:"observations"`
}

// SnapshotDiscoveryPlugin is additive. Older Discover-only plugins do not prove
// scan completeness; consumers must never infer it from an empty legacy slice.
type SnapshotDiscoveryPlugin interface {
	DiscoveryPlugin
	DiscoverSnapshot(context.Context) (DiscoverySnapshot, error)
}

// ScopedDiscoveryPlugin declares the same immutable selection used by snapshots.
// Core scheduling never guesses scan authority from an observation list.
type ScopedDiscoveryPlugin interface {
	SnapshotDiscoveryPlugin
	DiscoveryScope() map[string]string
}
