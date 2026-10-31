package prometheus

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/consize-oss/consize/pkg/evidence"
	"github.com/consize-oss/consize/pkg/plugin"
	"github.com/consize-oss/consize/pkg/resource"
)

const ID = "prometheus-metrics"

type Config struct {
	BaseURL string
	Window  time.Duration
	Step    time.Duration
	Units   map[string]string
	Queries map[string]string
}

type Plugin struct {
	client  Client
	window  time.Duration
	step    time.Duration
	units   map[string]string
	queries map[string]string
}

func New(cfg Config) (*Plugin, error) {
	client, err := NewHTTPClient(cfg.BaseURL, nil)
	if err != nil {
		return nil, err
	}
	return NewWithClient(client, cfg), nil
}

func NewWithClient(client Client, cfg Config) *Plugin {
	window := cfg.Window
	if window == 0 {
		window = 24 * time.Hour
	}
	step := cfg.Step
	if step == 0 {
		step = 5 * time.Minute
	}
	queries := map[string]string{}
	for key, query := range defaultQueries() {
		queries[key] = query
	}
	for key, query := range cfg.Queries {
		queries[key] = query
	}
	return &Plugin{client: client, window: window, step: step, queries: queries, units: cfg.Units}
}

func (p *Plugin) ID() string { return ID }

func (p *Plugin) Manifest() plugin.Manifest {
	return plugin.Manifest{
		ID:                     ID,
		DisplayName:            "Prometheus Metrics",
		Version:                "0.3.0-alpha",
		Category:               plugin.CategoryMetrics,
		SupportedResourceTypes: []string{resource.TypeKubernetesDeployment},
		Capabilities:           []string{plugin.CapabilityMetricsRead},
	}
}

func (p *Plugin) Health(ctx context.Context) plugin.Health {
	if p.client == nil {
		return plugin.Health{Status: "unhealthy", Message: "prometheus client is not configured", CheckedAt: time.Now().UTC()}
	}
	end := time.Now().UTC()
	_, err := p.client.QueryRange(ctx, "up", end.Add(-p.step), end, p.step)
	if err != nil {
		return plugin.Health{Status: "unhealthy", Message: err.Error(), CheckedAt: end}
	}
	return plugin.Health{Status: "healthy", Message: "prometheus query API reachable", CheckedAt: end}
}

func (p *Plugin) ReadMetrics(ctx context.Context, res resource.Resource) (plugin.MetricsSnapshot, error) {
	end := time.Now().UTC()
	return p.ReadMetricsBetween(ctx, res, end.Add(-p.window), end)
}

func (p *Plugin) ReadMetricsBetween(ctx context.Context, res resource.Resource, start, end time.Time) (plugin.MetricsSnapshot, error) {
	if !end.After(start) {
		return plugin.MetricsSnapshot{}, fmt.Errorf("invalid metrics interval")
	}
	if p.client == nil {
		return plugin.MetricsSnapshot{}, fmt.Errorf("prometheus client is not configured")
	}
	if res.Type != resource.TypeKubernetesDeployment {
		return plugin.MetricsSnapshot{}, fmt.Errorf("unsupported resource type %q", res.Type)
	}
	namespace, name, err := deploymentIdentity(res)
	if err != nil {
		return plugin.MetricsSnapshot{}, err
	}
	podSelector, _ := res.Metadata["pod_regex"].(string)
	if podSelector == "" {
		podSelector = fmt.Sprintf("%s-.+", regexpQuote(name))
	}
	collectedAt := time.Now().UTC()
	signals := map[string]any{
		"namespace": namespace,
		"name":      name,
		"window":    end.Sub(start).String(),
		"step":      p.step.String(),
	}
	coverage := map[string]plugin.MetricCoverage{}
	records := []evidence.Record{}
	keys := make([]string, 0, len(p.queries))
	for key := range p.queries {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		queryTemplate := p.queries[key]
		query := renderQuery(queryTemplate, namespace, podSelector)
		series, err := p.client.QueryRange(ctx, query, start, end, p.step)
		if err != nil {
			return plugin.MetricsSnapshot{}, err
		}
		values := []float64{}
		discarded := 0
		points := map[int64]bool{}
		var first, last time.Time
		for _, s := range series {
			for _, point := range s.Points {
				if point.Timestamp.Before(start) || point.Timestamp.After(end) || math.IsNaN(point.Value) || math.IsInf(point.Value, 0) {
					discarded++
					continue
				}
				points[point.Timestamp.UnixNano()] = true
				values = append(values, point.Value)
				if first.IsZero() || point.Timestamp.Before(first) {
					first = point.Timestamp
				}
				if point.Timestamp.After(last) {
					last = point.Timestamp
				}
			}
		}
		timestamps := make([]int64, 0, len(points))
		for timestamp := range points {
			timestamps = append(timestamps, timestamp)
		}
		sort.Slice(timestamps, func(i, j int) bool { return timestamps[i] < timestamps[j] })
		var maxGap time.Duration
		for i := 1; i < len(timestamps); i++ {
			gap := time.Duration(timestamps[i] - timestamps[i-1])
			if gap > maxGap {
				maxGap = gap
			}
		}
		coverage[key] = plugin.MetricCoverage{FirstSample: first, LastSample: last, ExpectedPoints: int(end.Sub(start)/p.step) + 1, ActualPoints: len(points), Step: p.step, MaxGap: maxGap}
		if len(values) > 0 {
			signals[key+"_p50"] = percentile(values, 50)
			signals[key+"_p95"] = percentile(values, 95)
			signals[key+"_p99"] = percentile(values, 99)
			signals[key+"_max"] = max(values)
		}
		signals[key+"_points"] = len(values)
		unit := signalUnit(key)
		if configured := p.units[key]; configured != "" {
			unit = configured
		}
		if unit == "" {
			return plugin.MetricsSnapshot{}, fmt.Errorf("unit is undefined for signal %q", key)
		}
		quality := evidence.Valid
		if len(values) == 0 {
			quality = evidence.Missing
			if discarded > 0 {
				quality = evidence.Invalid
			}
		} else if collectedAt.Sub(last) > 2*p.step {
			quality = evidence.Stale
		} else if float64(len(points))/float64(coverage[key].ExpectedPoints) < 0.9 || discarded > 0 {
			quality = evidence.Partial
		}
		for _, stat := range []string{"p50", "p95", "p99", "max"} {
			var value *float64
			if v, ok := signals[key+"_"+stat].(float64); ok {
				value = evidence.Number(v)
			}
			rec, err := evidence.Seal(evidence.Record{ResourceID: res.ID, PluginID: ID, Signal: key + "_" + stat, Query: query, Kind: evidence.Aggregated, Aggregation: stat, Unit: unit, OriginalUnit: unit, Scale: 1, Value: value, SampleCount: len(values), WindowStart: start, WindowEnd: end, CollectedAt: collectedAt, FreshUntil: collectedAt.Add(2 * p.step), Quality: quality, Extensions: map[string]string{"step": p.step.String(), "expected_points": strconv.Itoa(coverage[key].ExpectedPoints), "actual_points": strconv.Itoa(len(points)), "max_gap": maxGap.String(), "first_sample": first.UTC().Format(time.RFC3339Nano), "last_sample": last.UTC().Format(time.RFC3339Nano)}})
			if err != nil {
				return plugin.MetricsSnapshot{}, err
			}
			records = append(records, rec)
		}

	}
	return plugin.MetricsSnapshot{
		Records:    records,
		Coverage:   coverage,
		PluginID:   ID,
		ResourceID: res.ID,
		Source:     "prometheus",
		Window:     end.Sub(start).String(),
		Signals:    signals,
		Evidence: []string{
			"metrics read from Prometheus query_range",
			"deployment is matched through namespace and ReplicaSet-style pod prefix",
			"health verification can use the same metrics after an action",
		},
		CollectedAt: collectedAt,
	}, nil
}

func deploymentIdentity(res resource.Resource) (string, string, error) {
	ns, _ := res.Metadata["namespace"].(string)
	name, _ := res.Metadata["name"].(string)
	if ns == "" {
		ns, _ = res.Labels["namespace"]
	}
	if name == "" {
		name = res.Name
	}
	if ns == "" || name == "" {
		return "", "", fmt.Errorf("kubernetes deployment resource requires metadata.namespace and metadata.name")
	}
	return ns, name, nil
}

func percentile(values []float64, p float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sort.Float64s(values)
	idx := int(math.Ceil((p/100)*float64(len(values)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(values) {
		idx = len(values) - 1
	}
	return values[idx]
}

func max(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	out := values[0]
	for _, v := range values[1:] {
		if v > out {
			out = v
		}
	}
	return out
}

func regexpQuote(s string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `.`, `\.`, `+`, `\+`, `*`, `\*`, `?`, `\?`, `^`, `\^`, `$`, `\$`, `(`, `\(`, `)`, `\)`, `[`, `\[`, `]`, `\]`, `{`, `\{`, `}`, `\}`, `|`, `\|`)
	return replacer.Replace(s)
}

func defaultQueries() map[string]string {
	return map[string]string{
		"cpu_cores":                `max(sum by (pod) (rate(container_cpu_usage_seconds_total{namespace="{namespace}",pod=~"{pod_regex}",container!="",container!="POD",image!=""}[5m])))`,
		"memory_working_set_bytes": `max(sum by (pod) (container_memory_working_set_bytes{namespace="{namespace}",pod=~"{pod_regex}",container!="",container!="POD",image!=""}))`,
		"restarts_30m":             `sum(increase(kube_pod_container_status_restarts_total{namespace="{namespace}",pod=~"{pod_regex}"}[30m]))`,
		"cpu_throttling_ratio":     throttlingQuery(),
		"oom_events_30m":           `sum(increase(container_oom_events_total{namespace="{namespace}",pod=~"{pod_regex}",container!="",container!="POD"}[30m]))`,
		"evicted_pods":             evictionQuery(),
	}
}

func renderQuery(template, namespace, podRegex string) string {
	replacer := strings.NewReplacer(
		"{namespace}", namespace,
		"{pod_regex}", podRegex,
	)
	return replacer.Replace(template)
}

func signalUnit(key string) string {
	switch key {
	case "cpu_cores":
		return "cores"
	case "memory_working_set_bytes":
		return "bytes"
	case "cpu_throttling_ratio":
		return "ratio"
	case "restarts_30m", "oom_events_30m", "evicted_pods":
		return "count"
	}
	return ""
}
