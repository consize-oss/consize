package prometheus

import (
	"fmt"
	"math"
	"sort"
	"time"
)

func scopedMappingQueries(c MappingConfig, scope string) map[string]string {
	container := scope + fmt.Sprintf(`,%s!="",%s!="POD"`, c.Container, c.Container)
	numerator := fmt.Sprintf(`rate(container_cpu_cfs_throttled_periods_total{%s}[5m])`, container)
	denominator := fmt.Sprintf(`rate(container_cpu_cfs_periods_total{%s}[5m])`, container)
	return map[string]string{
		"cpu_cores":                fmt.Sprintf(`rate(container_cpu_usage_seconds_total{%s}[5m])`, container),
		"memory_working_set_bytes": fmt.Sprintf(`container_memory_working_set_bytes{%s}`, container),
		"restarts_30m":             fmt.Sprintf(`increase(kube_pod_container_status_restarts_total{%s}[30m]) or increase(kube_pod_init_container_status_restarts_total{%s}[30m])`, container, container),
		"cpu_throttling_ratio":     fmt.Sprintf(`((%s) / (%s)) or (((%s) == 0) and ((%s) == 0))`, numerator, denominator, numerator, denominator),
		"oom_events_30m":           fmt.Sprintf(`increase(container_oom_events_total{%s}[30m])`, container),
		"evicted_pods":             fmt.Sprintf(`(kube_pod_status_reason{%s,reason="Evicted"} == 1) or (0 * (kube_pod_status_phase{%s,phase=~"Running|Pending|Succeeded"} == 1)) or (0 * (kube_pod_status_reason{%s,reason!="Evicted"} == 1))`, scope, scope, scope),
	}
}

// Reproduce the existing workload reductions after identity validation. Missing
// container members suppress that timestamp instead of producing a partial zero.
func aggregateMapped(signal string, series []Series, c MappingConfig, expected int) []Series {
	switch signal {
	case "cpu_cores", "memory_working_set_bytes", "restarts_30m", "cpu_throttling_ratio", "oom_events_30m", "evicted_pods":
	default:
		return series
	}
	type bucket struct {
		pods    map[string]float64
		counts  map[string]int
		maximum float64
		at      time.Time
	}
	points := map[int64]*bucket{}
	for _, s := range series {
		for _, p := range s.Points {
			if math.IsNaN(p.Value) || math.IsInf(p.Value, 0) {
				continue
			}
			if signal == "cpu_throttling_ratio" && (p.Value < 0 || p.Value > 1) {
				continue
			}
			key := p.Timestamp.UnixNano()
			b := points[key]
			if b == nil {
				b = &bucket{pods: map[string]float64{}, counts: map[string]int{}, at: p.Timestamp}
				points[key] = b
			}
			pod := s.Metric[c.Pod]
			b.pods[pod] += p.Value
			b.counts[pod]++
			if len(b.counts) == 1 && b.counts[pod] == 1 || p.Value > b.maximum {
				b.maximum = p.Value
			}
		}
	}
	out := Series{Metric: map[string]string{}, Points: []Point{}}
	for _, b := range points {
		complete := true
		if signal != "evicted_pods" {
			for _, n := range b.counts {
				if n != expected {
					complete = false
				}
			}
		}
		if !complete {
			continue
		}
		var value float64
		switch signal {
		case "cpu_cores", "memory_working_set_bytes":
			for _, v := range b.pods {
				if v > value {
					value = v
				}
			}
		case "cpu_throttling_ratio":
			value = b.maximum
		default:
			for _, v := range b.pods {
				value += v
			}
		}
		if !math.IsNaN(value) && !math.IsInf(value, 0) {
			out.Points = append(out.Points, Point{Timestamp: b.at, Value: value})
		}
	}
	sort.Slice(out.Points, func(i, j int) bool { return out.Points[i].Timestamp.Before(out.Points[j].Timestamp) })
	if len(out.Points) == 0 {
		return nil
	}
	return []Series{out}
}

func mappingInventoryQuery(scope string) string {
	return "kube_pod_container_info{" + scope + "} or kube_pod_init_container_info{" + scope + "}"
}
