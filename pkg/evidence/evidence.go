// Package evidence defines immutable, provider-neutral measurement provenance.
package evidence

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

const Version = 1
const (
	Raw         = "raw"
	Aggregated  = "aggregated"
	Derived     = "derived"
	Valid       = "valid"
	Missing     = "missing"
	Stale       = "stale"
	Partial     = "partial"
	Invalid     = "invalid"
	Unavailable = "unavailable"
)

var ErrInvalid = errors.New("invalid evidence")

// Record values use canonical units; OriginalUnit and Scale preserve normalization.
// Value is nil when no trustworthy numeric measurement exists. Measured zero is a non-nil pointer to zero.
type Record struct {
	SchemaVersion int               `json:"schema_version"`
	ID            string            `json:"id"`
	ResourceID    string            `json:"resource_id"`
	PluginID      string            `json:"plugin_id"`
	Signal        string            `json:"signal"`
	Query         string            `json:"query"`
	Kind          string            `json:"kind"`
	Aggregation   string            `json:"aggregation"`
	Unit          string            `json:"unit"`
	OriginalUnit  string            `json:"original_unit"`
	Scale         float64           `json:"scale"`
	Value         *float64          `json:"value"`
	SampleCount   int               `json:"sample_count"`
	WindowStart   time.Time         `json:"window_start"`
	WindowEnd     time.Time         `json:"window_end"`
	CollectedAt   time.Time         `json:"collected_at"`
	FreshUntil    time.Time         `json:"fresh_until"`
	Quality       string            `json:"quality"`
	Parents       []string          `json:"parents,omitempty"`
	Extensions    map[string]string `json:"extensions,omitempty"`
}

func Number(v float64) *float64 { return &v }

// Seal canonicalizes timestamps, validates content, and assigns a content ID.
func Seal(r Record) (Record, error) {
	if r.SchemaVersion != 0 && r.SchemaVersion != Version {
		return Record{}, fmt.Errorf("%w: unsupported schema", ErrInvalid)
	}
	r.SchemaVersion = Version
	r.ID = ""
	r.WindowStart = r.WindowStart.UTC()
	r.WindowEnd = r.WindowEnd.UTC()
	r.CollectedAt = r.CollectedAt.UTC()
	r.FreshUntil = r.FreshUntil.UTC()
	if err := r.validate(); err != nil {
		return Record{}, err
	}
	b, err := json.Marshal(r)
	if err != nil {
		return Record{}, err
	}
	sum := sha256.Sum256(b)
	r.ID = "evidence:v1:" + hex.EncodeToString(sum[:])
	// Detach pointers, maps and slices from the producer.
	b, _ = json.Marshal(r)
	var out Record
	err = json.Unmarshal(b, &out)
	return out, err
}

func (r Record) Validate() error {
	if r.SchemaVersion != Version {
		return fmt.Errorf("%w: unsupported schema", ErrInvalid)
	}
	if err := r.validate(); err != nil {
		return err
	}
	sealed, err := Seal(r)
	if err != nil {
		return err
	}
	if sealed.ID != r.ID {
		return fmt.Errorf("%w: content identity mismatch", ErrInvalid)
	}
	return nil
}
func (r Record) validate() error {
	for _, v := range []string{r.ResourceID, r.PluginID, r.Signal, r.Query, r.Aggregation, r.Unit, r.OriginalUnit} {
		if strings.TrimSpace(v) == "" || len(v) > 8192 {
			return fmt.Errorf("%w: missing or oversized provenance", ErrInvalid)
		}
	}
	if r.Kind != Raw && r.Kind != Aggregated && r.Kind != Derived {
		return fmt.Errorf("%w: kind", ErrInvalid)
	}
	if r.Kind == Derived && len(r.Parents) == 0 {
		return fmt.Errorf("%w: derived parents required", ErrInvalid)
	}
	if r.WindowStart.IsZero() || !r.WindowEnd.After(r.WindowStart) || r.CollectedAt.Before(r.WindowEnd) || r.FreshUntil.Before(r.CollectedAt) {
		return fmt.Errorf("%w: time boundaries", ErrInvalid)
	}
	if r.SampleCount < 0 || r.Scale <= 0 || math.IsNaN(r.Scale) || math.IsInf(r.Scale, 0) {
		return fmt.Errorf("%w: sample count or normalization", ErrInvalid)
	}
	_, unit, scale, err := Normalize(1, r.OriginalUnit)
	if err == nil {
		if unit != r.Unit || scale != r.Scale {
			return fmt.Errorf("%w: normalization provenance mismatch", ErrInvalid)
		}
	} else if r.OriginalUnit != r.Unit || r.Scale != 1 {
		return fmt.Errorf("%w: custom units require explicit prior normalization", ErrInvalid)
	}
	switch r.Quality {
	case Valid, Partial, Stale:
		if r.Value == nil || r.SampleCount == 0 {
			return fmt.Errorf("%w: measured state needs samples and value", ErrInvalid)
		}
	case Missing, Invalid, Unavailable:
		if r.Value != nil {
			return fmt.Errorf("%w: untrustworthy state must have null value", ErrInvalid)
		}
	default:
		return fmt.Errorf("%w: quality", ErrInvalid)
	}
	if r.Value != nil && (math.IsNaN(*r.Value) || math.IsInf(*r.Value, 0)) {
		return fmt.Errorf("%w: nonfinite value", ErrInvalid)
	}
	if len(r.Extensions) > 32 || len(r.Parents) > 64 {
		return fmt.Errorf("%w: extension or parent limit", ErrInvalid)
	}
	size := 0
	for k, v := range r.Extensions {
		if k == "" || len(k) > 128 || len(v) > 1024 {
			return fmt.Errorf("%w: extension size", ErrInvalid)
		}
		size += len(k) + len(v)
	}
	if size > 8192 {
		return fmt.Errorf("%w: extensions exceed 8KiB", ErrInvalid)
	}
	for _, p := range r.Parents {
		if !strings.HasPrefix(p, "evidence:v1:") || len(p) != 76 {
			return fmt.Errorf("%w: parent identity", ErrInvalid)
		}
	}
	return nil
}

// Usable evaluates freshness without changing historical content or identity.
func (r Record) Usable(now time.Time) bool {
	return r.Validate() == nil && r.Quality == Valid && !now.Before(r.CollectedAt) && !now.After(r.FreshUntil)
}

// Normalize preserves the original unit and returns a canonical value and scale.
func Normalize(value float64, unit string) (float64, string, float64, error) {
	scale := 1.0
	canonical := unit
	switch unit {
	case "bytes", "cores", "count", "ratio", "seconds":
	case "KiB":
		canonical = "bytes"
		scale = 1024
	case "MiB":
		canonical = "bytes"
		scale = 1024 * 1024
	case "milliseconds":
		canonical = "seconds"
		scale = 0.001
	case "percent":
		canonical = "ratio"
		scale = 0.01
	case "millicores":
		canonical = "cores"
		scale = 0.001
	default:
		return 0, "", 0, fmt.Errorf("%w: unsupported unit %q", ErrInvalid, unit)
	}
	out := value * scale
	if math.IsNaN(out) || math.IsInf(out, 0) {
		return 0, "", 0, fmt.Errorf("%w: nonfinite normalized value", ErrInvalid)
	}
	return out, canonical, scale, nil
}
