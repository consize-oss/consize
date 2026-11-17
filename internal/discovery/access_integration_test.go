package discovery

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/consize-oss/consize/internal/bootstrap"
	"github.com/consize-oss/consize/internal/store"
	"github.com/consize-oss/consize/pkg/plugin"
	k8splugin "github.com/consize-oss/consize/pkg/plugins/kubernetes"
	"github.com/consize-oss/consize/pkg/resource"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

func TestHTTPAccessFailureCannotReconcileExistingRegistry(t *testing.T) {
	var denied atomic.Bool
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if denied.Load() {
			w.WriteHeader(403)
			fmt.Fprint(w, "fixture-private-api-denial")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		replicas := int32(1)
		list := appsv1.DeploymentList{TypeMeta: metav1.TypeMeta{Kind: "DeploymentList", APIVersion: "apps/v1"}, ListMeta: metav1.ListMeta{ResourceVersion: "fixture-version"}, Items: []appsv1.Deployment{{ObjectMeta: metav1.ObjectMeta{Namespace: "alpha", Name: "app", UID: "fixture-uid", CreationTimestamp: metav1.NewTime(time.Now().Add(-time.Hour))}, Spec: appsv1.DeploymentSpec{Replicas: &replicas, Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app"}}}}}}}}
		json.NewEncoder(w).Encode(list)
	}))
	defer server.Close()
	cfg := clientcmdapi.Config{Clusters: map[string]*clientcmdapi.Cluster{"fixture": {Server: server.URL, CertificateAuthorityData: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})}}, AuthInfos: map[string]*clientcmdapi.AuthInfo{"fixture": {Token: "fixture-private-auth-token"}}, Contexts: map[string]*clientcmdapi.Context{"fixture": {Cluster: "fixture", AuthInfo: "fixture"}}, CurrentContext: "fixture"}
	path := filepath.Join(t.TempDir(), "fixture-kubeconfig")
	if err := clientcmd.WriteToFile(cfg, path); err != nil {
		t.Fatal(err)
	}
	provider, err := k8splugin.New(k8splugin.Config{Kubeconfig: path, ClusterID: "fixture-cluster", Location: "local", Discovery: k8splugin.DiscoveryConfig{Namespaces: []string{"alpha"}}})
	if err != nil {
		t.Fatal(err)
	}
	manager := plugin.NewManager()
	manager.RegisterDiscovery(provider)
	st := store.NewMemory()
	service := NewWithConfig(st, manager, bootstrap.DiscoveryScheduleConfig{StaleAfter: "0s", TombstoneAfter: "1ms"})
	ctx := context.Background()
	results, err := service.Run(ctx)
	if err != nil || len(results) != 1 || results[0].Count != 1 {
		t.Fatal(results, err)
	}
	before, _ := st.GetResource(ctx, results[0].Resources[0].ID)
	denied.Store(true)
	time.Sleep(2 * time.Millisecond)
	_, err = service.Run(ctx)
	if err == nil {
		t.Fatal("forbidden API accepted as empty success")
	}
	after, _ := st.GetResource(ctx, before.ID)
	if after.LifecycleState != resource.LifecycleActive || !after.LastSeenAt.Equal(before.LastSeenAt) || after.Metadata["discovery_absence"] != nil {
		t.Fatal("denied access altered registry lifecycle")
	}
	runs, err := st.ListDiscoveryRuns(ctx, store.DiscoveryRunQuery{})
	if err != nil || len(runs) != 2 || runs[0].ErrorCode != "authorization" || runs[0].State != "failed" {
		t.Fatal(runs, err)
	}
	raw, _ := json.Marshal(runs)
	for _, secret := range []string{"fixture-private-api-denial", "fixture-private-auth-token", server.URL} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("sensitive provider data persisted")
		}
	}
	denied.Store(false)
	time.Sleep(2 * time.Millisecond)
	if _, err = service.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if provider.Health(ctx).Status != "healthy" {
		t.Fatal("complete recovery scan did not restore health")
	}
}
