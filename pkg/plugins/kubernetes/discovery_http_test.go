package kubernetes

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

func TestFakeClientNamespaceAndLabelSelection(t *testing.T) {
	chosen := deploymentFixture("alpha", "chosen", "one")
	excluded := deploymentFixture("alpha", "excluded", "two")
	excluded.Labels["app"] = "excluded"
	outside := deploymentFixture("beta", "chosen", "three")
	client := fake.NewSimpleClientset([]runtime.Object{&chosen, &excluded, &outside}...)
	p, err := NewWithPatcherAndConfig(NewK8sPatcherFromClient(client), Config{ClusterID: "fixture-cluster", Location: "local", Discovery: DiscoveryConfig{Namespaces: []string{"alpha"}, LabelSelector: "app=chosen"}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := p.DiscoverSnapshot(context.Background())
	if err != nil || !snapshot.Scan.Complete || snapshot.Scan.Count != 1 || snapshot.Observations[0].Resource.ProviderResourceID != "alpha/chosen" {
		t.Fatalf("selection: %+v %v", snapshot.Scan, err)
	}
}
func TestProductionDiscoveryClientHonorsExplicitContextAndPagination(t *testing.T) {
	calls := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-client-token" {
			t.Error("wrong discovery credentials")
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/version" {
			io.WriteString(w, `{"gitVersion":"v1.37.0"}`)
			return
		}
		if r.URL.Path != "/apis/apps/v1/namespaces/alpha/deployments" || r.URL.Query().Get("labelSelector") != "app=chosen" || r.URL.Query().Get("limit") != "1" {
			t.Errorf("wrong scoped request: %s", r.URL.Path)
			http.Error(w, "unexpected", 400)
			return
		}
		calls++
		result := page(deploymentFixture("alpha", "a", "uid-a"))
		result.TypeMeta = metav1.TypeMeta{Kind: "DeploymentList", APIVersion: "apps/v1"}
		if r.URL.Query().Get("continue") == "" {
			result.Items[0] = deploymentFixture("alpha", "b", "uid-b")
			result.Continue = "next"
		}
		json.NewEncoder(w).Encode(result)
	}))
	defer server.Close()
	config := clientcmdapi.Config{Clusters: map[string]*clientcmdapi.Cluster{"selected": {Server: server.URL, CertificateAuthorityData: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})}, "unused": {Server: "https://127.0.0.1:1"}}, AuthInfos: map[string]*clientcmdapi.AuthInfo{"fixture-user": {Token: "fixture-client-token"}}, Contexts: map[string]*clientcmdapi.Context{"selected-context": {Cluster: "selected", AuthInfo: "fixture-user"}, "unused-default": {Cluster: "unused", AuthInfo: "fixture-user"}}, CurrentContext: "unused-default"}
	path := filepath.Join(t.TempDir(), "fixture-kubeconfig")
	if err := clientcmd.WriteToFile(config, path); err != nil {
		t.Fatal(err)
	}
	p, err := New(Config{Kubeconfig: path, Context: "selected-context", ClusterID: "fixture-cluster", Location: "local", Discovery: DiscoveryConfig{Namespaces: []string{"alpha"}, LabelSelector: "app=chosen", PageSize: 1}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := p.DiscoverSnapshot(context.Background())
	if err != nil || snapshot.Scan.Count != 2 || snapshot.Scan.Pages != 2 || calls != 2 {
		t.Fatalf("real HTTP pagination: %+v %v", snapshot.Scan, err)
	}
	if health := p.Health(context.Background()); health.Status != "healthy" || health.Details == nil {
		t.Fatal("discovery health missing")
	}
	if _, err := New(Config{Kubeconfig: path, Context: "missing-context", ClusterID: "fixture-cluster", Location: "local"}); err == nil || strings.Contains(err.Error(), "fixture-client-token") {
		t.Fatal("invalid context accepted or credentials leaked")
	}
	if _, err := NewK8sPatcherForContext("", "selected-context"); err == nil {
		t.Fatal("context accepted without explicit kubeconfig")
	}
	// Keep credential material private even in test configuration files.
	if info, err := os.Stat(path); err != nil || info.Mode().Perm()&0077 != 0 {
		t.Fatal("fixture kubeconfig permissions")
	}
}

type discoveryRoundTrip func(*http.Request) (*http.Response, error)

func (f discoveryRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestDiscoveryResponseBoundDoesNotChangeActionTransport(t *testing.T) {
	for _, size := range []int{deploymentPageBytes, deploymentPageBytes + 1} {
		body := io.NopCloser(strings.NewReader(strings.Repeat("x", size)))
		transport := deploymentListTransport{next: discoveryRoundTrip(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: body, ContentLength: -1}, nil
		})}
		request, _ := http.NewRequest(http.MethodGet, "https://fixture.invalid/apis/apps/v1/namespaces/alpha/deployments", nil)
		response, err := transport.RoundTrip(request)
		if err != nil {
			t.Fatal(err)
		}
		_, err = io.ReadAll(response.Body)
		response.Body.Close()
		if size > deploymentPageBytes {
			requireScanCode(t, err, "limit_exceeded")
		} else if err != nil {
			t.Fatal(err)
		}
	}
	for _, target := range []struct{ method, path string }{{http.MethodGet, "/apis/apps/v1/namespaces/alpha/deployments/api"}, {http.MethodPatch, "/apis/apps/v1/namespaces/alpha/deployments/api"}} {
		body := io.NopCloser(strings.NewReader("fixture"))
		transport := deploymentListTransport{next: discoveryRoundTrip(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: body, ContentLength: deploymentPageBytes + 1}, nil
		})}
		request, _ := http.NewRequest(target.method, "https://fixture.invalid"+target.path, nil)
		response, err := transport.RoundTrip(request)
		if err != nil || response.Body != body {
			t.Fatal("action transport was altered")
		}
		response.Body.Close()
	}
}
func TestDiscoveryHealthRespectsParentCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	patcher := NewK8sPatcherFromClient(fake.NewSimpleClientset())
	if err := patcher.Health(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("health lost cancellation")
	}
}
