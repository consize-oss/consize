package prometheus

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/consize-oss/consize/pkg/resource"
)

const MappingVersion = "kubernetes-series-mapping-v1"

// MappingConfig declares label aliases; no name-prefix or value-based inference.
// owner_chain requires enriched ReplicaSet owner metrics carrying Deployment UID.
type MappingConfig struct {
	Mode          string `json:"mode,omitempty"`
	Cluster       string `json:"cluster,omitempty"`
	Namespace     string `json:"namespace,omitempty"`
	Pod           string `json:"pod,omitempty"`
	PodUID        string `json:"pod_uid,omitempty"`
	Container     string `json:"container,omitempty"`
	Deployment    string `json:"deployment,omitempty"`
	DeploymentUID string `json:"deployment_uid,omitempty"`
	ReplicaSet    string `json:"replicaset,omitempty"`
	OwnerName     string `json:"owner_name,omitempty"`
	OwnerKind     string `json:"owner_kind,omitempty"`
	Controller    string `json:"controller,omitempty"`
}

var labelName = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

func resolveMapping(c MappingConfig) (MappingConfig, error) {
	if c.Mode == "" {
		c.Mode = "direct"
	}
	if c.Mode != "direct" && c.Mode != "owner_chain" {
		return c, failure("mapping_configuration")
	}
	seen := map[string]bool{}
	for _, entry := range []struct {
		p *string
		v string
	}{{&c.Cluster, "cluster"}, {&c.Namespace, "namespace"}, {&c.Pod, "pod"}, {&c.PodUID, "uid"}, {&c.Container, "container"}, {&c.Deployment, "deployment"}, {&c.DeploymentUID, "deployment_uid"}, {&c.ReplicaSet, "replicaset"}, {&c.OwnerName, "owner_name"}, {&c.OwnerKind, "owner_kind"}, {&c.Controller, "owner_is_controller"}} {
		if *entry.p == "" {
			*entry.p = entry.v
		}
		if len(*entry.p) > 128 || !labelName.MatchString(*entry.p) || strings.HasPrefix(*entry.p, "__") || seen[*entry.p] {
			return c, failure("mapping_configuration")
		}
		seen[*entry.p] = true
	}
	return c, nil
}

type OwnershipSeries struct {
	Pods        []Series
	ReplicaSets []Series
}
type MappingResult struct {
	Series   []Series
	Status   string
	Source   string
	Digest   string
	Members  int
	Excluded int
}
type mappingTarget struct {
	ID, Cluster, Namespace, Name, UID string
	Containers                        []string
}

func targetForMapping(r resource.Resource) (mappingTarget, error) {
	ns, name, err := deploymentIdentity(r)
	uid, _ := r.Metadata["uid"].(string)
	if err != nil || r.ID == "" || r.Account == "" || uid == "" || r.Provider != resource.ProviderKubernetes || r.Type != resource.TypeKubernetesDeployment {
		return mappingTarget{}, failure("mapping_resource_identity")
	}
	if native, ok := r.Metadata["provider_instance_id"].(string); ok && native != uid {
		return mappingTarget{}, failure("mapping_resource_identity")
	}
	t := mappingTarget{ID: r.ID, Cluster: r.Account, Namespace: ns, Name: name, UID: uid, Containers: []string{}}
	mapping, _ := r.Metadata["mapping"].(map[string]any)
	if mapping["version"] != "kubernetes-deployment-mapper-v1" {
		return t, failure("mapping_container_inventory")
	}
	containers, _ := mapping["containers"].([]any)
	for _, raw := range containers {
		c, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		kind, _ := c["kind"].(string)
		name, _ := c["name"].(string)
		restart, _ := c["restart_policy"].(string)
		if name != "" && (kind == "container" || kind == "init_container" && restart == "Always") {
			t.Containers = append(t.Containers, name)
		}
	}
	sort.Strings(t.Containers)
	if len(t.Containers) == 0 || len(t.Containers) > 256 {
		return t, failure("mapping_container_inventory")
	}
	return t, nil
}

// MapSeries is pure: callers supply ownership facts from the same query interval.
// Every accepted point retains pod/container dimensions until later aggregation.
func MapSeries(r resource.Resource, signal string, input []Series, owners OwnershipSeries, cfg MappingConfig) MappingResult {
	result := MappingResult{Status: "mapped", Source: cfg.Mode, Series: []Series{}}
	unavailable := func(code string) MappingResult {
		return MappingResult{Status: code, Source: cfg.Mode, Series: nil, Excluded: result.Excluded}
	}
	cfg, err := resolveMapping(cfg)
	if err != nil {
		return unavailable("invalid_label_configuration")
	}
	result.Source = cfg.Mode
	target, err := targetForMapping(r)
	if err != nil {
		return unavailable("resource_identity_missing")
	}
	if len(input) > 10000 || len(owners.Pods) > 10000 || len(owners.ReplicaSets) > 10000 {
		return unavailable("mapping_limit")
	}
	podSignal := signal == "evicted_pods"
	index, code := indexOwners(owners, cfg)
	if code != "" {
		return unavailable(code)
	}
	allowed := map[string]bool{}
	for _, name := range target.Containers {
		allowed[name] = true
	}
	seen := map[string]bool{}
	members := map[string]bool{}
	facts := []string{}
	pointCount := 0
	for _, series := range input {
		metric := series.Metric
		if !validIdentityLabels(metric, cfg) {
			return unavailable("invalid_identity_label")
		}
		if metric[cfg.Cluster] == "" || metric[cfg.Namespace] == "" || metric[cfg.Pod] == "" {
			return unavailable("identity_labels_missing")
		}
		if metric[cfg.Cluster] != target.Cluster || metric[cfg.Namespace] != target.Namespace {
			return unavailable("cross_scope_series")
		}
		container := metric[cfg.Container]
		if podSignal {
			container = ""
		}
		if !podSignal && (container == "" || container == "POD" || !allowed[container]) {
			result.Excluded++
			continue
		}
		acceptedByUID := map[string]*Series{}
		for _, point := range series.Points {
			if signal == "inventory" && point.Value != 1 {
				return unavailable("invalid_inventory")
			}
			pointCount++
			if pointCount > 100000 {
				return unavailable("mapping_limit")
			}
			deployment, uid, podUID := metric[cfg.Deployment], metric[cfg.DeploymentUID], metric[cfg.PodUID]
			if cfg.Mode == "owner_chain" {
				var code string
				deployment, uid, podUID, code = resolveOwner(metric, point.Timestamp, index, cfg)
				if code != "" {
					return unavailable(code)
				}
				if metric[cfg.Deployment] != "" && metric[cfg.Deployment] != deployment || metric[cfg.DeploymentUID] != "" && metric[cfg.DeploymentUID] != uid {
					return unavailable("conflicting_ownership")
				}
			}
			if deployment == "" || uid == "" || podUID == "" {
				return unavailable("ownership_labels_missing")
			}
			if deployment != target.Name {
				result.Excluded++
				continue
			}
			if uid != target.UID {
				return unavailable("deployment_instance_mismatch")
			}
			member := metric[cfg.Pod] + "\x00" + podUID + "\x00" + container
			key := metric[cfg.Pod] + "\x00" + container + "\x00" + point.Timestamp.UTC().Format(time.RFC3339Nano)
			if seen[key] {
				return unavailable("ambiguous_series")
			}
			seen[key] = true
			members[member] = true
			facts = append(facts, member+"\x00"+key)
			accepted := acceptedByUID[podUID]
			if accepted == nil {
				accepted = &Series{Metric: map[string]string{}, Points: []Point{}}
				for k, v := range metric {
					accepted.Metric[k] = v
				}
				accepted.Metric[cfg.PodUID] = podUID
				accepted.Metric[cfg.Deployment] = deployment
				accepted.Metric[cfg.DeploymentUID] = uid
				acceptedByUID[podUID] = accepted
			}
			accepted.Points = append(accepted.Points, point)
		}
		uids := []string{}
		for uid := range acceptedByUID {
			uids = append(uids, uid)
		}
		sort.Strings(uids)
		for _, uid := range uids {
			result.Series = append(result.Series, *acceptedByUID[uid])
		}
	}
	if len(members) == 0 {
		return unavailable("no_matching_series")
	}
	sort.Strings(facts)
	encoded, _ := json.Marshal(struct {
		Version string
		Config  MappingConfig
		Target  mappingTarget
		Members []string
		Owners  []string
	}{MappingVersion, cfg, target, facts, index.facts})
	digest := sha256.Sum256(encoded)
	result.Digest = hex.EncodeToString(digest[:])
	result.Members = len(members)
	return result
}

type ownerFact struct {
	name, uid, kind, controller string
	count                       int
}
type ownershipIndex struct {
	pods, replicaSets map[string]ownerFact
	facts             []string
}

func ownershipKey(cluster, namespace, name string, at time.Time) string {
	return cluster + "\x00" + namespace + "\x00" + name + "\x00" + at.UTC().Format(time.RFC3339Nano)
}
func indexOwners(owners OwnershipSeries, c MappingConfig) (ownershipIndex, string) {
	out := ownershipIndex{pods: map[string]ownerFact{}, replicaSets: map[string]ownerFact{}, facts: []string{}}
	count := 0
	for _, group := range []struct {
		series []Series
		pod    bool
	}{{owners.Pods, true}, {owners.ReplicaSets, false}} {
		for _, s := range group.series {
			if s.Metric[c.Controller] != "true" {
				continue
			}
			if !validIdentityLabels(s.Metric, c) {
				return out, "invalid_identity_label"
			}
			name := s.Metric[c.ReplicaSet]
			uid := s.Metric[c.DeploymentUID]
			table := out.replicaSets
			if group.pod {
				name = s.Metric[c.Pod]
				uid = s.Metric[c.PodUID]
				table = out.pods
			}
			for _, p := range s.Points {
				count++
				if count > 100000 {
					return out, "mapping_limit"
				}
				if p.Value != 1 {
					continue
				}
				key := ownershipKey(s.Metric[c.Cluster], s.Metric[c.Namespace], name, p.Timestamp)
				value := ownerFact{name: s.Metric[c.OwnerName], uid: uid, kind: s.Metric[c.OwnerKind], controller: s.Metric[c.Controller], count: table[key].count + 1}
				table[key] = value
				encoded, _ := json.Marshal([]string{key, value.name, value.uid, value.kind, value.controller})
				out.facts = append(out.facts, string(encoded))
			}
		}
	}
	sort.Strings(out.facts)
	return out, ""
}
func resolveOwner(metric map[string]string, at time.Time, index ownershipIndex, c MappingConfig) (string, string, string, string) {
	pod, ok := index.pods[ownershipKey(metric[c.Cluster], metric[c.Namespace], metric[c.Pod], at)]
	if !ok {
		return "", "", "", "pod_owner_missing"
	}
	if pod.count != 1 {
		return "", "", "", "ambiguous_pod_owner"
	}
	if pod.kind != "ReplicaSet" || pod.controller != "true" {
		return "", "", "", "unsupported_owner"
	}
	if pod.uid == "" || pod.name == "" {
		return "", "", "", "owner_identity_missing"
	}
	if metric[c.PodUID] != "" && metric[c.PodUID] != pod.uid {
		return "", "", "", "pod_instance_mismatch"
	}
	rs, ok := index.replicaSets[ownershipKey(metric[c.Cluster], metric[c.Namespace], pod.name, at)]
	if !ok {
		return "", "", "", "replicaset_owner_missing"
	}
	if rs.count != 1 {
		return "", "", "", "ambiguous_replicaset_owner"
	}
	if rs.kind != "Deployment" || rs.controller != "true" {
		return "", "", "", "unsupported_owner"
	}
	if rs.name == "" || rs.uid == "" {
		return "", "", "", "deployment_uid_missing"
	}
	return rs.name, rs.uid, pod.uid, ""
}

func validateMembership(mapped, inventory MappingResult, signal string, c MappingConfig) MappingResult {
	fail := func(code string) MappingResult {
		return MappingResult{Status: code, Source: mapped.Source, Excluded: mapped.Excluded}
	}
	if inventory.Status != "mapped" {
		return fail("inventory_" + inventory.Status)
	}
	keys := func(series []Series) map[string]map[string]bool {
		out := map[string]map[string]bool{}
		for _, s := range series {
			for _, p := range s.Points {
				timestamp := p.Timestamp.UTC().Format(time.RFC3339Nano)
				member := s.Metric[c.Pod] + "\x00" + s.Metric[c.PodUID]
				if signal != "evicted_pods" {
					member += "\x00" + s.Metric[c.Container]
				}
				if out[timestamp] == nil {
					out[timestamp] = map[string]bool{}
				}
				out[timestamp][member] = true
			}
		}
		return out
	}
	observed, expected := keys(mapped.Series), keys(inventory.Series)
	for at, members := range expected {
		if len(observed[at]) != len(members) {
			return fail("incomplete_membership")
		}
		for member := range members {
			if !observed[at][member] {
				return fail("incomplete_membership")
			}
		}
	}
	for at := range observed {
		if expected[at] == nil {
			return fail("inventory_timestamp_missing")
		}
	}
	digest := sha256.Sum256([]byte(mapped.Digest + ":" + inventory.Digest))
	mapped.Digest = hex.EncodeToString(digest[:])
	return mapped
}

func validIdentityLabels(metric map[string]string, c MappingConfig) bool {
	for _, key := range []string{c.Cluster, c.Namespace, c.Pod, c.PodUID, c.Container, c.Deployment, c.DeploymentUID, c.ReplicaSet, c.OwnerName, c.OwnerKind, c.Controller} {
		value := metric[key]
		if len(value) > 512 || strings.IndexFunc(value, unicode.IsControl) >= 0 {
			return false
		}
	}
	return true
}

func validateInventory(inventory MappingResult, target mappingTarget, c MappingConfig) MappingResult {
	if inventory.Status != "mapped" {
		return inventory
	}
	counts := map[string]int{}
	for _, s := range inventory.Series {
		for _, p := range s.Points {
			key := ownershipKey(s.Metric[c.Cluster], s.Metric[c.Namespace], s.Metric[c.Pod], p.Timestamp)
			counts[key]++
		}
	}
	for _, count := range counts {
		if count != len(target.Containers) {
			return MappingResult{Status: "incomplete_container_inventory", Source: inventory.Source}
		}
	}
	return inventory
}
