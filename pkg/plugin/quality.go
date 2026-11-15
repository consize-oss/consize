package plugin

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/consize-oss/consize/pkg/evidence"
	"github.com/consize-oss/consize/pkg/evidence/quality"
)

// AssessQuality reads quality facts bound to sealed records. Legacy Coverage is
// only a projection: it cannot supply facts absent from immutable provenance.
func (s MetricsSnapshot) AssessQuality(signals []quality.Signal, purpose string, t quality.Thresholds, at time.Time) quality.Assessment {
	// Peaks are independently assessed rather than inheriting p95 coverage.
	expanded := append([]quality.Signal(nil), signals...)
	seen := map[string]bool{}
	for _, signal := range signals {
		seen[signal.Name] = true
	}
	for _, signal := range signals {
		if signal.PeakName != "" && !seen[signal.PeakName] {
			expanded = append(expanded, quality.Signal{Name: signal.PeakName, Unit: signal.Unit})
			seen[signal.PeakName] = true
		}
	}
	signals = expanded
	window, _ := time.ParseDuration(s.Window)
	in := quality.Input{ResourceID: s.ResourceID, PluginID: s.PluginID, CollectedAt: s.CollectedAt, Window: window, Signals: signals, Observations: map[string]quality.Observation{}, Invalid: len(s.Records) > 0 && s.ValidateRecords() != nil}
	records := map[string]*evidence.Record{}
	for i := range s.Records {
		r := s.Records[i]
		records[r.Signal] = &r
	}
	for _, signal := range signals {
		r := records[signal.Name]
		o := quality.Observation{Record: r, Peak: records[signal.PeakName]}
		if r != nil {
			// Projection checks do not turn a partial/stale measurement into invalid
			// provenance; the assessment reports its actual quality separately.
			if !consistentProjection(s.Signals, signal.Name, r.Value) {
				o.Invalid = true
			}
			if o.Peak != nil && !consistentProjection(s.Signals, signal.PeakName, o.Peak.Value) {
				o.Invalid = true
			}
			x := r.Extensions
			if raw := x[quality.ContextKey]; raw != "" {
				context, err := quality.DecodeContext(raw)
				if err != nil {
					o.Invalid = true
				} else {
					o.Context = context
				}
			}
			c := quality.Coverage{}
			var errors []error
			var e error
			c.Step, e = time.ParseDuration(x["step"])
			errors = append(errors, e)
			c.MaxGap, e = time.ParseDuration(x["max_gap"])
			errors = append(errors, e)
			c.ExpectedPoints, e = strconv.Atoi(x["expected_points"])
			errors = append(errors, e)
			c.ActualPoints, e = strconv.Atoi(x["actual_points"])
			errors = append(errors, e)
			c.FirstSample, e = time.Parse(time.RFC3339Nano, x["first_sample"])
			errors = append(errors, e)
			c.LastSample, e = time.Parse(time.RFC3339Nano, x["last_sample"])
			errors = append(errors, e)
			complete := true
			for _, err := range errors {
				if err != nil {
					complete = false
				}
			}
			if complete {
				o.Coverage = &c
				if projection, ok := s.Coverage[quality.BaseSignal(signal.Name)]; ok && (projection.Step != c.Step || projection.MaxGap != c.MaxGap || projection.ExpectedPoints != c.ExpectedPoints || projection.ActualPoints != c.ActualPoints || !projection.FirstSample.Equal(c.FirstSample) || !projection.LastSample.Equal(c.LastSample)) {
					o.Invalid = true
				}
			}
		}
		in.Observations[signal.Name] = o
	}
	return quality.Assess(in, purpose, t, at)
}

func consistentProjection(signals map[string]any, key string, value *float64) bool {
	projected, exists := signals[key]
	if !exists {
		return value == nil
	}
	b, err := json.Marshal(projected)
	if err != nil {
		return false
	}
	if value == nil {
		return string(b) == "null"
	}
	if string(b) == "null" {
		return false
	}
	var number float64
	return json.Unmarshal(b, &number) == nil && number == *value
}
