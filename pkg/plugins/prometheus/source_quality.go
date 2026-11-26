package prometheus

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/consize-oss/consize/pkg/evidence/quality"
	"github.com/consize-oss/consize/pkg/resource"
)

// Source timestamp queries must preserve the original scrape timestamp as their
// VALUE (timestamp(raw_selector), before joins/arithmetic). Query evaluation
// timestamps are deliberately not used as evidence of independent observations.
func (p *Plugin) sourceQuality(ctx context.Context, res resource.Resource, key, scope string, start, end time.Time, facts mappedCollection, mapping MappingResult) (string, string, error) {
	configured := p.sourceTimestampQueries[key]
	if configured == "" {
		return "", "", nil
	}
	interval := p.sourceScrapeInterval
	if override := p.sourceScrapeIntervals[key]; override > 0 {
		interval = override
	}
	query := strings.ReplaceAll(configured, "{scope}", scope)
	digest := sha256.Sum256([]byte("source-timestamps-v1\x00" + query + "\x00" + interval.String()))
	identity := "source-timestamps-v1:" + hex.EncodeToString(digest[:])
	result := quality.Context{Mapping: "unknown", ScrapeInterval: interval.String()}
	sourceStep := interval / 2
	sourceStep = sourceStep.Truncate(time.Millisecond)
	if sourceStep < time.Millisecond {
		sourceStep = time.Millisecond
	}
	if p.step < sourceStep {
		sourceStep = p.step
	}
	if sourceStep != p.step {
		reader := *p
		reader.step = sourceStep
		var err error
		facts, err = reader.mappedInventory(ctx, res, scope, start, end)
		if err != nil {
			return "", "", err
		}
	}
	raw, err := p.client.QueryRange(ctx, query, start, end, sourceStep)
	if err != nil {
		return "", "", safeError(err)
	}
	mapped := MapSeries(res, key, raw, facts.owners, p.mapping)
	if mapped.Status == "mapped" {
		mapped = validateMembership(mapped, facts.inventory, key, p.mapping)
	}
	if mapping.Status != "mapped" || mapped.Status != "mapped" || len(mapped.Series) == 0 {
		encoded, e := quality.EncodeContext(result)
		return encoded, identity, e
	}
	result.Mapping = "exact"
	var first, last time.Time
	maximumGap := time.Duration(0)
	minimumSamples := -1
	for _, series := range mapped.Series {
		seen := map[int64]bool{}
		for _, point := range series.Points {
			// timestamp() returns epoch seconds with source millisecond precision.
			if math.IsNaN(point.Value) || math.IsInf(point.Value, 0) || point.Value < 0 || point.Value > 253402300799 {
				continue
			}
			observed := time.UnixMilli(int64(math.Round(point.Value * 1000))).UTC()
			if point.Timestamp.Before(start) || point.Timestamp.After(end) || observed.Before(start) || observed.After(end) || observed.After(point.Timestamp) {
				continue
			}
			seen[observed.UnixMilli()] = true
		}
		times := make([]int64, 0, len(seen))
		for value := range seen {
			times = append(times, value)
		}
		sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
		if minimumSamples < 0 || len(times) < minimumSamples {
			minimumSamples = len(times)
		}
		if len(times) == 0 {
			minimumSamples = 0
			continue
		}
		memberFirst, memberLast := time.UnixMilli(times[0]).UTC(), time.UnixMilli(times[len(times)-1]).UTC()
		// Conservative intersection: every member must cover these boundaries.
		if first.IsZero() || memberFirst.After(first) {
			first = memberFirst
		}
		if last.IsZero() || memberLast.Before(last) {
			last = memberLast
		}
		for i := 1; i < len(times); i++ {
			gap := time.Duration(times[i]-times[i-1]) * time.Millisecond
			if gap > maximumGap {
				maximumGap = gap
			}
		}
	}
	if minimumSamples >= 0 {
		result.ObservedSamples = &minimumSamples
	}
	if minimumSamples > 0 && !first.IsZero() && !last.Before(first) {
		result.FirstObservationAt = &first
		result.LastObservationAt = &last
	}
	if maximumGap > 0 {
		result.SourceMaxGap = maximumGap.String()
	}
	encoded, err := quality.EncodeContext(result)
	return encoded, identity, err
}
