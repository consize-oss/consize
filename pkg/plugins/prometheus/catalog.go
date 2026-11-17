package prometheus

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/consize-oss/consize/pkg/evidence"
)

const QueryCatalogVersion = "kubernetes-promql-v1"
const AggregationVersion = "deployment-aggregation-v2"
const PercentileMethod = "nearest-rank-v1"

type QueryDefinition struct {
	Signal      string `json:"signal"`
	Version     string `json:"version"`
	Query       string `json:"query"`
	Unit        string `json:"unit"`
	Aggregation string `json:"aggregation"`
	Current     bool   `json:"current"`
}

// QueryCatalog returns independent, inspectable definitions, without network or
// registry access. Scope is supplied from the validated CON-18 matcher builder.
func QueryCatalog(c MappingConfig, cluster, namespace string) ([]QueryDefinition, error) {
	resolved, err := resolveMapping(c)
	if err != nil {
		return nil, err
	}
	for _, value := range []string{cluster, namespace} {
		if strings.TrimSpace(value) == "" || len(value) > 512 || strings.IndexFunc(value, unicode.IsControl) >= 0 {
			return nil, failure("configuration")
		}
	}
	scope := mappingMatcher(resolved.Cluster, "=", cluster) + "," + mappingMatcher(resolved.Namespace, "=", namespace)
	return queryCatalog(resolved, scope), nil
}
func queryCatalog(c MappingConfig, scope string) []QueryDefinition {
	queries := scopedMappingQueries(c, scope)
	for _, entry := range []struct{ signal, resource, kind, unit string }{
		{"cpu_request_cores", "cpu", "requests", "core"}, {"cpu_limit_cores", "cpu", "limits", "core"},
		{"memory_request_bytes", "memory", "requests", "byte"}, {"memory_limit_bytes", "memory", "limits", "byte"},
	} {
		matchers := []string{mappingMatcher("resource", "=", entry.resource), mappingMatcher("unit", "=", entry.unit)}
		regular := mappingSelector("kube_pod_container_resource_"+entry.kind, scope, matchers...)
		init := mappingSelector("kube_pod_init_container_resource_"+entry.kind, scope, matchers...)
		queries[entry.signal] = regular + " or " + init
	}
	out := []QueryDefinition{}
	for signal, query := range queries {
		aggregation := "sum-containers-max-pod"
		switch signal {
		case "restarts_30m", "oom_events_30m", "evicted_pods":
			aggregation = "sum-members"
		case "cpu_throttling_ratio":
			aggregation = "max-container"
		}
		out = append(out, QueryDefinition{Signal: signal, Version: QueryCatalogVersion + "/" + signal, Query: query, Unit: signalUnit(signal), Aggregation: aggregation, Current: isConfigurationSignal(signal)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Signal < out[j].Signal })
	return out
}
func isConfigurationSignal(signal string) bool {
	switch signal {
	case "cpu_request_cores", "cpu_limit_cores", "memory_request_bytes", "memory_limit_bytes":
		return true
	}
	return false
}
func queryIdentity(def QueryDefinition, step time.Duration, sourceUnit string) string {
	raw := def.Signal + "\x00" + strconv.FormatBool(def.Current) + "\x00" + def.Version + "\x00" + def.Query + "\x00" + def.Aggregation + "\x00" + AggregationVersion + "\x00" + step.String() + "\x00" + sourceUnit
	digest := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(digest[:])
}
func normalizedUnit(signal, source string) (string, float64, error) {
	if strings.TrimSpace(source) != source || strings.IndexFunc(source, unicode.IsControl) >= 0 {
		return "", 0, failure("unit_configuration")
	}
	canonical, scale := source, 1.
	_, unit, factor, err := evidence.Normalize(0, source)
	if err == nil {
		canonical, scale = unit, factor
	} else if signalUnit(signal) != "" || source == "" || len(source) > 64 {
		return "", 0, failure("unit_configuration")
	}
	if expected := signalUnit(signal); expected != "" && canonical != expected {
		return "", 0, failure("unit_configuration")
	}
	return canonical, scale, nil
}
func normalizeSeries(series []Series, scale float64) []Series {
	out := make([]Series, len(series))
	for i, s := range series {
		out[i] = Series{Metric: s.Metric, Points: make([]Point, len(s.Points))}
		for j, p := range s.Points {
			original := p.Value
			p.Value *= scale
			if original != 0 && p.Value == 0 {
				p.Value = math.NaN()
			}
			if original == 0 {
				p.Value = 0
			}
			out[i].Points[j] = p
		}
	}
	return out
}
func validMeasurement(signal string, value float64) bool {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return false
	}
	if signalUnit(signal) != "" && value < 0 {
		return false
	}
	return signal != "cpu_throttling_ratio" || value <= 1
}
func collectionStatistics(percentile int, current bool) []string {
	if current {
		return []string{"current"}
	}
	out := []string{"p50", "p95", "p99", "max"}
	configured := "p" + strconv.Itoa(percentile)
	for _, key := range out {
		if key == configured {
			return out
		}
	}
	return append(out, configured)
}
func statisticValue(values []float64, stat string) float64 {
	if stat == "current" {
		return values[len(values)-1]
	}
	if stat == "max" {
		return max(values)
	}
	number, _ := strconv.Atoi(stat[1:])
	return percentile(values, float64(number))
}
func validateCollectionSettings(c Config) error {
	if c.Window%time.Millisecond != 0 || c.Step%time.Millisecond != 0 {
		return failure("configuration")
	}
	if c.Window < 0 || c.Window > 365*24*time.Hour || c.Step < 0 || c.Step > 24*time.Hour || c.Step > 0 && c.Step < time.Millisecond {
		return failure("configuration")
	}
	if c.Percentile != 0 && (c.Percentile < 1 || c.Percentile > 99) || c.MinimumSamples != 0 && (c.MinimumSamples < 2 || c.MinimumSamples > 100000) {
		return failure("configuration")
	}
	for signal, query := range c.Queries {
		if len(signal) > 128 || !labelName.MatchString(signal) || !validQuery(query) || c.Units[signal] == "" {
			return failure("unit_configuration")
		}
	}
	for signal, unit := range c.Units {
		if signalUnit(signal) == "" && c.Queries[signal] == "" {
			return failure("unit_configuration")
		}
		if expected := signalUnit(signal); expected != "" && unit != expected && c.Queries[signal] == "" {
			return failure("unit_configuration")
		}
		if _, _, err := normalizedUnit(signal, unit); err != nil {
			return err
		}
	}
	return nil
}
func safeCustomVersion(query string) string {
	digest := sha256.Sum256([]byte(query))
	return fmt.Sprintf("custom-sha256:%x", digest[:])
}
