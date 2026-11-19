package discovery_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/consize-oss/consize/internal/api"
	"github.com/consize-oss/consize/internal/bootstrap"
	"github.com/consize-oss/consize/internal/config"
	"github.com/consize-oss/consize/internal/policy"
	"github.com/consize-oss/consize/internal/store"
	"github.com/consize-oss/consize/pkg/plugin"
	k8splugin "github.com/consize-oss/consize/pkg/plugins/kubernetes"
	"github.com/consize-oss/consize/pkg/resource"
	appsv1 "k8s.io/api/apps/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	quantity "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

type qualificationReport struct {
	Version         string   `json:"version"`
	Passed          bool     `json:"passed"`
	Steps           []string `json:"steps"`
	CleanupVerified bool     `json:"cleanup_verified"`
}

// This test provisions fixture objects only in newly created random namespaces.
// It never selects the current kubectl context or mutates non-fixture resources.
func TestIsolatedDiscoveryRegistryQualification(t *testing.T) {
	kubeconfig := os.Getenv("CONSIZE_DISCOVERY_TEST_KUBECONFIG")
	if kubeconfig == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("CI requires the isolated discovery cluster")
		}
		t.Skip("run scripts/test/discovery.sh to create an isolated qualification cluster")
	}
	contextName := os.Getenv("CONSIZE_DISCOVERY_TEST_CONTEXT")
	if os.Getenv("CONSIZE_DISCOVERY_TEST_FIXTURES") != "1" || !filepath.IsAbs(kubeconfig) || !strings.HasPrefix(contextName, "kind-consize-discovery-") {
		t.Fatal("refusing unapproved qualification target")
	}
	adminConfig, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfig}, &clientcmd.ConfigOverrides{CurrentContext: contextName}).ClientConfig()
	if err != nil {
		t.Fatal("load explicit qualification context")
	}
	endpoint, err := url.Parse(adminConfig.Host)
	if err != nil || endpoint.Scheme != "https" || net.ParseIP(endpoint.Hostname()) == nil || !net.ParseIP(endpoint.Hostname()).IsLoopback() {
		t.Fatal("refusing non-loopback qualification API")
	}
	adminConfig.Timeout = 5 * time.Second
	admin, err := kubernetes.NewForConfig(adminConfig)
	if err != nil {
		t.Fatal("create qualification admin client")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	report := qualificationReport{Version: "discovery-qualification-v1", Steps: []string{}}
	t.Cleanup(func() {
		report.Passed = !t.Failed() && report.CleanupVerified
		if path := os.Getenv("CONSIZE_DISCOVERY_QUALIFICATION_REPORT"); path != "" {
			raw, _ := json.MarshalIndent(report, "", "  ")
			if err := os.WriteFile(path, append(raw, '\n'), 0600); err != nil {
				t.Error("write qualification summary")
			}
		}
	})
	tokenBytes := make([]byte, 24)
	if _, err = rand.Read(tokenBytes); err != nil {
		t.Fatal(err)
	}
	suffix := hex.EncodeToString(tokenBytes[:6])
	alpha, beta := "consize-qualification-a-"+suffix, "consize-qualification-b-"+suffix
	owned := map[string]types.UID{}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 35*time.Second)
		defer stop()
		clean := true
		for ns, uid := range owned {
			e := admin.CoreV1().Namespaces().Delete(cleanup, ns, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}})
			if e != nil && !apierrors.IsNotFound(e) {
				t.Error("delete qualification namespace")
				clean = false
				continue
			}
			for {
				_, e = admin.CoreV1().Namespaces().Get(cleanup, ns, metav1.GetOptions{})
				if apierrors.IsNotFound(e) {
					break
				}
				if cleanup.Err() != nil {
					t.Error("qualification namespace cleanup not confirmed")
					clean = false
					break
				}
				time.Sleep(100 * time.Millisecond)
			}
		}
		report.CleanupVerified = clean
	})
	for _, ns := range []string{alpha, beta} {
		created, e := admin.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns, Labels: map[string]string{"consize.io/qualification": "fixture"}}}, metav1.CreateOptions{})
		if e != nil {
			t.Fatal("create isolated qualification namespace")
		}
		owned[ns] = created.UID
	}
	createWorkload := func(ns, name string) *appsv1.Deployment {
		t.Helper()
		zero := int32(0)
		d, e := admin.AppsV1().Deployments(ns).Create(ctx, &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{"consize.io/qualification": "selected"}}, Spec: appsv1.DeploymentSpec{Replicas: &zero, Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": name}}, Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": name}}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "example.invalid/consize-qualification:fixture", Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: quantity.MustParse("250m"), corev1.ResourceMemory: quantity.MustParse("128Mi")}}}, {Name: "sidecar", Image: "example.invalid/consize-qualification:fixture", Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: quantity.MustParse("25m"), corev1.ResourceMemory: quantity.MustParse("32Mi")}}}}}}}}, metav1.CreateOptions{})
		if e != nil {
			t.Fatal("create zero-replica qualification Deployment")
		}
		return d
	}
	first := createWorkload(alpha, "app")
	createWorkload(beta, "app")
	excluded := createWorkload(alpha, "excluded")
	excluded.Labels["consize.io/qualification"] = "excluded"
	if _, err = admin.AppsV1().Deployments(alpha).Update(ctx, excluded, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	sa, err := admin.CoreV1().ServiceAccounts(alpha).Create(ctx, &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "qualification-reader"}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	bindings := map[string]*rbacv1.RoleBinding{}
	for _, ns := range []string{alpha, beta} {
		_, err = admin.RbacV1().Roles(ns).Create(ctx, &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: "qualification-reader"}, Rules: []rbacv1.PolicyRule{{APIGroups: []string{"apps"}, Resources: []string{"deployments"}, Verbs: []string{"list"}}}}, metav1.CreateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		binding, e := admin.RbacV1().RoleBindings(ns).Create(ctx, &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "qualification-reader"}, RoleRef: rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: "qualification-reader"}, Subjects: []rbacv1.Subject{{Kind: "ServiceAccount", Name: sa.Name, Namespace: alpha}}}, metav1.CreateOptions{})
		if e != nil {
			t.Fatal(e)
		}
		bindings[ns] = binding
	}
	expires := int64(600)
	credential, err := admin.CoreV1().ServiceAccounts(alpha).CreateToken(ctx, sa.Name, &authenticationv1.TokenRequest{Spec: authenticationv1.TokenRequestSpec{ExpirationSeconds: &expires}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal("issue isolated reader credential")
	}
	dir := t.TempDir()
	readerPath := filepath.Join(dir, "reader-kubeconfig")
	anonymous := rest.AnonymousClientConfig(adminConfig)
	readerConfig := clientcmdapi.Config{Clusters: map[string]*clientcmdapi.Cluster{"qualification": {Server: anonymous.Host, CertificateAuthorityData: anonymous.CAData, CertificateAuthority: anonymous.CAFile, TLSServerName: anonymous.ServerName}}, AuthInfos: map[string]*clientcmdapi.AuthInfo{"reader": {Token: credential.Status.Token}}, Contexts: map[string]*clientcmdapi.Context{"qualification": {Cluster: "qualification", AuthInfo: "reader"}}, CurrentContext: "qualification"}
	if err = clientcmd.WriteToFile(readerConfig, readerPath); err != nil {
		t.Fatal("write private reader config")
	}
	t.Setenv("CON13_QUALIFICATION_ADMIN_TOKEN", hex.EncodeToString(tokenBytes))
	apiToken := hex.EncodeToString(tokenBytes)
	bootstrapBody := map[string]any{"state_path": filepath.Join(dir, "state.json"), "kubernetes": map[string]any{"enabled": true, "kubeconfig": readerPath, "context": "qualification", "cluster_id": "qualification-cluster-" + suffix, "location": "local", "discovery": map[string]any{"namespaces": []string{alpha, beta}, "label_selector": "consize.io/qualification=selected", "page_size": 1}}, "discovery": map[string]any{"enabled": true, "interval": "1s", "poll_interval": "1s", "timeout": "10s", "stale_after": "0s", "tombstone_after": "3s"}, "auth": map[string]any{"enabled": true, "tokens": []any{map[string]any{"subject": "qualification-admin", "role": "admin", "token_env": "CON13_QUALIFICATION_ADMIN_TOKEN"}}}}
	configPath := filepath.Join(dir, "consize.json")
	raw, _ := json.Marshal(bootstrapBody)
	if err = os.WriteFile(configPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadBootstrap(configPath)
	if err != nil {
		t.Fatal("load explicit backend configuration")
	}
	st, err := store.OpenDurable(cfg.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	var backend *httptest.Server
	var stopWorker context.CancelFunc
	var workerDone chan error
	var manager *plugin.Manager
	start := func() {
		t.Helper()
		manager = plugin.NewManager()
		if e := bootstrap.RegisterConfiguredPlugins(ctx, manager, cfg); e != nil {
			t.Fatal("register qualification plugins")
		}
		server, e := api.NewServer(st, manager, policy.NewEngine(), cfg)
		if e != nil {
			t.Fatal(e)
		}
		backend = httptest.NewServer(server.Handler())
		workerCtx, stop := context.WithCancel(ctx)
		stopWorker = stop
		workerDone = make(chan error, 1)
		go func() { workerDone <- server.RunWorker(workerCtx) }()
	}
	stop := func() {
		t.Helper()
		if stopWorker != nil {
			stopWorker()
			select {
			case e := <-workerDone:
				if e != nil {
					t.Error("qualification worker failed")
				}
			case <-time.After(15 * time.Second):
				t.Error("qualification worker did not stop")
			}
			stopWorker = nil
		}
		if backend != nil {
			backend.Close()
			backend = nil
		}
	}
	t.Cleanup(func() { stop(); st.Close() })
	start()
	wait := func(label string, predicate func() bool) {
		t.Helper()
		for {
			if predicate() {
				report.Steps = append(report.Steps, label)
				return
			}
			select {
			case <-ctx.Done():
				t.Fatalf("qualification timed out: %s", label)
			case <-time.After(100 * time.Millisecond):
			}
		}
	}
	rows := func() []resource.Resource {
		r, e := st.ListResources(ctx)
		if e != nil {
			t.Fatal("read qualification registry")
		}
		return r
	}
	var alphaID, betaID string
	wait("scheduled discovery registers distinct same-name Deployments", func() bool {
		r := rows()
		if len(r) != 2 {
			return false
		}
		for _, item := range r {
			if item.ProviderResourceID == alpha+"/app" {
				alphaID = item.ID
			}
			if item.ProviderResourceID == beta+"/app" {
				betaID = item.ID
			}
		}
		return alphaID != "" && betaID != "" && alphaID != betaID
	})
	request := func(method, path, body string) (int, []byte) {
		t.Helper()
		r, e := http.NewRequestWithContext(ctx, method, backend.URL+path, strings.NewReader(body))
		if e != nil {
			t.Fatal(e)
		}
		r.Header.Set("Authorization", "Bearer "+apiToken)
		r.Header.Set("Content-Type", "application/json")
		response, e := backend.Client().Do(r)
		if e != nil {
			t.Fatal("call qualification backend")
		}
		defer response.Body.Close()
		bodyBytes, e := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		if e != nil {
			t.Fatal(e)
		}
		return response.StatusCode, bodyBytes
	}
	wait("manual rescan returns inspectable durable operation", func() bool {
		code, body := request("POST", "/api/discovery/runs", `{"plugin_id":"kubernetes-action"}`)
		if code == 409 {
			return false
		}
		if code != 202 {
			t.Fatalf("manual rescan status=%d", code)
		}
		var run store.DiscoveryRun
		if json.Unmarshal(body, &run) != nil || run.ID == "" {
			t.Fatal("invalid durable operation")
		}
		code, _ = request("GET", "/api/discovery/runs?id="+run.ID, "")
		return code == 200
	})
	item, err := admin.AppsV1().Deployments(alpha).Get(ctx, "app", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	item.Labels["qualification-change"] = "updated"
	item.Spec.Template.Spec.Containers[0].Resources.Requests[corev1.ResourceMemory] = quantity.MustParse("256Mi")
	if _, err = admin.AppsV1().Deployments(alpha).Update(ctx, item, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	wait("metadata and sizing refresh preserve registry identity", func() bool {
		r, e := st.GetResource(ctx, alphaID)
		return e == nil && r.Labels["qualification-change"] == "updated" && fmt.Sprint(r.CurrentState["memory_request_bytes"]) == "301989888" && len(rows()) == 2
	})
	binding := bindings[alpha]
	uid := binding.UID
	if err = admin.RbacV1().RoleBindings(alpha).Delete(ctx, binding.Name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}}); err != nil {
		t.Fatal(err)
	}
	wait("authorization failure is durable and preserves lifecycle", func() bool {
		runs, e := st.ListDiscoveryRuns(ctx, store.DiscoveryRunQuery{Limit: 10})
		if e != nil || len(runs) == 0 || runs[0].ErrorCode != "authorization" {
			return false
		}
		for _, r := range rows() {
			if r.LifecycleState != resource.LifecycleActive || r.Metadata["discovery_absence"] != nil {
				t.Fatal("access failure reconciled absence")
			}
		}
		return true
	})
	if _, err = admin.RbacV1().RoleBindings(alpha).Create(ctx, &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: binding.Name}, RoleRef: binding.RoleRef, Subjects: binding.Subjects}, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	wait("restored access requires a complete healthy recovery scan", func() bool {
		health, e := manager.Health(ctx, k8splugin.ID)
		return e == nil && health.Status == "healthy"
	})
	stop()
	existing, err := st.ListDiscoveryRuns(ctx, store.DiscoveryRunQuery{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	var pending store.DiscoveryRun
	for _, run := range existing {
		if !store.DiscoveryTerminal(run.State) {
			pending = run
			break
		}
	}
	if pending.ID == "" {
		p := manager.DiscoveryPlugins()[0].(plugin.ScopedDiscoveryPlugin)
		pending, err = st.EnqueueDiscovery(ctx, store.DiscoveryRun{PluginID: p.ID(), Scope: p.DiscoveryScope(), RequestedAt: time.Now().UTC(), Actor: "qualification-admin", Trigger: "manual", MaxAttempts: 4, StaleAfter: "0s", TombstoneAfter: "3s"})
		if err != nil {
			t.Fatal(err)
		}
		pending, err = st.ClaimDiscovery(ctx, pending.ID, "qualification-interrupted", time.Second)
		if err != nil {
			t.Fatal(err)
		}
	}
	if err = st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = store.OpenDurable(cfg.StatePath)
	if err != nil {
		t.Fatal("reopen durable qualification state")
	}
	if len(rows()) != 2 {
		t.Fatal("restart lost registry records")
	}
	start()
	wait("restart resumes captured run without duplicate resources", func() bool {
		done, e := st.GetDiscoveryRun(ctx, pending.ID)
		return e == nil && done.State == "succeeded" && len(rows()) == 2
	})
	uid = first.UID
	if err = admin.AppsV1().Deployments(alpha).Delete(ctx, "app", metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}}); err != nil {
		t.Fatal(err)
	}
	wait("complete scoped absence records the stale transition", func() bool {
		r, e := st.GetResource(ctx, alphaID)
		if e != nil {
			return false
		}
		if r.LifecycleState == resource.LifecycleStale {
			return true
		}
		runs, e := st.ListDiscoveryRuns(ctx, store.DiscoveryRunQuery{Limit: 100})
		if e != nil {
			return false
		}
		for _, run := range runs {
			for _, id := range run.Reconciliation.Stale {
				if id == alphaID {
					return true
				}
			}
		}
		return false
	})
	wait("confirmed scoped deletion produces a registry tombstone", func() bool {
		r, e := st.GetResource(ctx, alphaID)
		other, oe := st.GetResource(ctx, betaID)
		if oe != nil || other.LifecycleState != resource.LifecycleActive {
			t.Fatal("one namespace affected another")
		}
		return e == nil && r.LifecycleState == resource.LifecycleDeleted
	})
	replacement := createWorkload(alpha, "app")
	if replacement.UID == first.UID {
		t.Fatal("fixture recreation did not change UID")
	}
	wait("same-name UID replacement is rejected without rewriting history", func() bool {
		runs, e := st.ListDiscoveryRuns(ctx, store.DiscoveryRunQuery{Limit: 10})
		if e != nil || len(runs) == 0 || runs[0].ErrorCode != "registry_conflict" {
			return false
		}
		r, e := st.GetResource(ctx, alphaID)
		if e != nil || r.Metadata["uid"] != string(first.UID) || r.LifecycleState != resource.LifecycleDeleted {
			t.Fatal("replacement overwrote protected identity")
		}
		return true
	})
	// Invalid context and missing credentials are setup failures, not empty scans.
	_, err = k8splugin.New(k8splugin.Config{Kubeconfig: readerPath, Context: "missing-qualification-context", ClusterID: cfg.Kubernetes.ClusterID, Location: "local"})
	if err == nil {
		t.Fatal("invalid context accepted")
	}
	badReader := readerConfig.DeepCopy()
	badReader.AuthInfos["reader"].Token = "fixture-deliberately-invalid-token"
	badPath := filepath.Join(dir, "invalid-reader-kubeconfig")
	if err = clientcmd.WriteToFile(*badReader, badPath); err != nil {
		t.Fatal(err)
	}
	badProvider, e := k8splugin.New(k8splugin.Config{Kubeconfig: badPath, ClusterID: cfg.Kubernetes.ClusterID, Location: "local", Discovery: k8splugin.DiscoveryConfig{Namespaces: []string{alpha}}})
	if e != nil {
		t.Fatal(e)
	}
	badSnapshot, e := badProvider.DiscoverSnapshot(ctx)
	var access *k8splugin.ScanError
	if !errors.As(e, &access) || access.Code != "unauthorized" || badSnapshot.Scan.Complete || badSnapshot.Observations != nil {
		t.Fatal("invalid credentials became successful discovery")
	}
	report.Steps = append(report.Steps, "unauthorized credential produces no authoritative observations")
	report.Steps = append(report.Steps, "invalid context fails without fabricated resources")
	stop()
	t.Log("real discovery qualification passed; cleanup verification runs next")
}
