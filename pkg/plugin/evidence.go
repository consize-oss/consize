package plugin

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/consize-oss/consize/pkg/evidence"
)

// ValidateRecords rejects forged IDs, duplicate signals, or cross-resource provenance.
func (s MetricsSnapshot) ValidateRecords() error {
	if len(s.Records) == 0 || len(s.Records) > 256 {
		return fmt.Errorf("metrics provenance records missing or oversized")
	}
	window, err := time.ParseDuration(s.Window)
	if err != nil || window <= 0 {
		return fmt.Errorf("invalid provenance observation window")
	}
	seen := map[string]bool{}
	for _, r := range s.Records {
		if err := r.Validate(); err != nil {
			return err
		}
		if r.ResourceID != s.ResourceID || r.PluginID != s.PluginID || !r.CollectedAt.Equal(s.CollectedAt) || seen[r.Signal] || r.WindowEnd.Sub(r.WindowStart) != window {
			return fmt.Errorf("metrics provenance identity mismatch or duplicate signal")
		}
		seen[r.Signal] = true
	}
	return nil
}

// Measurement makes typed provenance authoritative over the legacy signals projection.
// Historical verification evaluates quality at collection time, not wall-clock time.
func (s MetricsSnapshot) Measurement(key, unit string, at time.Time) (evidence.Record, error) {
	if err := s.ValidateRecords(); err != nil {
		return evidence.Record{}, err
	}
	for _, r := range s.Records {
		if r.Signal == key {
			value, exists := s.Signals[key]
			if !exists {
				return evidence.Record{}, fmt.Errorf("missing projected signal %s", key)
			}
			b, err := json.Marshal(value)
			if err != nil {
				return evidence.Record{}, err
			}
			var v float64
			if err = json.Unmarshal(b, &v); err != nil || r.Value == nil || v != *r.Value || r.Unit != unit || !r.Usable(at) {
				return evidence.Record{}, fmt.Errorf("unusable or inconsistent evidence for %s", key)
			}
			return r, nil
		}
	}
	return evidence.Record{}, fmt.Errorf("missing provenance for %s", key)
}
