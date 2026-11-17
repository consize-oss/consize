package kubernetes

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

func accessHTTPPlugin(t *testing.T, server *httptest.Server) *Plugin {
	t.Helper()
	config := clientcmdapi.Config{Clusters: map[string]*clientcmdapi.Cluster{"fixture": {Server: server.URL, CertificateAuthorityData: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})}}, AuthInfos: map[string]*clientcmdapi.AuthInfo{"fixture-user": {Token: "fixture-private-client-token"}}, Contexts: map[string]*clientcmdapi.Context{"fixture": {Cluster: "fixture", AuthInfo: "fixture-user"}}, CurrentContext: "fixture"}
	path := filepath.Join(t.TempDir(), "fixture-kubeconfig")
	if err := clientcmd.WriteToFile(config, path); err != nil {
		t.Fatal(err)
	}
	p, err := New(Config{Kubeconfig: path, Context: "fixture", ClusterID: "fixture-cluster", Location: "local", Discovery: DiscoveryConfig{Namespaces: []string{"alpha"}}})
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func TestDiscoveryAccessHTTPFailureMatrixAndRedaction(t *testing.T) {
	for _, tc := range []struct {
		status int
		code   string
		retry  bool
	}{{401, "unauthorized", false}, {403, "forbidden", false}, {429, "rate_limited", true}, {500, "server", true}, {503, "server", true}, {400, "request", false}, {410, "expired_continue", false}} {
		t.Run(fmt.Sprint(tc.status), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Warning", `299 - "fixture-private-warning"`)
				w.Header().Set("Retry-After", "60")
				w.WriteHeader(tc.status)
				fmt.Fprint(w, `{"kind":"Status","apiVersion":"v1","message":"fixture-private-response-body","status":"Failure"}`)
			}))
			defer server.Close()
			p := accessHTTPPlugin(t, server)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			batch, err := p.DiscoverSnapshot(ctx)
			var failure *ScanError
			if !errors.As(err, &failure) || failure.Code != tc.code || failure.Retryable != tc.retry || batch.Scan.Complete || batch.Observations != nil {
				t.Fatalf("code=%+v batch=%+v err=%v", failure, batch.Scan, err)
			}
			if calls.Load() != 1 {
				t.Fatalf("hidden client retries=%d", calls.Load())
			}
			if failure.Verb != "list" || failure.Resource != "deployments" || failure.Namespace != "alpha" {
				t.Fatalf("missing safe required permission: %+v", failure)
			}
			raw, _ := json.Marshal(p.DiscoveryDiagnostics())
			all := string(raw) + err.Error()
			for _, secret := range []string{"fixture-private-response-body", "fixture-private-client-token", "fixture-private-warning", server.URL} {
				if strings.Contains(all, secret) {
					t.Fatal("sensitive access detail leaked")
				}
			}
			if tc.status == 429 && failure.RetryAfterSeconds != 60 {
				t.Fatal("retry hint lost", failure)
			}
		})
	}
}
func TestDiscoveryAccessLossAndRecoveryRequiresNewScan(t *testing.T) {
	var denied atomic.Bool
	var versionCalls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/version" {
			versionCalls.Add(1)
			http.Error(w, "fixture-private-version", 403)
			return
		}
		if denied.Load() {
			http.Error(w, "fixture-private-denial", 403)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		result := page(deploymentFixture("alpha", "app", "fixture-uid"))
		result.TypeMeta = metav1.TypeMeta{Kind: "DeploymentList", APIVersion: "apps/v1"}
		json.NewEncoder(w).Encode(result)
	}))
	defer server.Close()
	p := accessHTTPPlugin(t, server)
	ctx := context.Background()
	if health := p.Health(ctx); health.Status != "degraded" {
		t.Fatal("permission probe substituted for complete scan", health)
	}
	if _, err := p.DiscoverSnapshot(ctx); err != nil {
		t.Fatal(err)
	}
	if health := p.Health(ctx); health.Status != "healthy" {
		t.Fatal(health)
	}
	denied.Store(true)
	if health := p.Health(ctx); health.Status != "degraded" {
		t.Fatal("access loss not detected", health)
	}
	diag := p.DiscoveryDiagnostics()
	if diag.Access.Code != "forbidden" || diag.Access.APIReachable == nil || !*diag.Access.APIReachable || !diag.Access.NeedsSuccessfulScan {
		t.Fatal(diag.Access)
	}
	denied.Store(false)
	if health := p.Health(ctx); health.Status != "degraded" {
		t.Fatal("restored access bypassed recovery scan", health)
	}
	if _, err := p.DiscoverSnapshot(ctx); err != nil {
		t.Fatal(err)
	}
	if health := p.Health(ctx); health.Status != "healthy" {
		t.Fatal(health)
	}
	if versionCalls.Load() != 0 {
		t.Fatal("health incorrectly required /version permission")
	}
	// Access diagnostics returned to callers are independent copies.
	copy := p.DiscoveryDiagnostics()
	*copy.Access.APIReachable = false
	if !*p.DiscoveryDiagnostics().Access.APIReachable {
		t.Fatal("mutable diagnostics")
	}
}
func TestAccessConfigurationTLSNetworkAndCancellation(t *testing.T) {
	for _, cfg := range []*rest.Config{{Host: "http://example.invalid"}, {Host: "https://user:fixture-private@example.invalid"}, {Host: "https://example.invalid?token=fixture-private"}, {Host: "https://example.invalid", TLSClientConfig: rest.TLSClientConfig{Insecure: true}}} {
		if validateDiscoveryRESTConfig(cfg) == nil {
			t.Fatal("unsafe configuration accepted")
		}
	}
	bad := filepath.Join(t.TempDir(), "private-kubeconfig")
	os.WriteFile(bad, []byte("fixture-private-invalid-config"), 0600)
	_, err := NewK8sPatcherForContext(bad, "")
	requireScanCode(t, err, "configuration")
	if strings.Contains(err.Error(), bad) || strings.Contains(err.Error(), "fixture-private-invalid-config") {
		t.Fatal("configuration leaked")
	}
	for _, tc := range []struct {
		err  error
		code string
	}{{&net.OpError{Op: "dial", Err: errors.New("fixture-private-endpoint")}, "network"}, {x509.UnknownAuthorityError{}, "tls"}, {&tls.CertificateVerificationError{Err: x509.HostnameError{}}, "tls"}, {context.Canceled, "canceled"}, {context.DeadlineExceeded, "timeout"}, {io.ErrUnexpectedEOF, "network"}, {errors.New("fixture-private-unknown"), "provider_failure"}} {
		classified := safeScanError(tc.err)
		if classified.Code != tc.code || strings.Contains(classified.Error(), "fixture-private") {
			t.Fatal(classified)
		}
		if errors.Is(tc.err, context.Canceled) && !errors.Is(classified, context.Canceled) {
			t.Fatal("cancellation lost")
		}
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	p := accessHTTPPlugin(t, server)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err = p.DiscoverSnapshot(ctx)
	requireScanCode(t, err, "timeout")
}
func TestDiscoveryRedirectNeverForwardsCredentials(t *testing.T) {
	var targetCalls atomic.Int32
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { targetCalls.Add(1) }))
	defer target.Close()
	source := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL+"/deployments", 302) }))
	defer source.Close()
	p := accessHTTPPlugin(t, source)
	_, err := p.DiscoverSnapshot(context.Background())
	requireScanCode(t, err, "redirect_blocked")
	if targetCalls.Load() != 0 {
		t.Fatal("credential-bearing redirect followed")
	}
}

func TestDedicatedDiscoveryCredentialDoesNotReachActionRequests(t *testing.T) {
	var readerCalls, actionCalls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/deployments") {
			readerCalls.Add(1)
			if r.Header.Get("Authorization") != "Bearer fixture-reader-credential" {
				t.Error("discovery used action credential")
			}
			result := page(deploymentFixture("alpha", "app", "fixture-uid"))
			result.TypeMeta = metav1.TypeMeta{Kind: "DeploymentList", APIVersion: "apps/v1"}
			json.NewEncoder(w).Encode(result)
			return
		}
		actionCalls.Add(1)
		if r.Header.Get("Authorization") != "Bearer fixture-action-credential" {
			t.Error("action used discovery credential")
		}
		http.Error(w, "fixture read-only action probe", 403)
	}))
	defer server.Close()
	write := func(token string) string {
		cfg := clientcmdapi.Config{Clusters: map[string]*clientcmdapi.Cluster{"fixture": {Server: server.URL, CertificateAuthorityData: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})}}, AuthInfos: map[string]*clientcmdapi.AuthInfo{"fixture": {Token: token}}, Contexts: map[string]*clientcmdapi.Context{"fixture": {Cluster: "fixture", AuthInfo: "fixture"}}, CurrentContext: "fixture"}
		path := filepath.Join(t.TempDir(), "fixture-kubeconfig")
		if err := clientcmd.WriteToFile(cfg, path); err != nil {
			t.Fatal(err)
		}
		return path
	}
	p, err := New(Config{Kubeconfig: write("fixture-action-credential"), DiscoveryAuth: &DiscoveryAuthConfig{Kubeconfig: write("fixture-reader-credential")}, ClusterID: "fixture-cluster", Location: "local", Discovery: DiscoveryConfig{Namespaces: []string{"alpha"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.DiscoverSnapshot(context.Background()); err != nil {
		t.Fatal(err)
	}
	patcher := p.patcher.(*K8sPatcher)
	patcher.client.AppsV1().Deployments("alpha").Get(context.Background(), "app", metav1.GetOptions{})
	if readerCalls.Load() != 1 || actionCalls.Load() != 1 {
		t.Fatal("credential separation was not exercised")
	}
	other := httptest.NewTLSServer(http.NotFoundHandler())
	defer other.Close()
	cfg, err := clientcmd.LoadFromFile(write("fixture-reader-credential"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Clusters["fixture"].Server = other.URL
	otherPath := filepath.Join(t.TempDir(), "fixture-other-cluster")
	clientcmd.WriteToFile(*cfg, otherPath)
	_, err = New(Config{Kubeconfig: write("fixture-action-credential"), DiscoveryAuth: &DiscoveryAuthConfig{Kubeconfig: otherPath}, ClusterID: "fixture-cluster", Location: "local"})
	requireScanCode(t, err, "configuration")
}
func TestProductionClientRejectsUntrustedTLSAndClassifiesConnectionFailure(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "fixture") }))
	cfg := clientcmdapi.Config{Clusters: map[string]*clientcmdapi.Cluster{"fixture": {Server: server.URL}}, AuthInfos: map[string]*clientcmdapi.AuthInfo{"fixture": {Token: "fixture-private-token"}}, Contexts: map[string]*clientcmdapi.Context{"fixture": {Cluster: "fixture", AuthInfo: "fixture"}}, CurrentContext: "fixture"}
	path := filepath.Join(t.TempDir(), "fixture-kubeconfig")
	clientcmd.WriteToFile(cfg, path)
	p, err := New(Config{Kubeconfig: path, ClusterID: "fixture-cluster", Location: "local", Discovery: DiscoveryConfig{Namespaces: []string{"alpha"}}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.DiscoverSnapshot(context.Background())
	requireScanCode(t, err, "tls")
	server.Close()
	_, err = p.DiscoverSnapshot(context.Background())
	requireScanCode(t, err, "network")
}

type accessWarningSink struct{ calls atomic.Int32 }

func (s *accessWarningSink) HandleWarningHeaderWithContext(context.Context, int, string, string) {
	s.calls.Add(1)
}
func TestDiscoveryNeverForwardsUntrustedWarningTextToClientLogger(t *testing.T) {
	sink := &accessWarningSink{}
	rest.SetDefaultWarningHandlerWithContext(sink)
	t.Cleanup(func() { rest.SetDefaultWarningHandlerWithContext(rest.WarningLogger{}) })
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Warning", `299 - "fixture-private-warning-text"`)
		w.Header().Set("Content-Type", "application/json")
		result := page()
		result.TypeMeta = metav1.TypeMeta{Kind: "DeploymentList", APIVersion: "apps/v1"}
		json.NewEncoder(w).Encode(result)
	}))
	defer server.Close()
	p := accessHTTPPlugin(t, server)
	if _, err := p.DiscoverSnapshot(context.Background()); err != nil {
		t.Fatal(err)
	}
	if sink.calls.Load() != 0 {
		t.Fatal("untrusted warning reached client logging")
	}
}
