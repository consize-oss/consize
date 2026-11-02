package resource

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode"
)

const CurrentSchemaVersion = 1

const (
	TypeKubernetesDeployment = "kubernetes.deployment"
	TypeAWSRDSInstance       = "aws.rds.instance"
	TypeStorageBucket        = "storage.bucket"
	TypeLLMApplication       = "llm.application"
)

const (
	ProviderKubernetes = "kubernetes"
	ProviderAWS        = "aws"
	ProviderGCP        = "gcp"
	ProviderAzure      = "azure"
	ProviderOpenAI     = "openai"
)

const (
	EnvProduction  = "production"
	EnvStaging     = "staging"
	EnvDevelopment = "development"
)

const (
	CriticalityLow    = "low"
	CriticalityMedium = "medium"
	CriticalityHigh   = "high"
)

const (
	LifecycleActive  = "active"
	LifecycleStale   = "stale"
	LifecycleDeleted = "deleted"
)

const (
	SupportFull        = "fully_supported"
	SupportModelOnly   = "model_only"
	SupportUnsupported = "unsupported"
)

var (
	ErrInvalidResource  = errors.New("invalid resource")
	ErrIdentityConflict = errors.New("resource identity conflict")
)

// Identity contains only immutable provider coordinates. Mutable ownership,
// sizing, utilization, labels, health, and cost must never participate in ID.
type Identity struct {
	Provider           string
	Account            string
	Location           string
	Type               string
	ProviderResourceID string
}

// Resource is the provider-neutral model for anything Consize can observe.
// Provider-specific fields belong in Metadata and CurrentState and cannot
// override validated core identity or lifecycle fields.
type Resource struct {
	SchemaVersion       int               `json:"schema_version"`
	ID                  string            `json:"id"`
	Type                string            `json:"type"`
	Provider            string            `json:"provider"`
	ProviderResourceID  string            `json:"provider_resource_id"`
	Name                string            `json:"name"`
	Environment         string            `json:"environment"`
	Owner               string            `json:"owner"`
	Region              string            `json:"region"`
	Account             string            `json:"account"`
	Criticality         string            `json:"criticality"`
	LifecycleState      string            `json:"lifecycle_state"`
	SupportStatus       string            `json:"support_status"`
	Labels              map[string]string `json:"labels"`
	Metadata            map[string]any    `json:"metadata"`
	CurrentState        map[string]any    `json:"current_state"`
	MonthlyCost         float64           `json:"monthly_cost"`
	SourcePluginID      string            `json:"source_plugin_id"`
	LifecycleObservedAt time.Time         `json:"lifecycle_observed_at,omitempty"`
	ObservedAt          time.Time         `json:"observed_at"`
	FirstSeenAt         time.Time         `json:"first_seen_at"`
	LastSeenAt          time.Time         `json:"last_seen_at"`
	CreatedAt           time.Time         `json:"created_at"`
	UpdatedAt           time.Time         `json:"updated_at"`
}

func (r Resource) Identity() Identity {
	return Identity{Provider: r.Provider, Account: r.Account, Location: r.Region, Type: r.Type, ProviderResourceID: r.ProviderResourceID}
}

// BuildID creates an unambiguous ID from immutable provider identity.
func BuildID(identity Identity) (string, error) {
	identity = normalizeIdentity(identity)
	if err := validateIdentity(identity); err != nil {
		return "", err
	}
	parts := []string{identity.Provider, identity.Account, identity.Location, identity.Type, identity.ProviderResourceID}
	for i := range parts {
		parts[i] = url.QueryEscape(parts[i])
	}
	return "resource:v1:" + strings.Join(parts, ":"), nil
}

func IsCanonicalID(id string) bool { return strings.HasPrefix(id, "resource:v1:") }

// Normalize applies universal defaults without inventing identity fields.
func (r Resource) Normalize(now time.Time) (Resource, error) {
	now = now.UTC()
	r.Provider = strings.ToLower(strings.TrimSpace(r.Provider))
	r.Type = strings.ToLower(strings.TrimSpace(r.Type))
	r.ProviderResourceID = strings.TrimSpace(r.ProviderResourceID)
	r.Account = strings.TrimSpace(r.Account)
	r.Region = strings.TrimSpace(r.Region)
	r.Name = strings.TrimSpace(r.Name)
	r.Environment = strings.ToLower(strings.TrimSpace(r.Environment))
	r.Owner = strings.TrimSpace(r.Owner)
	r.Criticality = strings.ToLower(strings.TrimSpace(r.Criticality))
	r.SourcePluginID = strings.TrimSpace(r.SourcePluginID)
	if r.SchemaVersion == 0 {
		r.SchemaVersion = CurrentSchemaVersion
	}
	if r.ID == "" {
		id, err := BuildID(r.Identity())
		if err != nil {
			return Resource{}, err
		}
		r.ID = id
	}
	if r.LifecycleState == "" {
		r.LifecycleState = LifecycleActive
	}
	r.SupportStatus = SupportForType(r.Type)
	if r.Labels == nil {
		r.Labels = map[string]string{}
	}
	if r.Metadata == nil {
		r.Metadata = map[string]any{}
	}
	if r.CurrentState == nil {
		r.CurrentState = map[string]any{}
	}
	if r.ObservedAt.IsZero() {
		r.ObservedAt = now
	} else {
		r.ObservedAt = r.ObservedAt.UTC()
	}
	if r.FirstSeenAt.IsZero() {
		r.FirstSeenAt = firstNonZero(r.CreatedAt, r.ObservedAt, now)
	}
	if r.LastSeenAt.IsZero() {
		r.LastSeenAt = firstNonZero(r.ObservedAt, r.UpdatedAt, now)
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = r.FirstSeenAt
	}
	r.UpdatedAt = now
	if err := r.Validate(); err != nil {
		return Resource{}, err
	}
	return r, nil
}

// WithDefaults remains for source compatibility. Store paths use Normalize so
// validation failures cannot be discarded.
func (r Resource) WithDefaults(now time.Time) Resource {
	normalized, err := r.Normalize(now)
	if err != nil {
		return r
	}
	return normalized
}

func (r Resource) Validate() error {
	if r.SchemaVersion != CurrentSchemaVersion {
		return fmt.Errorf("%w: unsupported schema_version %d", ErrInvalidResource, r.SchemaVersion)
	}
	if err := validateID(r.ID); err != nil {
		return err
	}
	if err := validateIdentity(r.Identity()); err != nil {
		return err
	}
	if r.Name == "" {
		return fmt.Errorf("%w: name is required", ErrInvalidResource)
	}
	if !oneOf(r.Environment, EnvProduction, EnvStaging, EnvDevelopment) {
		return fmt.Errorf("%w: environment must be production, staging, or development", ErrInvalidResource)
	}
	if r.Owner == "" {
		return fmt.Errorf("%w: owner is required", ErrInvalidResource)
	}
	if !oneOf(r.Criticality, CriticalityLow, CriticalityMedium, CriticalityHigh) {
		return fmt.Errorf("%w: criticality must be low, medium, or high", ErrInvalidResource)
	}
	if !oneOf(r.LifecycleState, LifecycleActive, LifecycleStale, LifecycleDeleted) {
		return fmt.Errorf("%w: lifecycle_state is invalid", ErrInvalidResource)
	}
	if r.SupportStatus != SupportForType(r.Type) {
		return fmt.Errorf("%w: support_status must be %q for type %q", ErrInvalidResource, SupportForType(r.Type), r.Type)
	}
	if r.ObservedAt.IsZero() || r.FirstSeenAt.IsZero() || r.LastSeenAt.IsZero() || r.CreatedAt.IsZero() || r.UpdatedAt.IsZero() {
		return fmt.Errorf("%w: observation and lifecycle timestamps are required", ErrInvalidResource)
	}
	if r.LastSeenAt.Before(r.FirstSeenAt) || r.ObservedAt.After(r.LastSeenAt) || (!r.LifecycleObservedAt.IsZero() && r.LifecycleObservedAt.Before(r.ObservedAt)) {
		return fmt.Errorf("%w: resource timestamps are inconsistent", ErrInvalidResource)
	}
	return nil
}

func SameIdentity(a, b Resource) bool {
	return normalizeIdentity(a.Identity()) == normalizeIdentity(b.Identity())
}

func ValidLifecycleTransition(from, to string) bool {
	if from == to {
		return true
	}
	switch from {
	case LifecycleActive:
		return to == LifecycleStale || to == LifecycleDeleted
	case LifecycleStale:
		return to == LifecycleActive || to == LifecycleDeleted
	case LifecycleDeleted:
		return to == LifecycleActive
	default:
		return false
	}
}

func SupportForType(resourceType string) string {
	switch strings.ToLower(strings.TrimSpace(resourceType)) {
	case TypeKubernetesDeployment:
		return SupportFull
	case TypeAWSRDSInstance, TypeStorageBucket, TypeLLMApplication:
		return SupportModelOnly
	default:
		return SupportUnsupported
	}
}

func normalizeIdentity(identity Identity) Identity {
	identity.Provider = strings.ToLower(strings.TrimSpace(identity.Provider))
	identity.Type = strings.ToLower(strings.TrimSpace(identity.Type))
	identity.Account = strings.TrimSpace(identity.Account)
	identity.Location = strings.TrimSpace(identity.Location)
	identity.ProviderResourceID = strings.TrimSpace(identity.ProviderResourceID)
	return identity
}

func validateIdentity(identity Identity) error {
	identity = normalizeIdentity(identity)
	fields := []struct {
		name  string
		value string
	}{
		{"provider", identity.Provider},
		{"account", identity.Account},
		{"region", identity.Location},
		{"type", identity.Type},
		{"provider_resource_id", identity.ProviderResourceID},
	}
	for _, field := range fields {
		if field.value == "" {
			return fmt.Errorf("%w: %s is required", ErrInvalidResource, field.name)
		}
		if len(field.value) > 512 || strings.IndexFunc(field.value, unicode.IsControl) >= 0 {
			return fmt.Errorf("%w: %s contains invalid characters or exceeds its limit", ErrInvalidResource, field.name)
		}
	}
	return nil
}

func validateID(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("%w: id is required", ErrInvalidResource)
	}
	if len(id) > 2048 || strings.IndexFunc(id, unicode.IsControl) >= 0 {
		return fmt.Errorf("%w: id contains invalid characters or exceeds its limit", ErrInvalidResource)
	}
	return nil
}

func firstNonZero(values ...time.Time) time.Time {
	for _, value := range values {
		if !value.IsZero() {
			return value.UTC()
		}
	}
	return time.Time{}
}

func oneOf(value string, options ...string) bool {
	for _, option := range options {
		if value == option {
			return true
		}
	}
	return false
}
