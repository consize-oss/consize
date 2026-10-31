package resource

import (
	"context"
	"errors"
	"time"
)

var (
	ErrNotFound          = errors.New("not found")
	ErrAlreadyExists     = errors.New("resource already exists")
	ErrStaleObservation  = errors.New("stale resource observation")
	ErrInvalidTransition = errors.New("invalid resource lifecycle transition")
	ErrInvalidQuery      = errors.New("invalid resource query")
	ErrScopeMismatch     = errors.New("resource scope mismatch")
)

// Registry is independent of storage and provider discovery implementations.
type Registry interface {
	CreateResource(context.Context, Resource) (Resource, error)
	UpsertResource(context.Context, Resource) (Resource, error)
	GetResource(context.Context, string) (Resource, error)
	ListResources(context.Context) ([]Resource, error)
	ListResourcePage(context.Context, Query) (Page, error)
	UpdateResourceLifecycle(context.Context, LifecycleUpdate) (Resource, error)
	ReconcileResources(context.Context, Reconciliation) (ReconciliationResult, error)
}

type Scope struct {
	Provider       string `json:"provider"`
	Account        string `json:"account"`
	Region         string `json:"region"`
	Type           string `json:"type"`
	SourcePluginID string `json:"source_plugin_id"`
}

type LifecycleUpdate struct {
	Scope      Scope     `json:"scope"`
	ID         string    `json:"id"`
	State      string    `json:"state"`
	ObservedAt time.Time `json:"observed_at"`
}

// Reconciliation describes one complete scan of exactly one scope. It never
// deletes records. Absence marks stale; only explicit confirmations tombstone.
type Reconciliation struct {
	Scope               Scope     `json:"scope"`
	ObservedIDs         []string  `json:"observed_ids"`
	ConfirmedDeletedIDs []string  `json:"confirmed_deleted_ids"`
	ObservedAt          time.Time `json:"observed_at"`
}

type ReconciliationResult struct {
	Stale   []string `json:"stale"`
	Deleted []string `json:"deleted"`
}

type Query struct {
	Provider       string `json:"provider,omitempty"`
	Account        string `json:"account,omitempty"`
	Region         string `json:"region,omitempty"`
	Type           string `json:"type,omitempty"`
	SourcePluginID string `json:"source_plugin_id,omitempty"`
	Environment    string `json:"environment,omitempty"`
	LifecycleState string `json:"lifecycle_state,omitempty"`
	SupportStatus  string `json:"support_status,omitempty"`
	Owner          string `json:"owner,omitempty"`
	Limit          int    `json:"-"`
	Cursor         string `json:"-"`
}

type Page struct {
	Resources  []Resource `json:"resources"`
	NextCursor string     `json:"next_cursor"`
	HasMore    bool       `json:"has_more"`
}
