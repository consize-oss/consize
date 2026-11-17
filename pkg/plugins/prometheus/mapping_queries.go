package prometheus

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

func scopedMappingQueries(c MappingConfig, scope string) map[string]string {
	container := scope + "," + mappingMatcher(c.Container, "!=", "") + "," + mappingMatcher(c.Container, "!=", "POD")
	evicted := mappingSelector("kube_pod_status_reason", scope, mappingMatcher("reason", "=", "Evicted"))
	normal := mappingSelector("kube_pod_status_phase", scope, mappingMatcher("phase", "=~", "Running|Pending|Succeeded"))
	other := mappingSelector("kube_pod_status_reason", scope, mappingMatcher("reason", "!=", "Evicted"))
	numerator := fmt.Sprintf(`rate(container_cpu_cfs_throttled_periods_total{%s}[5m])`, container)
	denominator := fmt.Sprintf(`rate(container_cpu_cfs_periods_total{%s}[5m])`, container)
	return map[string]string{
		"cpu_cores":                fmt.Sprintf(`rate(container_cpu_usage_seconds_total{%s}[5m])`, container),
		"memory_working_set_bytes": fmt.Sprintf(`container_memory_working_set_bytes{%s}`, container),
		"restarts_30m":             fmt.Sprintf(`increase(kube_pod_container_status_restarts_total{%s}[30m]) or increase(kube_pod_init_container_status_restarts_total{%s}[30m])`, container, container),
		"cpu_throttling_ratio":     fmt.Sprintf(`(((%s) / (%s)) and ((%s) > 0)) or (0 * (((%s) == 0) and ((%s) == 0)))`, numerator, denominator, denominator, numerator, denominator),
		"oom_events_30m":           fmt.Sprintf(`increase(container_oom_events_total{%s}[30m])`, container),
		"evicted_pods":             fmt.Sprintf(`(%s == 1) or (0 * (%s == 1)) or (0 * (%s == 1))`, evicted, normal, other),
	}
}

// Reproduce the existing workload reductions after identity validation. Missing
// container members suppress that timestamp instead of producing a partial zero.
func aggregateMapped(signal string, series []Series, c MappingConfig, expected int) []Series {
	out, _ := reduceMapped(signal, series, c, expected)
	return out
}
func reduceMapped(signal string, series []Series, c MappingConfig, expected int) ([]Series, int) {
	type bucket struct {
		at      time.Time
		members map[string]map[string]float64
		invalid bool
	}
	points := map[string]*bucket{}
	for _, s := range series {
		for _, p := range s.Points {
			key := p.Timestamp.UTC().Format(time.RFC3339Nano)
			b := points[key]
			if b == nil {
				b = &bucket{at: p.Timestamp, members: map[string]map[string]float64{}}
				points[key] = b
			}
			pod := s.Metric[c.Pod] + "\x00" + s.Metric[c.PodUID]
			if b.members[pod] == nil {
				b.members[pod] = map[string]float64{}
			}
			if !validMeasurement(signal, p.Value) {
				b.invalid = true
				continue
			}
			member := s.Metric[c.Container]
			if signal == "evicted_pods" {
				member = "pod"
			}
			if _, duplicate := b.members[pod][member]; duplicate {
				b.invalid = true
			}
			b.members[pod][member] = p.Value
		}
	}
	discarded := 0
	out := Series{Metric: map[string]string{}, Points: []Point{}}
	for _, b := range points {
		if b.invalid {
			discarded++
			continue
		}
		pods := []string{}
		for pod := range b.members {
			pods = append(pods, pod)
		}
		sort.Strings(pods)
		value := 0.
		podCorrection := 0.
		complete := true
		for _, pod := range pods {
			members := b.members[pod]
			required := expected
			if signal == "evicted_pods" {
				required = 1
			}
			if len(members) != required {
				complete = false
				break
			}
			names := []string{}
			for name := range members {
				names = append(names, name)
			}
			sort.Strings(names)
			total := 0.
			correction := 0.
			for _, name := range names {
				n := members[name]
				increment := n - correction
				next := total + increment
				correction = (next - total) - increment
				total = next
				if signal == "cpu_throttling_ratio" && n > value {
					value = n
				}
			}
			switch signal {
			case "cpu_throttling_ratio":
			case "restarts_30m", "oom_events_30m", "evicted_pods":
				increment := total - podCorrection
				next := value + increment
				podCorrection = (next - value) - increment
				value = next
			default:
				if total > value {
					value = total
				}
			}
		}
		if complete && !math.IsNaN(value) && !math.IsInf(value, 0) {
			out.Points = append(out.Points, Point{Timestamp: b.at, Value: value})
		} else {
			discarded++
		}
	}
	sort.Slice(out.Points, func(i, j int) bool { return out.Points[i].Timestamp.Before(out.Points[j].Timestamp) })
	if len(out.Points) == 0 {
		return nil, discarded
	}
	return []Series{out}, discarded
}

func mappingInventoryQuery(scope string) string {
	return "kube_pod_container_info{" + scope + "} or kube_pod_init_container_info{" + scope + "}"
}

// Label names are validated during configuration; operators are static syntax.
// Values are always escaped as complete PromQL double-quoted string literals.
func mappingMatcher(label, operator, value string) string {
	return label + operator + strconv.Quote(value)
}
func mappingSelector(metric, scope string, extra ...string) string {
	if len(extra) > 0 {
		scope += "," + strings.Join(extra, ",")
	}
	return metric + "{" + scope + "}"
}
