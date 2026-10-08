package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/consize-oss/consize/internal/auth"
	"github.com/consize-oss/consize/internal/bootstrap"
	"github.com/consize-oss/consize/internal/policy"
	"github.com/consize-oss/consize/internal/store"
	"github.com/consize-oss/consize/pkg/plugin"
	k8splugin "github.com/consize-oss/consize/pkg/plugins/kubernetes"
	"github.com/consize-oss/consize/pkg/resource"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	quantity "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestHealthReportsDurableStorageStatus(t *testing.T) {
	server, _, _ := apiFixture(t)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("health status = %d, body = %s", w.Code, w.Body.String())
	}
	var response struct {
		Storage store.StorageDiagnostics `json:"storage"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Storage.Durable || !response.Storage.Healthy {
		t.Fatalf("storage diagnostics = %+v", response.Storage)
	}
	if response.Storage.CurrentVersion == 0 || response.Storage.LoadedVersion != response.Storage.CurrentVersion {
		t.Fatalf("storage versions = loaded %d, current %d", response.Storage.LoadedVersion, response.Storage.CurrentVersion)
	}
	if response.Storage.MigrationStatus != store.MigrationCurrent {
		t.Fatalf("migration status = %q", response.Storage.MigrationStatus)
	}
	if len(response.Storage.Indexes) == 0 {
		t.Fatal("storage diagnostics omitted required indexes")
	}
}

type apiMetrics struct{}

func (apiMetrics) ID() string { return "metrics" }
func (apiMetrics) Manifest() plugin.Manifest {
	return plugin.Manifest{ID: "metrics", Category: plugin.CategoryMetrics, SupportedResourceTypes: []string{resource.TypeKubernetesDeployment}, Capabilities: []string{plugin.CapabilityMetricsRead}}
}
func (apiMetrics) Health(context.Context) plugin.Health { return plugin.Health{Status: "healthy"} }
func (m apiMetrics) ReadMetrics(ctx context.Context, res resource.Resource) (plugin.MetricsSnapshot, error) {
	now := time.Now().UTC()
	return m.ReadMetricsBetween(ctx, res, now.Add(-30*time.Minute), now)
}
func (apiMetrics) ReadMetricsBetween(_ context.Context, res resource.Resource, start, end time.Time) (plugin.MetricsSnapshot, error) {
	coverage := map[string]plugin.MetricCoverage{}
	for _, key := range []string{"memory_working_set_bytes", "cpu_cores", "restarts_30m"} {
		coverage[key] = plugin.MetricCoverage{FirstSample: start, LastSample: end, ExpectedPoints: 60, ActualPoints: 60, Step: 30 * time.Second}
	}
	return plugin.MetricsSnapshot{PluginID: "metrics", ResourceID: res.ID, CollectedAt: end, Coverage: coverage, Signals: map[string]any{"memory_working_set_bytes_p95": 100.0, "cpu_cores_p95": 1.0, "restarts_30m_max": 0.0, "memory_working_set_bytes_points": 60, "cpu_cores_points": 60, "restarts_30m_points": 60}}, nil
}

func apiFixture(t *testing.T) (*Server, *store.Memory, *fake.Clientset) {
	t.Helper()
	st, err := store.OpenDurable(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	replicas := int32(1)
	client := fake.NewSimpleClientset(&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "test", UID: "test-uid", Generation: 1}, Spec: appsv1.DeploymentSpec{Replicas: &replicas, Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceMemory: quantity.MustParse("512Mi")}, Limits: corev1.ResourceList{corev1.ResourceMemory: quantity.MustParse("1Gi")}}}}}}}, Status: appsv1.DeploymentStatus{ObservedGeneration: 1, Replicas: 1, ReadyReplicas: 1, UpdatedReplicas: 1, AvailableReplicas: 1}})
	plugins := plugin.NewManager()
	if err := plugins.RegisterAction(k8splugin.NewWithPatcher(k8splugin.NewK8sPatcherFromClient(client))); err != nil {
		t.Fatal(err)
	}
	if err := plugins.RegisterMetrics(apiMetrics{}); err != nil {
		t.Fatal(err)
	}
	res := resource.Resource{
		ID: "test", Type: resource.TypeKubernetesDeployment, Provider: resource.ProviderKubernetes,
		ProviderResourceID: "test/app", Name: "app", Account: "test-cluster", Region: "local",
		Owner: "team", Environment: resource.EnvDevelopment, Criticality: resource.CriticalityLow,
		Metadata:     map[string]any{"namespace": "test", "name": "app"},
		CurrentState: map[string]any{"memory_request_bytes": int64(512 * 1024 * 1024), "memory_limit_bytes": int64(1024 * 1024 * 1024)},
	}
	if _, err := st.UpsertResource(context.Background(), res); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateRecommendation(context.Background(), store.Recommendation{ResourceID: "test", PluginID: k8splugin.ID, AlgorithmID: "test-algorithm", EvidenceRefs: []string{"metrics:test:baseline"}, ActionType: "k8s.patch_resources", Confidence: "medium", Parameters: map[string]any{"patch": k8splugin.PatchDiff{Resource: "memory", CurrentReq: 512 * 1024 * 1024, ProposedReq: 384 * 1024 * 1024, CurrentLimit: 1024 * 1024 * 1024, ProposedLimit: 1024 * 1024 * 1024}}}); err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(st, plugins, policy.NewEngine(), bootstrap.Config{Verification: bootstrap.VerificationConfig{Enabled: true, MetricsPluginID: "metrics", Wait: "5m", RollbackOnFailure: true, RollbackOnTimeout: true, MaxMemoryP95IncreaseRatio: 1.25, MaxCPUP95IncreaseRatio: 1.5}})
	if err != nil {
		t.Fatal(err)
	}
	return server, st, client
}

func TestReviewCannotExecuteAndSubmissionIsDurableAndIdempotent(t *testing.T) {
	server, st, client := apiFixture(t)
	request := func(path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		server.Handler().ServeHTTP(w, r)
		return w
	}
	if w := request("/api/recommendations/1/plan", `{"mode":"approved"}`); w.Code != http.StatusOK {
		t.Fatalf("plan: %d %s", w.Code, w.Body)
	}
	for _, action := range client.Actions() {
		if action.GetVerb() == "update" {
			t.Fatal("review mutated infrastructure")
		}
	}
	for i := 0; i < 2; i++ {
		if w := request("/api/recommendations/1/execute", `{"mode":"approved"}`); w.Code != http.StatusAccepted {
			t.Fatalf("execute: %d %s", w.Code, w.Body)
		}
	}
	jobs, err := st.ListJobs(context.Background())
	if err != nil || len(jobs) != 1 || jobs[0].State != "prepared" {
		t.Fatalf("jobs=%v err=%v", jobs, err)
	}
	for _, action := range client.Actions() {
		if action.GetVerb() == "update" {
			t.Fatal("request executed before durable worker")
		}
	}
}

func TestUntrustedOriginAndMalformedApprovalAreRejected(t *testing.T) {
	server, st, _ := apiFixture(t)
	for _, body := range []string{`broken`, `{"mode":"approved"} {}`, `{"actor":"spoofed","mode":"approved"}`, `{"extra":true}`} {
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/recommendations/1/execute", strings.NewReader(body)))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("malformed request accepted: %d", w.Code)
		}
	}
	r := httptest.NewRequest(http.MethodPost, "/api/recommendations/1/execute", strings.NewReader(`{"mode":"approved"}`))
	r.Header.Set("Origin", "https://untrusted.example")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatal("untrusted browser origin accepted")
	}
	jobs, _ := st.ListJobs(context.Background())
	if len(jobs) != 0 {
		t.Fatal("rejected request queued execution")
	}
}

func TestAuthorizationProtectsMutationAndDerivesActorFromToken(t *testing.T) {
	server, _, _ := apiFixture(t)
	t.Setenv("CONSIZE_VIEWER_TOKEN", "viewer-token-at-least-24-characters")
	t.Setenv("CONSIZE_OPERATOR_TOKEN", "operator-token-at-least-24-characters")
	authorizer, err := auth.New(auth.Config{Enabled: true, Tokens: []auth.TokenConfig{
		{Subject: "read-only-user", Role: auth.RoleViewer, TokenEnv: "CONSIZE_VIEWER_TOKEN"},
		{Subject: "trusted-operator", Role: auth.RoleOperator, TokenEnv: "CONSIZE_OPERATOR_TOKEN"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	server.auth = authorizer

	request := func(method, path, token, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		server.Handler().ServeHTTP(w, r)
		return w
	}
	if w := request(http.MethodGet, "/api/resources", "viewer-token-at-least-24-characters", ""); w.Code != http.StatusOK {
		t.Fatalf("viewer read: %d %s", w.Code, w.Body)
	}
	if w := request(http.MethodPost, "/api/recommendations/1/execute", "viewer-token-at-least-24-characters", `{"mode":"approved"}`); w.Code != http.StatusForbidden {
		t.Fatalf("viewer mutation: %d %s", w.Code, w.Body)
	}
	if w := request(http.MethodPost, "/api/recommendations/1/execute", "", `{"mode":"approved"}`); w.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous mutation: %d %s", w.Code, w.Body)
	}
	if w := request(http.MethodPost, "/api/recommendations/1/execute", "operator-token-at-least-24-characters", `{"mode":"approved","actor":"spoofed"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("actor override: %d %s", w.Code, w.Body)
	}
}
