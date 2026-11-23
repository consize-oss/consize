package plugin_test

import (
	"context"
	"testing"
	"time"

	"github.com/consize-oss/consize/pkg/plugin"
	k8splugin "github.com/consize-oss/consize/pkg/plugins/kubernetes"
	"github.com/consize-oss/consize/pkg/resource"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

func TestTwoKubernetesInstancesKeepClusterScopeAndProvenance(t *testing.T) {
	ctx := context.Background()
	manager := plugin.NewManager()
	for _, cluster := range []string{"cluster-a", "cluster-b"} {
		client := fake.NewSimpleClientset(&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "apps", UID: types.UID(cluster + "-uid")}, Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "api"}}}}}})
		provider, err := k8splugin.NewWithPatcherAndConfig(k8splugin.NewK8sPatcherFromClient(client), k8splugin.Config{ClusterID: cluster, Location: "local"})
		if err != nil {
			t.Fatal(err)
		}
		if err := manager.RegisterInstance(ctx, plugin.InstanceConfig{ID: cluster, Required: true, PackageID: k8splugin.ID, Provider: resource.ProviderKubernetes, Account: cluster, Location: "local"}, provider, nil); err != nil {
			t.Fatal(err)
		}
	}
	for _, status := range manager.InstanceStatuses() {
		if status.State != plugin.StateDegraded {
			t.Fatal("unscanned but authorized discovery must start degraded", status.State)
		}
	}
	providers := manager.DiscoveryPlugins()
	if len(providers) != 2 {
		t.Fatal("instances overwritten")
	}
	identities := map[string]bool{}
	for _, provider := range providers {
		scoped, ok := provider.(plugin.ScopedDiscoveryPlugin)
		if !ok {
			t.Fatal("scoped discovery interface lost")
		}
		snapshot, err := scoped.DiscoverSnapshot(ctx)
		if err != nil || len(snapshot.Observations) != 1 {
			t.Fatal(snapshot, err)
		}
		observation := snapshot.Observations[0]
		r := observation.Resource
		r, err = r.Normalize(time.Now().UTC())
		if err != nil {
			t.Fatal(err)
		}
		if r.Account != provider.ID() || observation.PluginID != provider.ID() || snapshot.Scan.PluginID != provider.ID() || r.Producer == nil || r.Producer.InstanceID != provider.ID() || r.Producer.PackageID != k8splugin.ID {
			t.Fatal("cross-instance attribution", snapshot)
		}
		if identities[r.ID] {
			t.Fatal("different clusters collapsed into one resource")
		}
		identities[r.ID] = true
		scope := scoped.DiscoveryScope()
		scope["cluster_id"] = "tampered"
		if scoped.DiscoveryScope()["cluster_id"] != provider.ID() {
			t.Fatal("scope escaped mutable")
		}
	}
}
