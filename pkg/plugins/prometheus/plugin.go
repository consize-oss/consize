package prometheus

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"

	"github.com/consize-oss/consize/pkg/evidence"
	"github.com/consize-oss/consize/pkg/plugin"
	"github.com/consize-oss/consize/pkg/resource"
)

const ID = "prometheus-metrics"

type Config struct {
	Percentile     int `json:"percentile,omitempty"`
	MinimumSamples int `json:"minimum_samples,omitempty"`
	Mapping        MappingConfig
	Transport      TransportConfig
	BaseURL        string
	Window         time.Duration
	Step           time.Duration
	Units          map[string]string
	Queries        map[string]string
}

type Plugin struct {
	percentile     int
	minimumSamples int
	now            func() time.Time
	mapping        MappingConfig
	mappingError   error
	customQueries  map[string]string
	client         Client
	window         time.Duration
	step           time.Duration
	units          map[string]string
}

func New(cfg Config) (*Plugin, error) {
	if err := validateCollectionSettings(cfg); err != nil {
		return nil, err
	}
	if len(cfg.Queries) > 32 || len(cfg.Units) > 32 {
		return nil, failure("configuration")
	}
	if _, err := resolveMapping(cfg.Mapping); err != nil {
		return nil, err
	}
	client, err := NewConfiguredHTTPClient(cfg.BaseURL, cfg.Transport)
	if err != nil {
		return nil, err
	}
	p := NewWithClient(client, cfg)
	if p.window < p.step || p.window < time.Millisecond || int64(p.window/p.step)+1 > int64(client.limits.samples) {
		return nil, failure("configuration")
	}
	return p, nil
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
	mapping, mappingError := resolveMapping(cfg.Mapping)
	if err := validateCollectionSettings(cfg); err != nil {
		mappingError = err
	}
	requestedPercentile := cfg.Percentile
	if requestedPercentile == 0 {
		requestedPercentile = 95
	}
	if window < step {
		mappingError = failure("configuration")
	}
	minimumSamples := cfg.MinimumSamples
	if minimumSamples == 0 {
		minimumSamples = 2
	}
	if len(cfg.Queries) > 32 || len(cfg.Units) > 32 {
		mappingError = failure("configuration")
	}
	custom := map[string]string{}
	for key, query := range cfg.Queries {
		custom[key] = query
	}
	units := map[string]string{}
	for key, unit := range cfg.Units {
		units[key] = unit
	}
	return &Plugin{percentile: requestedPercentile, minimumSamples: minimumSamples, now: time.Now, client: client, window: window, step: step, units: units, mapping: mapping, mappingError: mappingError, customQueries: custom}
}

func (p *Plugin) ID() string { return ID }

func (p *Plugin) Manifest() plugin.Manifest {
	return plugin.WithBuiltinRuntime(plugin.Manifest{
		ID:                     ID,
		DisplayName:            "Prometheus Metrics",
		Version:                "0.3.0-alpha",
		DataVersion:            QueryCatalogVersion + "/" + AggregationVersion,
		Category:               plugin.CategoryMetrics,
		SupportedResourceTypes: []string{resource.TypeKubernetesDeployment},
		Capabilities:           []string{plugin.CapabilityMetricsRead},
	}, "kubernetes")
}

func (p *Plugin) Health(ctx context.Context) plugin.Health {
	if p.client == nil {
		return plugin.Health{Status: "unhealthy", Message: "prometheus client is not configured", CheckedAt: time.Now().UTC()}
	}
	if httpClient, ok := p.client.(*HTTPClient); ok {
		ctx, cancel := context.WithTimeout(ctx, httpClient.limits.request)
		defer cancel()
		err := httpClient.Ready(ctx)
		if err == nil {
			_, err = httpClient.QueryInstant(ctx, "1", time.Now().UTC())
		}
		diagnostics := httpClient.Diagnostics()
		status, message := "healthy", "Prometheus is ready and its query API is reachable; metric coverage is assessed separately."
		if err != nil {
			status, message = "unhealthy", safeError(err).Error()
		} else if diagnostics.State == "degraded" {
			status, message = "degraded", "Prometheus returned query warnings; inspect provider diagnostics."
		}
		return plugin.Health{Status: status, Message: message, CheckedAt: time.Now().UTC(), Details: map[string]any{"provider": diagnostics}}
	}
	end := time.Now().UTC()
	_, err := p.client.QueryRange(ctx, "up", end.Add(-p.step), end, p.step)
	if err != nil {
		return plugin.Health{Status: "unhealthy", Message: safeError(err).Error(), CheckedAt: end}
	}
	return plugin.Health{Status: "healthy", Message: "prometheus query API reachable", CheckedAt: end}
}

func (p *Plugin) ReadMetrics(ctx context.Context, res resource.Resource) (plugin.MetricsSnapshot, error) {
	end := p.now().UTC().Truncate(time.Millisecond)
	return p.ReadMetricsBetween(ctx, res, end.Add(-p.window), end)
}

func (p *Plugin) ReadMetricsBetween(ctx context.Context, res resource.Resource, start, end time.Time) (plugin.MetricsSnapshot, error) {
	if end.After(p.now().UTC()) || !end.After(start) || end.Sub(start) < time.Millisecond || end.Sub(start) > 365*24*time.Hour {
		return plugin.MetricsSnapshot{}, fmt.Errorf("invalid metrics interval")
	}
	if p.client == nil {
		return plugin.MetricsSnapshot{}, fmt.Errorf("prometheus client is not configured")
	}
	if res.Type != resource.TypeKubernetesDeployment {
		return plugin.MetricsSnapshot{}, fmt.Errorf("unsupported resource type %q", res.Type)
	}
	requestedStart, requestedEnd := start, end
	namespace, name, err := deploymentIdentity(res)
	if err != nil {
		return plugin.MetricsSnapshot{}, err
	}
	if p.mappingError != nil {
		return plugin.MetricsSnapshot{}, p.mappingError
	}
	cfg := p.mapping
	scope := mappingMatcher(cfg.Cluster, "=", res.Account) + "," + mappingMatcher(cfg.Namespace, "=", namespace)
	definitions := p.definitions(scope)
	queryStart := start.Truncate(time.Millisecond)
	if queryStart.Before(start) {
		queryStart = queryStart.Add(time.Millisecond)
	}
	evaluationAt := end.Truncate(time.Millisecond)
	if !evaluationAt.After(queryStart) {
		return plugin.MetricsSnapshot{}, failure("configuration")
	}
	start, end = queryStart, evaluationAt
	historical, err := p.mappedInventory(ctx, res, scope, queryStart, evaluationAt)
	if err != nil {
		return plugin.MetricsSnapshot{}, err
	}
	current, err := p.mappedInventory(ctx, res, scope, evaluationAt, evaluationAt)
	if err != nil {
		return plugin.MetricsSnapshot{}, err
	}
	collectedAt := p.now().UTC()
	signals := map[string]any{
		"namespace": namespace,
		"name":      name,
		"window":    end.Sub(start).String(),
		"step":      p.step.String(),
		"mapping":   map[string]any{},
	}
	coverage := map[string]plugin.MetricCoverage{}
	records := []evidence.Record{}
	keys := make([]string, 0, len(definitions))
	for key := range definitions {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if err := ctx.Err(); err != nil {
			return plugin.MetricsSnapshot{}, safeError(err)
		}
		definition := definitions[key]
		query := definition.Query
		from, to, facts := queryStart, evaluationAt, historical
		recordStart, recordEnd := start, end
		if definition.Current {
			from = evaluationAt
			facts = current

		}
		sourceUnit := definition.Unit
		if configured := p.units[key]; configured != "" {
			sourceUnit = configured
		}
		unit, scale, err := normalizedUnit(key, sourceUnit)
		if err != nil {
			return plugin.MetricsSnapshot{}, err
		}
		series, err := p.client.QueryRange(ctx, query, from, to, p.step)
		if err != nil {
			return plugin.MetricsSnapshot{}, safeError(err)
		}
		mapping := MapSeries(res, key, series, facts.owners, cfg)
		if mapping.Status == "mapped" {
			mapping = validateMembership(mapping, facts.inventory, key, cfg)
		}
		if definition.Aggregation == "single-series-custom" && len(mapping.Series) > 1 {
			mapping.Status = "custom_aggregation_undefined"
			mapping.Series = nil
		}
		signals["mapping"].(map[string]any)[key] = map[string]any{"version": MappingVersion, "status": mapping.Status, "source": mapping.Source, "members": mapping.Members, "excluded": mapping.Excluded, "digest": mapping.Digest}
		series = normalizeSeries(mapping.Series, scale)
		for i := range series {
			for j, point := range series[i].Points {
				if point.Timestamp.Before(from) || point.Timestamp.After(to) || point.Timestamp.Sub(from)%p.step != 0 {
					series[i].Points[j].Value = math.NaN()
				}
			}
		}
		rawDiscarded := 0
		for _, raw := range series {
			for _, point := range raw.Points {
				if point.Timestamp.Before(from) || point.Timestamp.After(to) || point.Timestamp.Sub(from)%p.step != 0 || !validMeasurement(key, point.Value) {
					rawDiscarded++
				}
			}
		}
		aggregateDiscarded := 0
		if mapping.Status == "mapped" && definition.Aggregation != "single-series-custom" {
			target, _ := targetForMapping(res)
			series, aggregateDiscarded = reduceMapped(key, series, cfg, len(target.Containers))
		}
		values := []float64{}
		discarded := rawDiscarded + aggregateDiscarded
		points := map[int64]bool{}
		var first, last time.Time
		for _, s := range series {
			for _, point := range s.Points {
				if point.Timestamp.Before(from) || point.Timestamp.After(to) || math.IsNaN(point.Value) || math.IsInf(point.Value, 0) {
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
		expectedPoints := int(end.Sub(start)/p.step) + 1
		if definition.Current {
			expectedPoints = 1
		}
		coverage[key] = plugin.MetricCoverage{FirstSample: first, LastSample: last, ExpectedPoints: expectedPoints, ActualPoints: len(points), Step: p.step, MaxGap: maxGap}
		requiredSamples := p.minimumSamples
		sampleKind := "range"
		if definition.Current {
			requiredSamples = 1
			sampleKind = "current_configuration"
		}
		statistics := collectionStatistics(p.percentile, definition.Current)
		if len(values) > 0 {
			for _, stat := range statistics {
				signals[key+"_"+stat] = statisticValue(append([]float64(nil), values...), stat)
			}
		}
		signals[key+"_points"] = len(values)
		quality := evidence.Valid
		if len(values) == 0 {
			quality = evidence.Missing
			if discarded > 0 {
				quality = evidence.Invalid
			}
		} else if collectedAt.Sub(last) > 2*p.step {
			quality = evidence.Stale
		} else if float64(len(points))/float64(coverage[key].ExpectedPoints) < 0.9 || discarded > 0 || !definition.Current && len(values) < p.minimumSamples {
			quality = evidence.Partial
		}
		if mapping.Status != "mapped" {
			quality = evidence.Unavailable
		}
		for _, stat := range statistics {
			aggregation := stat
			if stat == "current" {
				aggregation = "last"
			}
			var value *float64
			if v, ok := signals[key+"_"+stat].(float64); ok {
				value = evidence.Number(v)
			}
			rec, err := evidence.Seal(evidence.Record{ResourceID: res.ID, PluginID: ID, Signal: key + "_" + stat, Query: query, Kind: evidence.Aggregated, Aggregation: aggregation, Unit: unit, OriginalUnit: sourceUnit, Scale: scale, Value: value, SampleCount: len(values), WindowStart: recordStart, WindowEnd: recordEnd, CollectedAt: collectedAt, FreshUntil: collectedAt.Add(2 * p.step), Quality: quality, Extensions: map[string]string{"requested_start": requestedStart.UTC().Format(time.RFC3339Nano), "requested_end": requestedEnd.UTC().Format(time.RFC3339Nano), "discarded_source_points": strconv.Itoa(rawDiscarded), "discarded_evaluation_points": strconv.Itoa(aggregateDiscarded), "query_version": definition.Version, "query_digest": queryIdentity(definition, p.step, sourceUnit), "catalog_version": QueryCatalogVersion, "aggregation_version": AggregationVersion, "aggregation_rule": definition.Aggregation, "percentile_method": PercentileMethod, "evaluation_at": evaluationAt.Format(time.RFC3339Nano), "minimum_samples": strconv.Itoa(requiredSamples), "sample_kind": sampleKind, "configured_percentile": strconv.Itoa(p.percentile), "mapping_cluster": res.Account, "mapping_deployment_uid": mappingUID(res), "mapping_namespace": namespace, "mapping_version": MappingVersion, "mapping_status": mapping.Status, "mapping_source": mapping.Source, "mapping_digest": mapping.Digest, "mapping_members": strconv.Itoa(mapping.Members), "mapping_excluded": strconv.Itoa(mapping.Excluded), "step": p.step.String(), "expected_points": strconv.Itoa(coverage[key].ExpectedPoints), "actual_points": strconv.Itoa(len(points)), "max_gap": maxGap.String(), "first_sample": first.UTC().Format(time.RFC3339Nano), "last_sample": last.UTC().Format(time.RFC3339Nano)}})
			if err != nil {
				return plugin.MetricsSnapshot{}, err
			}
			records = append(records, rec)
		}

	}
	completedAt := p.now().UTC()
	for i, record := range records {
		record.CollectedAt = completedAt
		record.FreshUntil = completedAt.Add(2 * p.step)
		last, _ := time.Parse(time.RFC3339Nano, record.Extensions["last_sample"])
		if (record.Quality == evidence.Valid || record.Quality == evidence.Partial) && completedAt.Sub(last) > 2*p.step {
			record.Quality = evidence.Stale
		}
		sealed, e := evidence.Seal(record)
		if e != nil {
			return plugin.MetricsSnapshot{}, e
		}
		records[i] = sealed
	}
	snapshot := plugin.MetricsSnapshot{
		Records:    records,
		Coverage:   coverage,
		PluginID:   ID,
		ResourceID: res.ID,
		Source:     "prometheus",
		Window:     end.Sub(start).String(),
		Signals:    signals,
		Evidence: []string{
			"metrics read from Prometheus query_range",
			"series ownership is validated before workload aggregation; see mapping provenance on each record",
			"health verification can use the same metrics after an action",
		},
		CollectedAt: completedAt,
	}
	if err := snapshot.ValidateRecords(); err != nil {
		return plugin.MetricsSnapshot{}, err
	}
	return snapshot, nil
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
	idx := int(math.Ceil(p*float64(len(values))/100)) - 1
	if math.Trunc(p) == p {
		idx = (int(p)*len(values)+99)/100 - 1
	}
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

func signalUnit(key string) string {
	switch key {
	case "cpu_cores", "cpu_request_cores", "cpu_limit_cores":
		return "cores"
	case "memory_working_set_bytes", "memory_request_bytes", "memory_limit_bytes":
		return "bytes"
	case "cpu_throttling_ratio":
		return "ratio"
	case "restarts_30m", "oom_events_30m", "evicted_pods":
		return "count"
	}
	return ""
}

func mappingUID(r resource.Resource) string { uid, _ := r.Metadata["uid"].(string); return uid }
