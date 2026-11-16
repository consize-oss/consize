package prometheus

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/consize-oss/consize/pkg/evidence"
	"github.com/consize-oss/consize/pkg/resource"
)

var mapAt = time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)

func mapResource() resource.Resource {
	return mappingTestResource(resource.Resource{ID: "resource:fixture", Type: resource.TypeKubernetesDeployment, Name: "checkout-api", Metadata: map[string]any{"namespace": "payments", "name": "checkout-api"}})
}
func mapSeries(pod, container string, value float64) Series {
	labels := mappingTestLabels("payments", "checkout-api")
	labels["pod"] = pod
	labels["uid"] = pod + "-uid"
	labels["container"] = container
	return Series{Metric: labels, Points: []Point{{Timestamp: mapAt, Value: value}}}
}
func ownerFixture(pod, rs string) OwnershipSeries {
	return OwnershipSeries{Pods: []Series{{Metric: map[string]string{"cluster": "fixture-cluster", "namespace": "payments", "pod": pod, "uid": pod + "-uid", "owner_kind": "ReplicaSet", "owner_name": rs, "owner_is_controller": "true"}, Points: []Point{{Timestamp: mapAt, Value: 1}}}}, ReplicaSets: []Series{{Metric: map[string]string{"cluster": "fixture-cluster", "namespace": "payments", "replicaset": rs, "owner_name": "checkout-api", "owner_kind": "Deployment", "owner_is_controller": "true", "deployment_uid": "fixture-deployment-uid"}, Points: []Point{{Timestamp: mapAt, Value: 1}}}}}
}
func TestMappingRejectsUnsafeIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		edit       func(*Series)
	}{
		{"missing cluster", "identity_labels_missing", func(s *Series) { delete(s.Metric, "cluster") }},
		{"cross cluster", "cross_scope_series", func(s *Series) { s.Metric["cluster"] = "other" }},
		{"cross namespace", "cross_scope_series", func(s *Series) { s.Metric["namespace"] = "other" }},
		{"missing owner", "ownership_labels_missing", func(s *Series) { delete(s.Metric, "deployment") }},
		{"missing pod uid", "ownership_labels_missing", func(s *Series) { delete(s.Metric, "uid") }},
		{"replaced deployment", "deployment_instance_mismatch", func(s *Series) { s.Metric["deployment_uid"] = "old-deployment" }},
		{"bad delimiter", "invalid_identity_label", func(s *Series) { s.Metric["pod"] = "bad\x00pod" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := mapSeries("checkout-api-rs-pod", "app", 1)
			tc.edit(&s)
			got := MapSeries(mapResource(), "cpu_cores", []Series{s}, OwnershipSeries{}, MappingConfig{})
			if got.Status != tc.want || got.Series != nil {
				t.Fatalf("got %+v", got)
			}
		})
	}
	r := MapSeries(mapResource(), "cpu_cores", []Series{mapSeries("pod", "app", 1), mapSeries("pod", "app", 2)}, OwnershipSeries{}, MappingConfig{})
	if r.Status != "ambiguous_series" {
		t.Fatal("duplicate scrape accepted", r.Status)
	}
}
func TestPrefixCollisionDoesNotAttributeAnotherDeployment(t *testing.T) {
	s := mapSeries("checkout-api-extra-rs-pod", "app", 123)
	s.Metric["deployment"] = "checkout-api-extra"
	got := MapSeries(mapResource(), "cpu_cores", []Series{s}, OwnershipSeries{}, MappingConfig{})
	if got.Status != "no_matching_series" || len(got.Series) != 0 {
		t.Fatal(got)
	}
}
func TestOwnerChainValidatesAtEveryTimestamp(t *testing.T) {
	s := mapSeries("pod-a", "app", 1)
	delete(s.Metric, "deployment")
	delete(s.Metric, "deployment_uid")
	delete(s.Metric, "uid")
	owners := ownerFixture("pod-a", "rollout-123")
	cfg := MappingConfig{Mode: "owner_chain"}
	got := MapSeries(mapResource(), "cpu_cores", []Series{s}, owners, cfg)
	if got.Status != "mapped" || got.Series[0].Metric["uid"] != "pod-a-uid" {
		t.Fatal(got.Status)
	}
	for _, tc := range []struct {
		name, want string
		edit       func(*OwnershipSeries)
	}{
		{"missing RS", "replicaset_owner_missing", func(o *OwnershipSeries) { o.ReplicaSets = nil }},
		{"missing deployment uid", "deployment_uid_missing", func(o *OwnershipSeries) { delete(o.ReplicaSets[0].Metric, "deployment_uid") }},
		{"duplicate pod owner", "ambiguous_pod_owner", func(o *OwnershipSeries) { o.Pods = append(o.Pods, o.Pods[0]) }},
		{"duplicate RS owner", "ambiguous_replicaset_owner", func(o *OwnershipSeries) { o.ReplicaSets = append(o.ReplicaSets, o.ReplicaSets[0]) }},
		{"wrong owner kind", "unsupported_owner", func(o *OwnershipSeries) { o.Pods[0].Metric["owner_kind"] = "Job" }},
		{"owner from another time", "pod_owner_missing", func(o *OwnershipSeries) { o.Pods[0].Points[0].Timestamp = mapAt.Add(-time.Minute) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := ownerFixture("pod-a", "rollout-123")
			tc.edit(&o)
			result := MapSeries(mapResource(), "cpu_cores", []Series{s}, o, cfg)
			if result.Status != tc.want {
				t.Fatalf("got=%s want=%s", result.Status, tc.want)
			}
		})
	}
}
func TestRolloutGenerationsAndContainers(t *testing.T) {
	r := mapResource()
	r.Metadata["mapping"].(map[string]any)["containers"] = []any{map[string]any{"kind": "container", "name": "app"}, map[string]any{"kind": "container", "name": "sidecar"}}
	input := []Series{mapSeries("old-rs-pod", "app", 1), mapSeries("old-rs-pod", "sidecar", 2), mapSeries("new-rs-pod", "app", 4), mapSeries("new-rs-pod", "sidecar", 5), mapSeries("new-rs-pod", "POD", 90), mapSeries("new-rs-pod", "", 90), mapSeries("new-rs-pod", "init-only", 90)}
	got := MapSeries(r, "cpu_cores", input, OwnershipSeries{}, MappingConfig{})
	if got.Status != "mapped" || got.Members != 4 || got.Excluded != 3 {
		t.Fatal(got.Status, got.Members, got.Excluded)
	}
	c, _ := resolveMapping(MappingConfig{})
	reduced := aggregateMapped("cpu_cores", got.Series, c, 2)
	if len(reduced) != 1 || reduced[0].Points[0].Value != 9 {
		t.Fatal("workload aggregation lost per-pod semantics", reduced)
	}
	reversed := append([]Series(nil), input...)
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	other := MapSeries(r, "cpu_cores", reversed, OwnershipSeries{}, MappingConfig{})
	if got.Digest != other.Digest {
		t.Fatal("input ordering changed mapping identity")
	}
}
func TestMappingAliasesAndConfiguration(t *testing.T) {
	c := MappingConfig{Cluster: "source_cluster", Namespace: "k8s_namespace", Pod: "pod_name", PodUID: "pod_uid", Container: "container_name", Deployment: "workload", DeploymentUID: "workload_uid"}
	resolved, e := resolveMapping(c)
	if e != nil {
		t.Fatal(e)
	}
	s := mapSeries("pod", "app", 1)
	for old, next := range map[string]string{"cluster": c.Cluster, "namespace": c.Namespace, "pod": c.Pod, "uid": c.PodUID, "container": c.Container, "deployment": c.Deployment, "deployment_uid": c.DeploymentUID} {
		s.Metric[next] = s.Metric[old]
		delete(s.Metric, old)
	}
	got := MapSeries(mapResource(), "cpu_cores", []Series{s}, OwnershipSeries{}, resolved)
	if got.Status != "mapped" {
		t.Fatal(got.Status)
	}
	for _, bad := range []MappingConfig{{Mode: "guess"}, {Cluster: "bad-label"}, {Pod: "namespace"}, {Cluster: "__name__"}} {
		if _, e = resolveMapping(bad); e == nil {
			t.Fatal("invalid alias accepted", bad)
		}
	}
}
func TestMembershipCannotMaskMissingOrExtraContainers(t *testing.T) {
	c, _ := resolveMapping(MappingConfig{})
	r := mapResource()
	inventory := MapSeries(r, "inventory", []Series{mapSeries("pod-a", "app", 1), mapSeries("pod-b", "app", 1)}, OwnershipSeries{}, c)
	one := MapSeries(r, "cpu_cores", []Series{mapSeries("pod-a", "app", 1)}, OwnershipSeries{}, c)
	if validateMembership(one, inventory, "cpu_cores", c).Status != "incomplete_membership" {
		t.Fatal("missing pod hidden")
	}
}

type rejectedMappingClient struct{}

func (rejectedMappingClient) QueryRange(_ context.Context, _ string, start, end time.Time, step time.Duration) ([]Series, error) {
	s := mapSeries("prefix-pod", "app", 0)
	delete(s.Metric, "deployment_uid")
	s.Points = []Point{{Timestamp: end, Value: 0}}
	return []Series{s}, nil
}
func TestMappingFailureProducesUnavailableEvidence(t *testing.T) {
	p := NewWithClient(rejectedMappingClient{}, Config{Step: time.Minute})
	end := time.Now().UTC()
	snapshot, err := p.ReadMetricsBetween(context.Background(), mapResource(), end.Add(-2*time.Minute), end)
	if err != nil {
		t.Fatal(err)
	}
	if err = snapshot.ValidateRecords(); err != nil {
		t.Fatal(err)
	}
	for _, record := range snapshot.Records {
		if record.Quality != evidence.Unavailable || record.Value != nil || record.Extensions["mapping_version"] != MappingVersion || record.Extensions["mapping_status"] != "ownership_labels_missing" {
			t.Fatal(record.Signal, record.Quality, record.Extensions)
		}
	}
	for key := range snapshot.Signals {
		if strings.HasSuffix(key, "_p95") || strings.HasSuffix(key, "_max") {
			t.Fatal("mapping failure exposed a zero", key)
		}
	}
}
func TestMappingGolden(t *testing.T) {
	raw, err := os.ReadFile("testdata/series-mapping.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Series   []Series `json:"series"`
		Expected struct {
			Status, Source, Digest string
			Members, Excluded      int
		} `json:"expected"`
	}
	if err = json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	got := MapSeries(mapResource(), "cpu_cores", fixture.Series, OwnershipSeries{}, MappingConfig{})
	actual := struct {
		Status, Source, Digest string
		Members, Excluded      int
	}{got.Status, got.Source, got.Digest, got.Members, got.Excluded}
	if !reflect.DeepEqual(actual, fixture.Expected) {
		b, _ := json.MarshalIndent(actual, "", "  ")
		t.Fatalf("mapping golden changed:\n%s", b)
	}
}

func TestRestartableInitSidecarAndNonControllerOwner(t *testing.T) {
	r := mapResource()
	r.Metadata["mapping"].(map[string]any)["containers"] = []any{map[string]any{"kind": "container", "name": "app"}, map[string]any{"kind": "init_container", "name": "proxy", "restart_policy": "Always"}, map[string]any{"kind": "init_container", "name": "setup"}}
	result := MapSeries(r, "cpu_cores", []Series{mapSeries("pod", "app", 1), mapSeries("pod", "proxy", 2), mapSeries("pod", "setup", 5)}, OwnershipSeries{}, MappingConfig{})
	if result.Status != "mapped" || result.Members != 2 || result.Excluded != 1 {
		t.Fatal(result.Status, result.Members, result.Excluded)
	}
	owners := ownerFixture("pod", "rs")
	extra := ownerFixture("pod", "other-rs").Pods[0]
	extra.Metric["owner_is_controller"] = "false"
	owners.Pods = append(owners.Pods, extra)
	s := mapSeries("pod", "app", 1)
	delete(s.Metric, "deployment")
	delete(s.Metric, "deployment_uid")
	if got := MapSeries(mapResource(), "cpu_cores", []Series{s}, owners, MappingConfig{Mode: "owner_chain"}); got.Status != "mapped" {
		t.Fatal("noncontroller reference overrode controller", got.Status)
	}
}
func TestMappingLimitsAndInvalidConfiguration(t *testing.T) {
	input := make([]Series, 10001)
	if got := MapSeries(mapResource(), "cpu_cores", input, OwnershipSeries{}, MappingConfig{}); got.Status != "mapping_limit" {
		t.Fatal(got.Status)
	}
	if _, err := New(Config{BaseURL: "http://127.0.0.1:9090", Mapping: MappingConfig{Cluster: "bad-name"}}); err == nil {
		t.Fatal("invalid production mapping configuration accepted")
	}
}

func TestMappingMatcherEscapesCompleteStringLiteral(t *testing.T) {
	got := mappingMatcher("cluster", "=", "fixture\"} or vector(1) #\\tail")
	want := `cluster="fixture\"} or vector(1) #\\tail"`
	if got != want {
		t.Fatalf("matcher escaping changed: %q", got)
	}
}
