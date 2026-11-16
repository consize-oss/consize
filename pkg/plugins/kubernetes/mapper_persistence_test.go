package kubernetes_test

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/consize-oss/consize/internal/store"
	k8s "github.com/consize-oss/consize/pkg/plugins/kubernetes"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	quantity "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestMappedExtensionsSurviveDurableRestart(t *testing.T) {
	deployment := appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Namespace: "payments", Name: "app", UID: "fixture-uid", Labels: map[string]string{"owner": "payments", "environment": "staging"}}, Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: quantity.MustParse("500m"), corev1.ResourceMemory: quantity.MustParse("512Mi")}}}}}}}}
	res, err := k8s.MapDeployment(deployment, k8s.MappingConfig{ClusterID: "cluster", Location: "local"}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "state.json")
	st, err := store.OpenDurable(path)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := st.UpsertResource(context.Background(), res)
	if err != nil {
		t.Fatal(err)
	}
	if err = st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = store.OpenDurable(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	got, err := st.GetResource(context.Background(), saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(saved)
	b, _ := json.Marshal(got)
	if !bytes.Equal(a, b) {
		t.Fatal("extension or quantities changed after persistence")
	}
	if !reflect.DeepEqual(got.Metadata["mapping"], saved.Metadata["mapping"]) {
		t.Fatal("mapping changed")
	}
}
