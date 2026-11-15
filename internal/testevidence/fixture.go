// Package testevidence supplies synthetic, explicitly identified test observations.
package testevidence

import (
	"github.com/consize-oss/consize/pkg/evidence"
	"github.com/consize-oss/consize/pkg/evidence/quality"
	"github.com/consize-oss/consize/pkg/plugin"
	"sort"
	"strconv"
	"strings"
	"time"
)

func Attach(s plugin.MetricsSnapshot) plugin.MetricsSnapshot {
	s.Records = nil
	if s.CollectedAt.IsZero() {
		s.CollectedAt = time.Now().UTC()
	}
	if s.Window == "" {
		s.Window = "30m"
	}
	if s.Source == "" {
		s.Source = "synthetic-test"
	}
	window, err := time.ParseDuration(s.Window)
	if err != nil {
		panic(err)
	}
	if s.Coverage == nil {
		s.Coverage = map[string]plugin.MetricCoverage{}
	}
	stable, changed, count := true, false, 1
	old := s.CollectedAt.Add(-365 * 24 * time.Hour)
	context := quality.Context{Mapping: "exact", ScrapeInterval: "1s", LastObservationAt: &s.CollectedAt, CardinalityStable: &stable, ReplicaCountChanged: &changed, ExpectedReplicas: &count, ObservedReplicas: &count, ExpectedContainers: &count, ObservedContainers: &count, ResourceCreatedAt: &old, ResourceChangedAt: &old}
	keys := make([]string, 0, len(s.Signals))
	for key := range s.Signals {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		v := s.Signals[key]
		number, ok := v.(float64)
		if !ok {
			continue
		}
		stat := ""
		for _, suffix := range []string{"p50", "p95", "p99", "max"} {
			if strings.HasSuffix(key, "_"+suffix) {
				stat = suffix
			}
		}
		if stat == "" {
			continue
		}
		unit := "count"
		if strings.HasPrefix(key, "memory_") {
			unit = "bytes"
		}
		if strings.HasPrefix(key, "cpu_cores") {
			unit = "cores"
		}
		if strings.HasPrefix(key, "latency_seconds") {
			unit = "seconds"
		}
		if strings.HasPrefix(key, "error_rate") {
			unit = "ratio"
		}
		if strings.Contains(key, "ratio") {
			unit = "ratio"
		}
		base := quality.BaseSignal(key)
		coverage, ok := s.Coverage[base]
		if !ok {
			step := window / 60
			coverage = plugin.MetricCoverage{FirstSample: s.CollectedAt.Add(-window), LastSample: s.CollectedAt, Step: step, MaxGap: step, ExpectedPoints: 61, ActualPoints: 61}
			s.Coverage[base] = coverage
		}
		facts := context
		facts.ScrapeInterval = coverage.Step.String()
		facts.ObservedSamples = &coverage.ActualPoints
		facts.FirstObservationAt = &coverage.FirstSample
		facts.SourceMaxGap = coverage.MaxGap.String()
		encodedContext, _ := quality.EncodeContext(facts)
		extensions := map[string]string{quality.ContextKey: encodedContext, "step": coverage.Step.String(), "max_gap": coverage.MaxGap.String(), "first_sample": coverage.FirstSample.Format(time.RFC3339Nano), "last_sample": coverage.LastSample.Format(time.RFC3339Nano), "expected_points": strconv.Itoa(coverage.ExpectedPoints), "actual_points": strconv.Itoa(coverage.ActualPoints)}
		rec, err := evidence.Seal(evidence.Record{ResourceID: s.ResourceID, PluginID: s.PluginID, Signal: key, Query: "synthetic fixture: " + key, Kind: evidence.Aggregated, Aggregation: stat, Unit: unit, OriginalUnit: unit, Scale: 1, Value: evidence.Number(number), SampleCount: coverage.ActualPoints, Extensions: extensions, WindowStart: s.CollectedAt.Add(-window), WindowEnd: s.CollectedAt, CollectedAt: s.CollectedAt, FreshUntil: s.CollectedAt.Add(2 * time.Minute), Quality: evidence.Valid})
		if err != nil {
			panic(err)
		}
		s.Records = append(s.Records, rec)
	}
	return s
}
