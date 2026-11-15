package plugin_test

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/consize-oss/consize/internal/testevidence"
	"github.com/consize-oss/consize/pkg/evidence"
	"github.com/consize-oss/consize/pkg/evidence/quality"
	"github.com/consize-oss/consize/pkg/plugin"
)

func qualityFixture() plugin.MetricsSnapshot {
	return testevidence.Attach(plugin.MetricsSnapshot{ResourceID: "resource", PluginID: "metrics", Window: "168h", CollectedAt: time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC), Signals: map[string]any{"memory_working_set_bytes_p95": 100., "memory_working_set_bytes_max": 120.}})
}
func changeRecord(t *testing.T, s *plugin.MetricsSnapshot, fn func(*evidence.Record)) {
	t.Helper()
	for i := range s.Records {
		r := s.Records[i]
		fn(&r)
		sealed, err := evidence.Seal(r)
		if err != nil {
			t.Fatal(err)
		}
		s.Records[i] = sealed
	}
}
func changeContext(t *testing.T, s *plugin.MetricsSnapshot, fn func(*quality.Context)) {
	changeRecord(t, s, func(r *evidence.Record) {
		var c quality.Context
		if err := json.Unmarshal([]byte(r.Extensions[quality.ContextKey]), &c); err != nil {
			t.Fatal(err)
		}
		fn(&c)
		raw, err := quality.EncodeContext(c)
		if err != nil {
			t.Fatal(err)
		}
		r.Extensions[quality.ContextKey] = raw
	})
}
func assess(s plugin.MetricsSnapshot, purpose string, at time.Time) quality.Assessment {
	t, _ := quality.Resolve(quality.Config{}, purpose, true, false)
	return s.AssessQuality([]quality.Signal{{Name: "memory_working_set_bytes_p95", Unit: "bytes", PeakName: "memory_working_set_bytes_max"}}, purpose, t, at)
}
func TestQualityDistinguishesFailures(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		mutate     func(*testing.T, *plugin.MetricsSnapshot)
		advance    time.Duration
	}{
		{"complete", "", func(*testing.T, *plugin.MetricsSnapshot) {}, 0},
		{"stale", "stale_collection", func(*testing.T, *plugin.MetricsSnapshot) {}, 3 * time.Minute},
		{"sparse", "sparse_samples", func(t *testing.T, s *plugin.MetricsSnapshot) {
			s.Coverage = nil
			changeRecord(t, s, func(r *evidence.Record) { r.SampleCount = 1; r.Extensions["actual_points"] = "1" })
		}, 0},
		{"gapped", "excessive_gaps", func(t *testing.T, s *plugin.MetricsSnapshot) {
			s.Coverage = nil
			changeRecord(t, s, func(r *evidence.Record) { r.Extensions["max_gap"] = "48h" })
		}, 0},
		{"new workload", "recent_workload_age", func(t *testing.T, s *plugin.MetricsSnapshot) {
			changeContext(t, s, func(c *quality.Context) { recent := s.CollectedAt.Add(-time.Hour); c.ResourceCreatedAt = &recent })
		}, 0},
		{"replicas changed", "replica_change_changed", func(t *testing.T, s *plugin.MetricsSnapshot) {
			changeContext(t, s, func(c *quality.Context) { changed := true; c.ReplicaCountChanged = &changed })
		}, 0},
		{"partial containers", "partial_containers", func(t *testing.T, s *plugin.MetricsSnapshot) {
			changeContext(t, s, func(c *quality.Context) { zero := 0; c.ObservedContainers = &zero })
		}, 0},
		{"ambiguous mapping", "ambiguous_mapping", func(t *testing.T, s *plugin.MetricsSnapshot) {
			changeContext(t, s, func(c *quality.Context) { c.Mapping = "ambiguous" })
		}, 0},
		{"cardinality change", "cardinality_changed", func(t *testing.T, s *plugin.MetricsSnapshot) {
			changeContext(t, s, func(c *quality.Context) { stable := false; c.CardinalityStable = &stable })
		}, 0},
		{"missing topology", "replicas_unknown", func(t *testing.T, s *plugin.MetricsSnapshot) {
			changeRecord(t, s, func(r *evidence.Record) { delete(r.Extensions, quality.ContextKey) })
		}, 0},
		{"forged projection", "invalid_provenance", func(t *testing.T, s *plugin.MetricsSnapshot) { s.Signals["memory_working_set_bytes_p95"] = 999. }, 0},
		{"forged provenance", "invalid_evidence", func(t *testing.T, s *plugin.MetricsSnapshot) { s.Records[0].Query = "altered" }, 0},
		{"unavailable", "unusable_measurement", func(t *testing.T, s *plugin.MetricsSnapshot) {
			changeRecord(t, s, func(r *evidence.Record) {
				r.Quality = evidence.Unavailable
				s.Signals[r.Signal] = nil
				r.Value = nil
				r.SampleCount = 0
			})
		}, 0},
		{"stale source despite recent query", "stale_observations", func(t *testing.T, s *plugin.MetricsSnapshot) {
			changeContext(t, s, func(c *quality.Context) { old := s.CollectedAt.Add(-3 * time.Minute); c.LastObservationAt = &old })
		}, 0},
		{"recent resource change", "recent_workload_change", func(t *testing.T, s *plugin.MetricsSnapshot) {
			changeContext(t, s, func(c *quality.Context) { recent := s.CollectedAt.Add(-time.Hour); c.ResourceChangedAt = &recent })
		}, 0},
		{"concentrated samples", "incomplete_window", func(t *testing.T, s *plugin.MetricsSnapshot) {
			s.Coverage = nil
			changeRecord(t, s, func(r *evidence.Record) {
				r.Extensions["first_sample"] = s.CollectedAt.Add(-time.Hour).Format(time.RFC3339Nano)
			})
		}, 0},
		{"missing sealed coverage", "coverage_unknown", func(t *testing.T, s *plugin.MetricsSnapshot) {
			changeRecord(t, s, func(r *evidence.Record) { delete(r.Extensions, "step") })
		}, 0},
		{"unknown context fields", "invalid_provenance", func(t *testing.T, s *plugin.MetricsSnapshot) {
			changeRecord(t, s, func(r *evidence.Record) { r.Extensions[quality.ContextKey] = `{"mapping":"exact","invented":true}` })
		}, 0},
		{"trailing context JSON", "invalid_provenance", func(t *testing.T, s *plugin.MetricsSnapshot) {
			changeRecord(t, s, func(r *evidence.Record) { r.Extensions[quality.ContextKey] += " {}" })
		}, 0},
		{"partially missing signal", "missing_signal", func(t *testing.T, s *plugin.MetricsSnapshot) {
			for i, r := range s.Records {
				if r.Signal == "memory_working_set_bytes_p95" {
					s.Records = append(s.Records[:i], s.Records[i+1:]...)
					break
				}
			}
		}, 0},

		{"dense queries sparse source", "source_sample_gaps", func(t *testing.T, s *plugin.MetricsSnapshot) {
			changeContext(t, s, func(c *quality.Context) { few := 2; c.ObservedSamples = &few })
		}, 0},

		{"raw scrape outage hidden by queries", "source_scrape_outage", func(t *testing.T, s *plugin.MetricsSnapshot) {
			changeContext(t, s, func(c *quality.Context) { c.SourceMaxGap = "24h" })
		}, 0},

		{"coarse scrapes", "coarse_observations", func(t *testing.T, s *plugin.MetricsSnapshot) {
			changeContext(t, s, func(c *quality.Context) { c.ScrapeInterval = "24h" })
		}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := qualityFixture()
			tc.mutate(t, &s)
			at := s.CollectedAt.Add(tc.advance)
			got := assess(s, quality.Recommendation, at)
			if !reflect.DeepEqual(got, assess(s, quality.Recommendation, at)) {
				t.Fatal("assessment is not deterministic")
			}
			if got.Eligible != (tc.code == "") {
				t.Fatalf("unexpected eligibility: %+v", got)
			}
			if tc.code != "" {
				found := false
				for _, d := range got.Dimensions {
					if d.Code == tc.code && d.Status != "pass" {
						found = true
					}
				}
				if !found {
					t.Fatalf("missing %s: %+v", tc.code, got)
				}
			}
			encoded, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), "synthetic fixture") {
				t.Fatal("query leaked")
			}
		})
	}
}
func TestQualityZeroAndUnknownRemainDifferent(t *testing.T) {
	s := qualityFixture()
	changeRecord(t, &s, func(r *evidence.Record) { r.Value = evidence.Number(0); s.Signals[r.Signal] = 0. })
	if got := assess(s, quality.Reporting, s.CollectedAt); !got.Eligible {
		t.Fatalf("measured zero rejected: %+v", got)
	}
	changeContext(t, &s, func(c *quality.Context) { c.ObservedContainers = nil })
	got := assess(s, quality.Verification, s.CollectedAt)
	for _, d := range got.Dimensions {
		if d.Name == "containers" {
			if d.Observed != nil || d.Status != "unknown" || got.Eligible {
				t.Fatal("unknown became measured zero")
			}
			return
		}
	}
	t.Fatal("missing containers assessment")
}

func TestQualityThresholdBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name     string
		points   int
		advance  time.Duration
		eligible bool
	}{{"exact coverage", 90, 0, true}, {"below coverage", 89, 0, false}, {"exact freshness", 90, 2 * time.Minute, true}, {"expired", 90, 2*time.Minute + time.Nanosecond, false}} {
		t.Run(tc.name, func(t *testing.T) {
			s := qualityFixture()
			s.Window = "99m"
			s.Coverage = nil
			changeRecord(t, &s, func(r *evidence.Record) {
				r.WindowStart = r.WindowEnd.Add(-99 * time.Minute)
				r.SampleCount = tc.points
				r.Extensions["step"] = "1m"
				r.Extensions["max_gap"] = "2m"
				r.Extensions["expected_points"] = "100"
				r.Extensions["actual_points"] = fmt.Sprint(tc.points)
				r.Extensions["first_sample"] = r.WindowStart.Format(time.RFC3339Nano)
			})
			changeContext(t, &s, func(c *quality.Context) {
				c.ScrapeInterval = "1m"
				c.ObservedSamples = &tc.points
				first := s.CollectedAt.Add(-99 * time.Minute)
				c.FirstObservationAt = &first
				c.SourceMaxGap = "2m"
			})
			if got := assess(s, quality.Reporting, s.CollectedAt.Add(tc.advance)); got.Eligible != tc.eligible {
				t.Fatalf("unexpected boundary result: %+v", got)
			}
		})
	}
	for _, peak := range []float64{200, 201} {
		s := qualityFixture()
		changeRecord(t, &s, func(r *evidence.Record) {
			if r.Signal == "memory_working_set_bytes_max" {
				r.Value = evidence.Number(peak)
				s.Signals[r.Signal] = peak
			}
		})
		if got := assess(s, quality.Recommendation, s.CollectedAt); got.Eligible != (peak == 200) {
			t.Fatalf("peak boundary: %+v", got)
		}
	}
}

func TestPeakCannotInheritPrimarySignalQuality(t *testing.T) {
	s := qualityFixture()
	for i, r := range s.Records {
		if r.Signal != "memory_working_set_bytes_max" {
			continue
		}
		context, err := quality.DecodeContext(r.Extensions[quality.ContextKey])
		if err != nil {
			t.Fatal(err)
		}
		zero := 0
		context.ObservedContainers = &zero
		r.Extensions[quality.ContextKey], err = quality.EncodeContext(*context)
		if err != nil {
			t.Fatal(err)
		}
		s.Records[i], err = evidence.Seal(r)
		if err != nil {
			t.Fatal(err)
		}
	}
	report := assess(s, quality.Recommendation, s.CollectedAt)
	if report.Eligible {
		t.Fatal("partial peak evidence inherited complete p95 coverage")
	}
	for _, d := range report.Dimensions {
		if d.Signal == "memory_working_set_bytes_max" && d.Code == "partial_containers" {
			return
		}
	}
	t.Fatal("missing independent peak quality diagnostic")
}
