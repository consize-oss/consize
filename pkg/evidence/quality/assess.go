package quality

import (
	"math"
	"strings"
	"time"

	"github.com/consize-oss/consize/pkg/evidence"
)

// Assess evaluates only supplied observations. Missing facts never become zero.
func Assess(in Input, purpose string, thresholds Thresholds, at time.Time) Assessment {
	a := Assessment{Version: Version, Purpose: purpose, ResourceID: in.ResourceID, PluginID: in.PluginID, AssessedAt: at.UTC(), State: "valid", Eligible: true, Thresholds: thresholds, EvidenceIDs: []string{}, Dimensions: []Dimension{}}
	setState := func(state string) {
		priority := map[string]int{"valid": 0, "unknown": 1, "partial": 2, "stale": 3, "missing": 4, "unavailable": 5, "invalid": 6}
		if priority[state] > priority[a.State] {
			a.State = state
		}
	}
	add := func(name, signal, status, code, message string, mandatory bool, observed, required *float64, unit string) {
		a.Dimensions = append(a.Dimensions, Dimension{Name: name, Signal: signal, Status: status, Mandatory: mandatory, Code: code, Message: message, Observed: observed, Required: required, Unit: unit})
		if mandatory && status != "pass" {
			a.Eligible = false
			if status == "unknown" {
				setState("unknown")
			} else {
				setState("partial")
			}
			if code == "stale_collection" || code == "stale_evidence" || code == "stale_observations" {
				setState("stale")
			}
		}
	}
	check := func(name, signal string, ok bool, code, message string, mandatory bool, observed, required *float64, unit string) {
		status := "fail"
		if ok {
			status = "pass"
			code = "satisfied"
			message = "Required evidence is present and within threshold."
		}
		add(name, signal, status, code, message, mandatory, observed, required, unit)
	}
	resolved, thresholdError := Resolve(Config{MinHistory: thresholds.MinHistory, MaxAge: thresholds.MaxAge, MinimumCoverage: thresholds.MinimumCoverage, MinimumSamples: thresholds.MinimumSamples, MaximumGapSteps: thresholds.MaximumGapSteps, MaximumPeakRatio: thresholds.MaximumPeakRatio}, purpose, false, false)
	thresholds = resolved
	a.Thresholds = resolved
	history, e1 := time.ParseDuration(thresholds.MinHistory)
	age, e2 := time.ParseDuration(thresholds.MaxAge)
	if thresholdError != nil || e1 != nil || e2 != nil || at.IsZero() || in.Invalid || in.ResourceID == "" || in.PluginID == "" || in.Window <= 0 || in.Window > 365*24*time.Hour || len(in.Signals) == 0 || len(in.Signals) > 256 {
		add("provenance", "", "fail", "invalid_evidence", "Evidence identity, timestamps or configuration are invalid.", true, nil, nil, "")
		a.State = "invalid"
		return a
	}
	check("collection_freshness", "", !in.CollectedAt.IsZero() && !in.CollectedAt.After(at) && at.Sub(in.CollectedAt) <= age, "stale_collection", "Collection time is missing, in the future or too old.", true, number(at.Sub(in.CollectedAt).Seconds()), number(age.Seconds()), "seconds")
	for _, signal := range in.Signals {
		o := in.Observations[signal.Name]
		r := o.Record
		if r == nil {
			add("availability", signal.Name, "fail", "missing_signal", "No measurement was recorded.", true, nil, nil, "")
			setState("missing")
			continue
		}
		valid := (o.Context == nil || o.Context.Validate() == nil) && !o.Invalid && r.Validate() == nil && r.ResourceID == in.ResourceID && r.PluginID == in.PluginID && r.Signal == signal.Name && r.Unit == signal.Unit && r.CollectedAt.Equal(in.CollectedAt) && r.WindowEnd.Sub(r.WindowStart) == in.Window
		check("provenance", signal.Name, valid, "invalid_provenance", "Measurement provenance or projection is inconsistent.", true, nil, nil, "")
		if !valid {
			setState("invalid")
			continue
		}
		a.EvidenceIDs = append(a.EvidenceIDs, r.ID)
		if r.Quality != evidence.Valid {
			setState(r.Quality)
		}
		check("availability", signal.Name, r.Quality == evidence.Valid && r.Value != nil && *r.Value >= 0, "unusable_measurement", "Measurement is missing, partial, stale, invalid or unavailable.", true, nil, nil, "")
		check("freshness", signal.Name, !at.After(r.FreshUntil) && !r.CollectedAt.After(at), "stale_evidence", "Measurement has expired or is not usable at assessment time.", true, nil, nil, "")
		c := o.Coverage
		if c == nil || c.Step <= 0 || c.Step > in.Window || c.Step > 24*time.Hour || c.FirstSample.IsZero() || c.LastSample.IsZero() {
			add("coverage", signal.Name, "unknown", "coverage_unknown", "Timestamped coverage is unavailable.", true, nil, nil, "")
		} else {
			expected := int64(in.Window/c.Step) + 1
			counts := expected >= 2 && int64(c.ExpectedPoints) == expected && c.ActualPoints >= thresholds.MinimumSamples && c.ActualPoints <= c.ExpectedPoints && r.SampleCount == c.ActualPoints
			check("sample_count", signal.Name, counts, "sparse_samples", "Sample counts are insufficient or inconsistent with the query interval.", true, number(float64(c.ActualPoints)), number(float64(thresholds.MinimumSamples)), "points")
			a.Dimensions[len(a.Dimensions)-1].Expected = number(float64(expected))
			ratio := 0.0
			if c.ExpectedPoints > 0 {
				ratio = float64(c.ActualPoints) / float64(c.ExpectedPoints)
			}
			check("coverage", signal.Name, counts && ratio >= thresholds.MinimumCoverage, "sparse_coverage", "Too few expected query points were observed.", true, number(ratio), number(thresholds.MinimumCoverage), "ratio")
			span := c.LastSample.Sub(c.FirstSample)
			times := !c.LastSample.Before(c.FirstSample) && !c.FirstSample.Before(r.WindowStart) && !c.LastSample.After(r.WindowEnd) && !r.WindowEnd.After(in.CollectedAt) && in.CollectedAt.Sub(c.LastSample) <= 2*c.Step && c.FirstSample.Sub(r.WindowStart) <= 2*c.Step
			check("continuity", signal.Name, times, "incomplete_window", "Observed timestamps do not cover the claimed window.", true, nil, nil, "")
			check("history", signal.Name, in.Window >= history && span >= history-c.Step && (history == 0 || c.Step <= history/24), "insufficient_history", "Observed history is too short or coarse.", true, number(span.Seconds()), number(history.Seconds()), "seconds")
			check("gaps", signal.Name, c.MaxGap >= c.Step && c.MaxGap <= time.Duration(thresholds.MaximumGapSteps)*c.Step, "excessive_gaps", "Sample gaps are unknown or exceed the threshold.", true, number(c.MaxGap.Seconds()), number((time.Duration(thresholds.MaximumGapSteps) * c.Step).Seconds()), "seconds")
		}
		context := o.Context
		if context == nil || context.LastObservationAt == nil {
			add("observation_freshness", signal.Name, "unknown", "observation_time_unknown", "Actual source observation time is unknown; query evaluation time is insufficient.", true, nil, nil, "seconds")
		} else {
			observed := *context.LastObservationAt
			check("observation_freshness", signal.Name, !observed.IsZero() && !observed.Before(r.WindowStart) && !observed.After(r.WindowEnd) && at.Sub(observed) <= age, "stale_observations", "Actual source observations are stale or outside the measurement window.", true, number(at.Sub(observed).Seconds()), number(age.Seconds()), "seconds")
		}
		if context == nil || context.ScrapeInterval == "" {
			add("sampling_resolution", signal.Name, "unknown", "scrape_interval_unknown", "Query points do not establish independent scrape observations.", thresholds.RequireTopology, nil, nil, "seconds")
		} else {
			scrape, _ := time.ParseDuration(context.ScrapeInterval)
			resolution := scrape
			if c != nil && c.Step > resolution {
				resolution = c.Step
			}
			check("sampling_resolution", signal.Name, resolution > 0 && int64(in.Window/resolution)+1 >= int64(thresholds.MinimumSamples) && (history == 0 || resolution <= history/24), "coarse_observations", "Scrape or query resolution is too coarse for the required history.", thresholds.RequireTopology, number(resolution.Seconds()), nil, "seconds")
		}
		if context == nil || context.FirstObservationAt == nil || context.LastObservationAt == nil || context.ScrapeInterval == "" || context.SourceMaxGap == "" {
			add("source_continuity", signal.Name, "unknown", "source_continuity_unknown", "Underlying source coverage boundaries and gaps are unknown.", true, nil, nil, "seconds")
		} else {
			scrape, _ := time.ParseDuration(context.ScrapeInterval)
			gap, _ := time.ParseDuration(context.SourceMaxGap)
			first, last := *context.FirstObservationAt, *context.LastObservationAt
			check("source_continuity", signal.Name, !first.IsZero() && !first.Before(r.WindowStart) && !last.Before(first) && !last.After(r.WindowEnd) && first.Sub(r.WindowStart) <= 2*scrape && r.WindowEnd.Sub(last) <= 2*scrape, "incomplete_source_window", "Actual source samples do not span the claimed window.", true, nil, nil, "")
			check("source_gaps", signal.Name, gap >= scrape && gap <= time.Duration(thresholds.MaximumGapSteps)*scrape, "source_scrape_outage", "Underlying source gaps exceed the allowed scrape intervals.", true, number(gap.Seconds()), number((time.Duration(thresholds.MaximumGapSteps) * scrape).Seconds()), "seconds")
		}
		if context == nil || context.ObservedSamples == nil || context.ScrapeInterval == "" {
			add("source_coverage", signal.Name, "unknown", "source_samples_unknown", "Distinct underlying source observations are unknown.", true, nil, number(thresholds.MinimumCoverage), "ratio")
		} else {
			scrape, _ := time.ParseDuration(context.ScrapeInterval)
			expected := int64(in.Window/scrape) + 1
			observed := int64(*context.ObservedSamples)
			ratio := 0.0
			if expected > 0 {
				ratio = float64(observed) / float64(expected)
			}
			check("source_samples", signal.Name, expected >= 2 && observed >= int64(thresholds.MinimumSamples) && observed <= expected, "source_samples_inconsistent", "Source sample counts are insufficient or inconsistent.", true, number(float64(observed)), number(float64(thresholds.MinimumSamples)), "samples")
			a.Dimensions[len(a.Dimensions)-1].Expected = number(float64(expected))
			check("source_coverage", signal.Name, expected >= 2 && observed >= int64(thresholds.MinimumSamples) && observed <= expected && ratio >= thresholds.MinimumCoverage, "source_sample_gaps", "Underlying source samples are sparse or inconsistent with the scrape interval.", true, number(ratio), number(thresholds.MinimumCoverage), "ratio")
		}
		if context == nil {
			for _, dim := range []string{"mapping", "cardinality", "replicas", "containers", "workload_age", "workload_change"} {
				mandatory := thresholds.RequireTopology || dim == "mapping"
				add(dim, signal.Name, "unknown", dim+"_unknown", "The producer did not supply this observation.", mandatory, nil, nil, "")
			}
		} else {
			check("mapping", signal.Name, context.Mapping == "exact", "ambiguous_mapping", "Resource mapping is ambiguous or unknown.", true, nil, nil, "")
			for _, v := range []struct {
				name  string
				value *bool
				want  bool
			}{{"cardinality", context.CardinalityStable, true}, {"replica_change", context.ReplicaCountChanged, false}} {
				if v.value == nil {
					add(v.name, signal.Name, "unknown", v.name+"_unknown", "Historical topology is unknown.", thresholds.RequireTopology, nil, nil, "")
				} else {
					check(v.name, signal.Name, *v.value == v.want, v.name+"_changed", "Resource membership changed within the observation window.", thresholds.RequireTopology, nil, nil, "")
				}
			}
			for _, v := range []struct {
				name               string
				expected, observed *int
			}{{"replicas", context.ExpectedReplicas, context.ObservedReplicas}, {"containers", context.ExpectedContainers, context.ObservedContainers}} {
				if v.expected == nil || v.observed == nil {
					add(v.name, signal.Name, "unknown", v.name+"_unknown", "Expected and observed membership are unknown.", thresholds.RequireTopology, nil, nil, "")
				} else {
					check(v.name, signal.Name, *v.expected > 0 && *v.observed == *v.expected, "partial_"+v.name, "Some expected members lack observations.", thresholds.RequireTopology, number(float64(*v.observed)), number(float64(*v.expected)), "members")
				}
			}
			for _, v := range []struct {
				name      string
				timestamp *time.Time
			}{{"workload_age", context.ResourceCreatedAt}, {"workload_change", context.ResourceChangedAt}} {
				mandatory := purpose == Recommendation
				if v.timestamp == nil {
					add(v.name, signal.Name, "unknown", v.name+"_unknown", "Workload history is unknown.", mandatory, nil, nil, "seconds")
				} else {
					elapsed := in.CollectedAt.Sub(*v.timestamp)
					check(v.name, signal.Name, !v.timestamp.IsZero() && elapsed >= history, "recent_"+v.name, "Workload is new or changed within the required history.", mandatory, number(elapsed.Seconds()), number(history.Seconds()), "seconds")
				}
			}
		}
		if signal.PeakName != "" && purpose == Recommendation {
			p := o.Peak
			ok := r.Aggregation == "p95" && r.Value != nil && *r.Value > 0 && p != nil && p.Validate() == nil && p.Usable(at) && p.Aggregation == "max" && p.Signal == signal.PeakName && p.SampleCount == r.SampleCount && p.Unit == r.Unit && p.ResourceID == r.ResourceID && p.PluginID == r.PluginID && p.CollectedAt.Equal(r.CollectedAt) && p.WindowStart.Equal(r.WindowStart) && p.WindowEnd.Equal(r.WindowEnd) && p.Value != nil && *p.Value >= *r.Value
			if p != nil && p.Validate() == nil {
				a.EvidenceIDs = append(a.EvidenceIDs, p.ID)
			}
			var ratio *float64
			if ok {
				value := *p.Value / *r.Value
				ok = !math.IsInf(value, 0) && !math.IsNaN(value) && value <= thresholds.MaximumPeakRatio
				if !math.IsInf(value, 0) && !math.IsNaN(value) {
					ratio = number(value)
				}
			}
			check("peak", signal.Name, ok, "burst_sensitive", "Observed peak is missing, inconsistent or exceeds the peak-to-p95 threshold.", true, ratio, number(thresholds.MaximumPeakRatio), "ratio")
		}
	}
	if !a.Eligible && a.State == "valid" {
		a.State = "partial"
	}
	return a
}
func number(v float64) *float64 { return &v }
func BaseSignal(name string) string {
	for _, suffix := range []string{"_p50", "_p95", "_p99", "_max"} {
		if strings.HasSuffix(name, suffix) {
			return strings.TrimSuffix(name, suffix)
		}
	}
	return name
}
