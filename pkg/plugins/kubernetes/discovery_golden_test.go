package kubernetes

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	quantity "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestNativeDiscoveryObservationGolden(t *testing.T) {
	deployment := deploymentFixture("alpha", "api", "uid-1")
	deployment.ResourceVersion = "12"
	deployment.Generation = 3
	deployment.CreationTimestamp = metav1.NewTime(time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC))
	controller := true
	deployment.OwnerReferences = []metav1.OwnerReference{{APIVersion: "argoproj.io/v1alpha1", Kind: "Rollout", Name: "api", UID: types.UID("owner-1"), Controller: &controller}}
	deployment.Annotations = map[string]string{"example.io/team": "platform", "kubectl.kubernetes.io/last-applied-configuration": "fixture-private-manifest"}
	deployment.Spec.Selector = &metav1.LabelSelector{MatchLabels: map[string]string{"app": "chosen"}}
	deployment.Spec.Template.Labels = map[string]string{"app": "chosen"}
	deployment.Spec.Template.Annotations = map[string]string{"example.io/team": "worker", "example.io/note": "fixture-private-template"}
	replicas := int32(2)
	deployment.Spec.Replicas = &replicas
	deployment.Status.Replicas = 2
	deployment.Status.ReadyReplicas = 1
	deployment.Status.UpdatedReplicas = 1
	deployment.Status.AvailableReplicas = 1
	deployment.Status.UnavailableReplicas = 1
	deployment.Status.ObservedGeneration = 2
	deployment.Spec.Template.Spec.Containers[0].Resources = corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: quantity.MustParse("500m"), corev1.ResourceMemory: quantity.MustParse("512Mi")}, Limits: corev1.ResourceList{corev1.ResourceMemory: quantity.MustParse("1Gi")}}
	deployment.Spec.Template.Spec.Containers[0].Env = []corev1.EnvVar{{Name: "FIXTURE_PRIVATE_VALUE", Value: "fixture-private-environment"}}
	restart := corev1.ContainerRestartPolicyAlways
	deployment.Spec.Template.Spec.InitContainers = []corev1.Container{{Name: "bootstrap", RestartPolicy: &restart, Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: quantity.MustParse("10m")}}}}
	got, err := captureDeployment(deployment, "cluster-one", []string{"example.io/team"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("testdata/discovery-observation.json")
	if err != nil {
		t.Fatal(err)
	}
	var want map[string]any
	if err = json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		encoded, _ := json.MarshalIndent(got, "", "  ")
		t.Fatalf("native discovery capture changed:\n%s", encoded)
	}
}
