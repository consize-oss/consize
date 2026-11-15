// Package quality defines deterministic, provider-neutral evidence eligibility.
// It does not predict future demand or authorize infrastructure changes.
package quality

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"time"

	"github.com/consize-oss/consize/pkg/evidence"
)

const Version = "evidence-quality-v1"
const ContextKey = "quality_context_v1"
const (
	Reporting      = "reporting"
	Recommendation = "recommendation"
	Verification   = "verification"
)

// Config contains optional thresholds. Zero values select conservative defaults.
// Durations are strings at the configuration/API boundary, not nanosecond counts.
type Config struct {
	MinHistory       string  `json:"min_history,omitempty"`
	MaxAge           string  `json:"max_age,omitempty"`
	MinimumCoverage  float64 `json:"minimum_coverage,omitempty"`
	MinimumSamples   int     `json:"minimum_samples,omitempty"`
	MaximumGapSteps  int     `json:"maximum_gap_steps,omitempty"`
	MaximumPeakRatio float64 `json:"maximum_peak_ratio,omitempty"`
}

type Thresholds struct {
	MinHistory       string  `json:"min_history"`
	MaxAge           string  `json:"max_age"`
	MinimumCoverage  float64 `json:"minimum_coverage"`
	MinimumSamples   int     `json:"minimum_samples"`
	MaximumGapSteps  int     `json:"maximum_gap_steps"`
	MaximumPeakRatio float64 `json:"maximum_peak_ratio"`
	RequireTopology  bool    `json:"require_topology"`
}

func Resolve(c Config, purpose string, production, lab bool) (Thresholds, error) {
	t := Thresholds{MinHistory: "0s", MaxAge: "2m", MinimumCoverage: .90, MinimumSamples: 2, MaximumGapSteps: 3, MaximumPeakRatio: 2, RequireTopology: purpose != Reporting}
	if purpose != Reporting && purpose != Recommendation && purpose != Verification {
		return t, fmt.Errorf("unsupported quality purpose")
	}
	if purpose == Recommendation {
		t.MinHistory = "24h"
		if production {
			t.MinHistory = "168h"
		}
		if lab && !production {
			t.MinHistory = "30m"
		}
	}
	if c.MinHistory != "" {
		t.MinHistory = c.MinHistory
	}
	if c.MaxAge != "" {
		t.MaxAge = c.MaxAge
	}
	if c.MinimumCoverage != 0 {
		t.MinimumCoverage = c.MinimumCoverage
	}
	if c.MinimumSamples != 0 {
		t.MinimumSamples = c.MinimumSamples
	}
	if c.MaximumGapSteps != 0 {
		t.MaximumGapSteps = c.MaximumGapSteps
	}
	if c.MaximumPeakRatio != 0 {
		t.MaximumPeakRatio = c.MaximumPeakRatio
	}
	history, e1 := time.ParseDuration(t.MinHistory)
	age, e2 := time.ParseDuration(t.MaxAge)
	if e1 != nil || e2 != nil || history < 0 || history > 365*24*time.Hour || purpose == Recommendation && history < 30*time.Minute || age <= 0 || age > 24*time.Hour || math.IsNaN(t.MinimumCoverage) || math.IsInf(t.MinimumCoverage, 0) || t.MinimumCoverage < .5 || t.MinimumCoverage > 1 || t.MinimumSamples < 2 || t.MinimumSamples > 1000000 || t.MaximumGapSteps < 1 || t.MaximumGapSteps > 100 || math.IsNaN(t.MaximumPeakRatio) || math.IsInf(t.MaximumPeakRatio, 0) || t.MaximumPeakRatio < 1 || t.MaximumPeakRatio > 100 {
		return Thresholds{}, fmt.Errorf("invalid evidence quality thresholds")
	}
	return t, nil
}

// Context is producer-observed topology during the measurement window. Nil
// counts/flags mean unknown, including when aggregated queries erased labels.
// Producers bind these facts to each record using the sealed extension key.
// Discovery first_seen/last_seen and current replica count are not history.
type Context struct {
	Mapping             string     `json:"mapping"` // exact, ambiguous, unknown
	CardinalityStable   *bool      `json:"cardinality_stable,omitempty"`
	ReplicaCountChanged *bool      `json:"replica_count_changed,omitempty"`
	ExpectedReplicas    *int       `json:"expected_replicas,omitempty"`
	ObservedReplicas    *int       `json:"observed_replicas,omitempty"`
	ExpectedContainers  *int       `json:"expected_containers,omitempty"`
	ObservedContainers  *int       `json:"observed_containers,omitempty"`
	ResourceCreatedAt   *time.Time `json:"resource_created_at,omitempty"`
	ResourceChangedAt   *time.Time `json:"resource_changed_at,omitempty"`
	FirstObservationAt  *time.Time `json:"first_observation_at,omitempty"`
	SourceMaxGap        string     `json:"source_max_gap,omitempty"`
	ObservedSamples     *int       `json:"observed_samples,omitempty"`
	LastObservationAt   *time.Time `json:"last_observation_at,omitempty"`
	ScrapeInterval      string     `json:"scrape_interval,omitempty"`
}

func EncodeContext(c Context) (string, error) {
	if err := c.Validate(); err != nil {
		return "", err
	}
	b, err := json.Marshal(c)
	if len(b) > 1024 {
		return "", fmt.Errorf("quality context exceeds provenance budget")
	}
	return string(b), err
}
func (c Context) Validate() error {
	if c.Mapping != "exact" && c.Mapping != "ambiguous" && c.Mapping != "unknown" {
		return fmt.Errorf("invalid mapping certainty")
	}
	for _, n := range []*int{c.ExpectedReplicas, c.ObservedReplicas, c.ExpectedContainers, c.ObservedContainers} {
		if n != nil && (*n < 0 || *n > 1000000) {
			return fmt.Errorf("invalid topology count")
		}
	}
	if c.SourceMaxGap != "" {
		d, err := time.ParseDuration(c.SourceMaxGap)
		if err != nil || d <= 0 || d > 365*24*time.Hour {
			return fmt.Errorf("invalid source gap")
		}
	}
	if c.ObservedSamples != nil && (*c.ObservedSamples < 0 || *c.ObservedSamples > 1000000000) {
		return fmt.Errorf("invalid source sample count")
	}
	if c.ScrapeInterval != "" {
		d, err := time.ParseDuration(c.ScrapeInterval)
		if err != nil || d <= 0 || d > 24*time.Hour {
			return fmt.Errorf("invalid scrape interval")
		}
	}
	return nil
}

type Coverage struct {
	FirstSample, LastSample      time.Time
	ExpectedPoints, ActualPoints int
	Step, MaxGap                 time.Duration
}
type Signal struct{ Name, Unit, PeakName string }
type Observation struct {
	Record   *evidence.Record
	Peak     *evidence.Record
	Coverage *Coverage
	Context  *Context
	Invalid  bool
}
type Input struct {
	ResourceID, PluginID string
	CollectedAt          time.Time
	Window               time.Duration
	Signals              []Signal
	Observations         map[string]Observation
	Invalid              bool
}
type Dimension struct {
	Name      string   `json:"name"`
	Signal    string   `json:"signal"`
	Status    string   `json:"status"` // pass, fail, unknown
	Mandatory bool     `json:"mandatory"`
	Code      string   `json:"code"`
	Message   string   `json:"message"`
	Observed  *float64 `json:"observed"`
	Expected  *float64 `json:"expected,omitempty"`
	Required  *float64 `json:"required"`
	Unit      string   `json:"unit"`
}
type Assessment struct {
	Version     string      `json:"version"`
	Purpose     string      `json:"purpose"`
	ResourceID  string      `json:"resource_id"`
	PluginID    string      `json:"plugin_id"`
	AssessedAt  time.Time   `json:"assessed_at"`
	State       string      `json:"state"`
	Eligible    bool        `json:"eligible"`
	Thresholds  Thresholds  `json:"thresholds"`
	EvidenceIDs []string    `json:"evidence_ids"`
	Dimensions  []Dimension `json:"dimensions"`
}

// BlockedError preserves safe structured diagnostics without copying provider
// payloads, queries, URLs or credentials into an error message.
type BlockedError struct{ Assessment Assessment }

func (e *BlockedError) Error() string { return "evidence quality does not satisfy mandatory gates" }

// DecodeContext accepts only the bounded, versioned producer contract.
func DecodeContext(raw string) (*Context, error) {
	if len(raw) == 0 || len(raw) > 1024 {
		return nil, fmt.Errorf("invalid quality context size")
	}
	var c Context
	decoder := json.NewDecoder(bytes.NewBufferString(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&c); err != nil {
		return nil, fmt.Errorf("invalid quality context")
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF || c.Validate() != nil {
		return nil, fmt.Errorf("invalid quality context")
	}
	return &c, nil
}
