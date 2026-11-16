package kubernetes

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/consize-oss/consize/internal/policy"
	"github.com/consize-oss/consize/pkg/plugin"
	"github.com/consize-oss/consize/pkg/resource"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	quantity "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var mappingTime = time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)

func mappingFixture() (MappingConfig, resource.Resource, error) {
	d := deploymentFixture("payments", "checkout", "uid-checkout")
	d.Generation = 4
	d.CreationTimestamp = metav1.NewTime(mappingTime.Add(-time.Hour))
	d.Labels = map[string]string{"consize.io/owner": "platform", "owner": "ignored", "consize.io/environment": "production", "environment": "development", "custom.example/team": "payments"}
	d.Annotations = map[string]string{"example.io/public": "preserved", "example.io/private": "fixture-private-annotation"}
	d.Spec.Template.Spec.Containers = []corev1.Container{
		{Name: "app", Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: quantity.MustParse("500m"), corev1.ResourceMemory: quantity.MustParse("512Mi"), corev1.ResourceEphemeralStorage: quantity.MustParse("1Gi"), corev1.ResourceName("example.io/gpu"): quantity.MustParse("1")}, Limits: corev1.ResourceList{corev1.ResourceCPU: quantity.MustParse("1"), corev1.ResourceMemory: quantity.MustParse("1Gi")}}},
		{Name: "sidecar", Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: quantity.MustParse("0.0001"), corev1.ResourceMemory: quantity.MustParse("0")}}},
	}
	restart := corev1.ContainerRestartPolicyAlways
	d.Spec.Template.Spec.InitContainers = []corev1.Container{{Name: "bootstrap", RestartPolicy: &restart, Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: quantity.MustParse("20m")}}}}
	cfg := MappingConfig{ClusterID: "cluster-one", Location: "local", AnnotationAllowlist: []string{"example.io/public"}}
	r, e := MapDeployment(d, cfg, mappingTime)
	return cfg, r, e
}
func TestDeploymentMappingGolden(t *testing.T) {
	_, got, err := mappingFixture()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	expected, err := os.ReadFile("testdata/deployment-mapping.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.TrimSpace(raw), bytes.TrimSpace(expected)) {
		t.Fatalf("mapping golden changed:\n%s", raw)
	}
	if strings.Contains(string(raw), "fixture-private-annotation") {
		t.Fatal("private annotation leaked")
	}
	if _, err = got.Normalize(mappingTime); err != nil {
		t.Fatal("invalid universal resource", err)
	}
}
func TestMappingIdentityAndMutableMetadata(t *testing.T) {
	cfg := MappingConfig{ClusterID: "cluster", Location: "local"}
	d := deploymentFixture("alpha", "app", "uid-original")
	mapID := func(cfg MappingConfig) resource.Resource {
		t.Helper()
		r, e := MapDeployment(d, cfg, mappingTime)
		if e != nil {
			t.Fatal(e)
		}
		r, e = r.Normalize(mappingTime)
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	first := mapID(cfg)
	d.Labels = map[string]string{"owner": "team", "environment": "staging"}
	d.Generation++
	replicas := int32(9)
	d.Spec.Replicas = &replicas
	d.Spec.Template.Spec.Containers[0].Resources.Requests = corev1.ResourceList{corev1.ResourceCPU: quantity.MustParse("2")}
	changed := mapID(cfg)
	if changed.ID != first.ID {
		t.Fatal("mutable update changed canonical ID")
	}
	d.UID = "uid-replacement"
	replacement := mapID(cfg)
	if replacement.ID != first.ID || !errors.Is(resource.ValidateInstanceUpdate(first, replacement), resource.ErrIdentityConflict) {
		t.Fatal("replacement silently changes identity or passes guard")
	}
	d.Namespace = "beta"
	other := mapID(cfg)
	if other.ID == first.ID {
		t.Fatal("namespace collision")
	}
	cfg.ClusterID = "another-cluster"
	if mapID(cfg).ID == other.ID {
		t.Fatal("cluster collision")
	}
}
func TestMetadataPrecedenceAndPolicy(t *testing.T) {
	for _, tc := range []struct {
		name               string
		labels             map[string]string
		env, owner, source string
	}{
		{"unlabeled", nil, "unknown", "unassigned", "fallback"},
		{"legacy", map[string]string{"environment": "staging", "owner": "payments"}, "staging", "payments", "label:environment"},
		{"primary", map[string]string{"consize.io/environment": "production", "environment": "development", "consize.io/owner": "platform", "owner": "other"}, "production", "platform", "label:consize.io/environment"},
		{"blank primary", map[string]string{"consize.io/environment": "  ", "environment": "development", "owner": "team"}, "development", "team", "label:environment"},
		{"unknown sentinel", map[string]string{"environment": "unknown", "owner": "UNASSIGNED"}, "unknown", "unassigned", "label:environment"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := deploymentFixture("alpha", "api", "uid-1")
			d.Labels = tc.labels
			r, e := MapDeployment(d, MappingConfig{ClusterID: "cluster", Location: "local"}, mappingTime)
			if e != nil {
				t.Fatal(e)
			}
			if r.Environment != tc.env || r.Owner != tc.owner {
				t.Fatalf("metadata %s/%s", r.Environment, r.Owner)
			}
			m := r.Metadata["mapping"].(map[string]any)
			provenance := m["provenance"].(map[string]any)
			env := provenance["environment"].(map[string]any)
			if env["source"] != tc.source || env["confirmed"] != (tc.env != "unknown") {
				t.Fatal(env)
			}
			r, e = r.Normalize(mappingTime)
			if e != nil {
				t.Fatal(e)
			}
			engine := policy.NewEngine()
			engine.AutoApplyNonProduction = true
			decision := engine.Evaluate(context.Background(), r, plugin.Manifest{ID: "fixture", SupportedResourceTypes: []string{resource.TypeKubernetesDeployment}}, "auto", "")
			if (tc.env == "unknown" || tc.env == "production" || tc.owner == "unassigned") && decision.Decision == policy.DecisionAutoApply {
				t.Fatal("unconfirmed metadata enabled auto apply")
			}
		})
	}
}
func TestMappingRejectsInvalidInputs(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		edit       func(*MappingConfig, *corev1.Container, *metav1.ObjectMeta)
	}{
		{"missing uid", "uid_required_or_invalid", func(_ *MappingConfig, _ *corev1.Container, m *metav1.ObjectMeta) { m.UID = "" }},
		{"missing cluster", "invalid_cluster_identity", func(c *MappingConfig, _ *corev1.Container, _ *metav1.ObjectMeta) { c.ClusterID = "" }},
		{"invalid namespace", "invalid_object_identity", func(_ *MappingConfig, _ *corev1.Container, m *metav1.ObjectMeta) { m.Namespace = "wrong/name" }},
		{"invalid environment", "invalid_environment", func(_ *MappingConfig, _ *corev1.Container, m *metav1.ObjectMeta) { m.Labels["environment"] = "prod" }},
		{"negative", "negative_quantity", func(_ *MappingConfig, c *corev1.Container, _ *metav1.ObjectMeta) {
			c.Resources.Requests = corev1.ResourceList{corev1.ResourceCPU: quantity.MustParse("-1")}
		}},
		{"overflow", "quantity_overflow", func(_ *MappingConfig, c *corev1.Container, _ *metav1.ObjectMeta) {
			c.Resources.Requests = corev1.ResourceList{corev1.ResourceCPU: quantity.MustParse("9223372036854775807")}
		}},
		{"extreme exponent", "quantity_overflow", func(_ *MappingConfig, c *corev1.Container, _ *metav1.ObjectMeta) {
			c.Resources.Requests = corev1.ResourceList{corev1.ResourceCPU: quantity.MustParse("1e100")}
		}},
		{"request over limit", "request_exceeds_limit", func(_ *MappingConfig, c *corev1.Container, _ *metav1.ObjectMeta) {
			c.Resources.Requests = corev1.ResourceList{corev1.ResourceCPU: quantity.MustParse("2")}
			c.Resources.Limits = corev1.ResourceList{corev1.ResourceCPU: quantity.MustParse("1")}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := MappingConfig{ClusterID: "cluster", Location: "local"}
			d := deploymentFixture("alpha", "api", "uid-1")
			tc.edit(&cfg, &d.Spec.Template.Spec.Containers[0], &d.ObjectMeta)
			_, e := MapDeployment(d, cfg, mappingTime)
			var failure *MappingError
			if !errors.As(e, &failure) || failure.Code != tc.code || !errors.Is(e, resource.ErrInvalidResource) {
				t.Fatalf("error %v", e)
			}
		})
	}
}

func TestMappingQuantitySemanticsAndIsolation(t *testing.T) {
	_, r, err := mappingFixture()
	if err != nil {
		t.Fatal(err)
	}
	if r.CurrentState["cpu_request_millicores"] != int64(501) || r.CurrentState["memory_request_bytes"] != int64(536870912) {
		t.Fatal("legacy regular-container aggregates changed", r.CurrentState)
	}
	mapped := r.Metadata["mapping"].(map[string]any)
	complete := mapped["aggregate_complete"].(map[string]any)
	if complete["memory_limit_bytes"] != false || complete["memory_request_bytes"] != true {
		t.Fatal("omitted limit or explicit zero lost", complete)
	}
	entries := mapped["containers"].([]any)
	sidecar := entries[1].(map[string]any)
	cpu := sidecar["requests"].([]any)[0].(map[string]any)
	if cpu["value"] != "0.1" || cpu["unit"] != "millicores" || len(sidecar["limits"].([]any)) != 0 {
		t.Fatal("exact fractional CPU or omission lost", sidecar)
	}
	d := deploymentFixture("alpha", "api", "uid-1")
	d.Labels["owner"] = "team"
	got, e := MapDeployment(d, MappingConfig{ClusterID: "cluster", Location: "local"}, mappingTime)
	if e != nil {
		t.Fatal(e)
	}
	got.Labels["owner"] = "changed"
	if d.Labels["owner"] != "team" {
		t.Fatal("mapper aliases caller labels")
	}
}
func TestMapperFailureDiscardsDiscoveryBatch(t *testing.T) {
	good := deploymentFixture("alpha", "good", "uid-good")
	bad := deploymentFixture("alpha", "invalid", "uid-invalid")
	bad.Labels["environment"] = "invalid"
	client := &discoveryTestClient{list: func(context.Context, string, metav1.ListOptions) (*appsv1.DeploymentList, error) {
		return page(good, bad), nil
	}}
	p := discoveryPlugin(t, client, DiscoveryConfig{Namespaces: []string{"alpha"}})
	batch, e := p.DiscoverSnapshot(context.Background())
	var mapped *MappingError
	if !errors.As(e, &mapped) || mapped.Code != "invalid_environment" || batch.Scan.Complete || batch.Observations != nil {
		t.Fatal("mapping failure exposed partial discovery", e)
	}
	requireScanCode(t, e, "invalid_observation")
}
func TestUnlabeledDeploymentMappingGolden(t *testing.T) {
	d := deploymentFixture("prod-payments", "checkout", "uid-production-like")
	d.Labels = nil
	r, e := MapDeployment(d, MappingConfig{ClusterID: "cluster-prod", Location: "us-east-1"}, mappingTime)
	if e != nil {
		t.Fatal(e)
	}
	raw, e := json.MarshalIndent(r, "", "  ")
	if e != nil {
		t.Fatal(e)
	}
	expected, e := os.ReadFile("testdata/deployment-mapping-unlabeled.json")
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(bytes.TrimSpace(raw), bytes.TrimSpace(expected)) {
		t.Fatalf("unlabeled mapping golden changed:\n%s", raw)
	}
	if r.Environment != resource.EnvUnknown || r.Owner != resource.OwnerUnassigned {
		t.Fatal("production-like name inferred metadata")
	}
}

func TestMappingRejectsAggregateOverflowAndDuplicateContainers(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		containers []corev1.Container
	}{
		{"sum overflow", "quantity_overflow", []corev1.Container{{Name: "a", Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceMemory: quantity.MustParse("9000000000000000000")}}}, {Name: "b", Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceMemory: quantity.MustParse("9000000000000000000")}}}}},
		{"duplicate names", "invalid_or_duplicate_container", []corev1.Container{{Name: "app"}, {Name: "app"}}},
		{"no containers", "container_required", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := deploymentFixture("alpha", "api", "uid-1")
			d.Spec.Template.Spec.Containers = tc.containers
			_, e := MapDeployment(d, MappingConfig{ClusterID: "cluster", Location: "local"}, mappingTime)
			var failure *MappingError
			if !errors.As(e, &failure) || failure.Code != tc.code {
				t.Fatalf("error=%v", e)
			}
		})
	}
}
