// Historical PromQL semantics fixtures. Production collection uses mapping_queries.go.
package prometheus

import (
	"fmt"
	"strings"
)

const podScope = `namespace="{namespace}",pod=~"{pod_regex}"`
const containerScope = podScope + `,container!="",container!="POD"`

// coveredAggregate emits a value only when every inventory member has evidence.
// Filtering by identity before counting prevents unrelated series masking a gap.
func coveredAggregate(evidence, inventory, aggregate, labels string) string {
	matched := fmt.Sprintf(`((%s) and on(%s) (%s))`, evidence, labels, inventory)
	return fmt.Sprintf(`%s(%s) and on() (count(%s) == count(%s))`, aggregate, matched, matched, inventory)
}

func throttlingQuery() string {
	labels := "namespace,pod,container"
	n := fmt.Sprintf(`max by (%s) (rate(container_cpu_cfs_throttled_periods_total{%s}[5m]))`, labels, containerScope)
	d := fmt.Sprintf(`max by (%s) (rate(container_cpu_cfs_periods_total{%s}[5m]))`, labels, containerScope)
	// A measured zero numerator AND denominator is idle, not missing telemetry.
	// A missing counter does not match either branch and cannot become a zero.
	known := fmt.Sprintf(`(((%s) / (%s)) and on(%s) ((%s) > 0)) or on(%s) (((%s) == 0) and on(%s) ((%s) == 0))`, n, d, labels, d, labels, n, labels, d)
	finite := fmt.Sprintf(`((%s) >= 0) and on(%s) ((%s) <= 1)`, known, labels, known)
	inventory := fmt.Sprintf(`max by (%s) (kube_pod_container_info{%s})`, labels, containerScope)
	return coveredAggregate(finite, inventory, "max", labels)
}

func evictionQuery() string {
	labels := "namespace,pod"
	evicted := fmt.Sprintf(`max by (%s) (kube_pod_status_reason{%s,reason="Evicted"} == 1)`, labels, podScope)
	// KSM 2.20 emits reasons sparsely. A normal API phase is positive evidence
	// that a pod is not currently eviction-failed; absence alone is not evidence.
	normal := fmt.Sprintf(`0 * max by (%s) (kube_pod_status_phase{%s,phase=~"Running|Pending|Succeeded"} == 1)`, labels, podScope)
	other := fmt.Sprintf(`0 * max by (%s) (kube_pod_status_reason{%s,reason!="Evicted"} == 1)`, labels, podScope)
	known := fmt.Sprintf(`(%s) or on(%s) (%s) or on(%s) (%s)`, evicted, labels, normal, labels, other)
	inventory := fmt.Sprintf(`max by (%s) (kube_pod_info{%s})`, labels, podScope)
	return coveredAggregate(known, inventory, "sum", labels)
}

func defaultQueries() map[string]string {
	c, _ := resolveMapping(MappingConfig{})
	return scopedMappingQueries(c, "{scope}")
}

func renderQuery(template, namespace, podRegex string) string {
	replacer := strings.NewReplacer(
		"{namespace}", namespace,
		"{pod_regex}", podRegex,
	)
	return replacer.Replace(template)
}
