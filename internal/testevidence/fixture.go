// Package testevidence supplies synthetic, explicitly identified test observations.
package testevidence

import (
	"github.com/consize-oss/consize/pkg/evidence"
	"github.com/consize-oss/consize/pkg/plugin"
	"sort"
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
		rec, err := evidence.Seal(evidence.Record{ResourceID: s.ResourceID, PluginID: s.PluginID, Signal: key, Query: "synthetic fixture: " + key, Kind: evidence.Aggregated, Aggregation: stat, Unit: unit, OriginalUnit: unit, Scale: 1, Value: evidence.Number(number), SampleCount: 60, WindowStart: s.CollectedAt.Add(-window), WindowEnd: s.CollectedAt, CollectedAt: s.CollectedAt, FreshUntil: s.CollectedAt.Add(2 * time.Minute), Quality: evidence.Valid})
		if err != nil {
			panic(err)
		}
		s.Records = append(s.Records, rec)
	}
	return s
}
