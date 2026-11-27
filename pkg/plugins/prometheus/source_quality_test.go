package prometheus

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/consize-oss/consize/pkg/evidence/quality"
)

type sourceObservationClient struct {
	series     []Series
	sourceStep time.Duration
}

func (c *sourceObservationClient) QueryRange(_ context.Context, query string, _, _ time.Time, step time.Duration) ([]Series, error) {
	if strings.Contains(query, "kube_pod_container_info") {
		out := []Series{}
		for _, s := range c.series {
			copy := Series{Metric: s.Metric}
			for _, p := range s.Points {
				copy.Points = append(copy.Points, Point{Timestamp: p.Timestamp, Value: 1})
			}
			out = append(out, copy)
		}
		return out, nil
	}
	c.sourceStep = step
	return c.series, nil
}
func sourceObservationSeries(values []float64) Series {
	s := mapSeries("owned-pod", "app", 0)
	s.Points = nil
	for i, value := range values {
		s.Points = append(s.Points, Point{Timestamp: mapAt.Add(time.Duration(i) * time.Second), Value: value})
	}
	return s
}
func TestSourceQualityCountsActualScrapesNotEvaluations(t *testing.T) {
	base := float64(mapAt.Unix())
	raw := sourceObservationSeries([]float64{base, base, base + 2, base + 2, base + 4})
	client := &sourceObservationClient{series: []Series{raw}}
	p := NewWithClient(client, Config{Step: time.Second, SourceScrapeInterval: 2 * time.Second, SourceTimestampQueries: map[string]string{"cpu_cores": "timestamp(fixture_source)"}})
	inventory := raw
	inventory.Points = append([]Point(nil), raw.Points...)
	for i := range inventory.Points {
		inventory.Points[i].Value = 1
	}
	facts := mappedCollection{inventory: MapSeries(mapResource(), "inventory", []Series{inventory}, OwnershipSeries{}, MappingConfig{})}
	encoded, identity, err := p.sourceQuality(context.Background(), mapResource(), "cpu_cores", "", mapAt, mapAt.Add(4*time.Second), facts, MappingResult{Status: "mapped"})
	if err != nil {
		t.Fatal(err)
	}
	observed, err := quality.DecodeContext(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if observed.ObservedSamples == nil || *observed.ObservedSamples != 3 || observed.SourceMaxGap != "2s" || !observed.FirstObservationAt.Equal(mapAt) || !observed.LastObservationAt.Equal(mapAt.Add(4*time.Second)) || !strings.HasPrefix(identity, "source-timestamps-v1:") {
		t.Fatal("evaluation timestamps were mistaken for independent scrapes", observed)
	}
}
func TestSourceQualityDoesNotTurnRepeatedOrInvalidSamplesIntoCoverage(t *testing.T) {
	base := float64(mapAt.Unix())
	for name, values := range map[string][]float64{"repeated": {base, base, base, base, base}, "future_at_evaluation": {base + 4, base + 3, base + 4, base + 4, math.NaN()}, "invalid": {math.NaN(), math.Inf(1), base - 1, base + 1000, base - 2}} {
		t.Run(name, func(t *testing.T) {
			raw := sourceObservationSeries(values)
			client := &sourceObservationClient{series: []Series{raw}}
			p := NewWithClient(client, Config{Step: 4 * time.Second, SourceScrapeInterval: 2 * time.Second, SourceTimestampQueries: map[string]string{"cpu_cores": "timestamp(fixture_source)"}})
			encoded, _, err := p.sourceQuality(context.Background(), mapResource(), "cpu_cores", "", mapAt, mapAt.Add(4*time.Second), mappedCollection{}, MappingResult{Status: "mapped"})
			if err != nil {
				t.Fatal(err)
			}
			observed, err := quality.DecodeContext(encoded)
			if err != nil {
				t.Fatal(err)
			}
			if observed.ObservedSamples == nil || *observed.ObservedSamples > 1 || observed.SourceMaxGap != "" {
				t.Fatal("invalid independent-source coverage", observed)
			}
			if client.sourceStep != time.Second {
				t.Fatal("coarse evaluation step hid source scrapes", client.sourceStep)
			}
		})
	}
}
func TestSourceQualityConfigurationFailsClosed(t *testing.T) {
	for _, cfg := range []Config{{SourceTimestampQueries: map[string]string{"cpu_cores": "timestamp(up)"}}, {SourceScrapeInterval: -time.Second}, {SourceScrapeInterval: time.Second, SourceTimestampQueries: map[string]string{"not_declared": "timestamp(up)"}}} {
		if validateCollectionSettings(cfg) == nil {
			t.Fatal("invalid source quality configuration accepted")
		}
	}
}

func TestSourceQualityUsesSignalCadenceAndCopiesConfiguration(t *testing.T) {
	base := float64(mapAt.Unix())
	raw := sourceObservationSeries([]float64{base, base, base + 2, base + 2, base + 4})
	client := &sourceObservationClient{series: []Series{raw}}
	intervals := map[string]time.Duration{"cpu_cores": 2 * time.Second}
	queries := map[string]string{"cpu_cores": "timestamp(fixture_source)"}
	cfg := Config{Step: 4 * time.Second, SourceScrapeInterval: 10 * time.Second, SourceScrapeIntervals: intervals, SourceTimestampQueries: queries}
	if err := validateCollectionSettings(cfg); err != nil {
		t.Fatal(err)
	}
	p := NewWithClient(client, cfg)
	intervals["cpu_cores"] = time.Hour
	queries["cpu_cores"] = "changed"
	encoded, _, err := p.sourceQuality(context.Background(), mapResource(), "cpu_cores", "", mapAt, mapAt.Add(4*time.Second), mappedCollection{}, MappingResult{Status: "mapped"})
	if err != nil {
		t.Fatal(err)
	}
	observed, err := quality.DecodeContext(encoded)
	if err != nil || observed.ScrapeInterval != "2s" || client.sourceStep != time.Second {
		t.Fatal("per-signal source cadence lost", observed, err)
	}
	for _, bad := range []Config{
		{SourceScrapeIntervals: map[string]time.Duration{"cpu_cores": time.Second}},
		{SourceTimestampQueries: map[string]string{"cpu_cores": "timestamp(up)"}, SourceScrapeIntervals: map[string]time.Duration{"cpu_cores": -time.Second}},
		{SourceTimestampQueries: map[string]string{"cpu_cores": "timestamp(up)"}, SourceScrapeIntervals: map[string]time.Duration{"cpu_cores": time.Nanosecond}},
	} {
		if validateCollectionSettings(bad) == nil {
			t.Fatal("invalid per-signal cadence accepted")
		}
	}
}

func TestSourceCadenceParticipatesInCollectionIdentity(t *testing.T) {
	cfg := Config{SourceScrapeInterval: time.Second, SourceScrapeIntervals: map[string]time.Duration{"cpu_cores": 2 * time.Second}}
	before, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	cfg.SourceScrapeInterval = 3 * time.Second
	after, _ := json.Marshal(cfg)
	if string(before) == string(after) {
		t.Fatal("global cadence omitted from provider configuration identity")
	}
	before = after
	cfg.SourceScrapeIntervals["cpu_cores"] = 4 * time.Second
	after, _ = json.Marshal(cfg)
	if string(before) == string(after) {
		t.Fatal("signal cadence omitted from provider configuration identity")
	}
}
