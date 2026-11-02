package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/consize-oss/consize/internal/audit"
	"github.com/consize-oss/consize/internal/auth"
	"github.com/consize-oss/consize/internal/bootstrap"
	"github.com/consize-oss/consize/internal/policy"
	"github.com/consize-oss/consize/internal/store"
	"github.com/consize-oss/consize/internal/testevidence"
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
	return testevidence.Attach(plugin.MetricsSnapshot{PluginID: "metrics", ResourceID: res.ID, CollectedAt: end, Coverage: coverage, Signals: map[string]any{"memory_working_set_bytes_p95": 100.0, "cpu_cores_p95": 1.0, "restarts_30m_max": 0.0, "memory_working_set_bytes_points": 60, "cpu_cores_points": 60, "restarts_30m_points": 60}}), nil
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
	planResponse := request("/api/recommendations/1/plan", `{"mode":"approved"}`)
	if w := planResponse; w.Code != http.StatusOK {
		t.Fatalf("plan: %d %s", w.Code, w.Body)
	}
	correlationID := planResponse.Header().Get("X-Request-ID")
	auditPage, err := st.QueryAuditEvents(context.Background(), audit.Query{CorrelationID: correlationID, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(auditPage.Events) != 3 {
		t.Fatalf("plan audit chain has %d events: %#v", len(auditPage.Events), auditPage.Events)
	}
	if auditPage.Events[1].CausationID != auditPage.Events[0].ID {
		t.Fatalf("planned event does not link to request: %#v", auditPage.Events)
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

func TestAuditQuerySummaryFiltersAndExport(t *testing.T) {
	server, st, _ := apiFixture(t)
	w := audit.NewWriter(st)
	for i, visibility := range []audit.Visibility{audit.VisibilitySummary, audit.VisibilityDetail} {
		_, err := w.Append(context.Background(), audit.Input{
			IdempotencyKey: fmt.Sprintf("audit-api-%d", i), Type: "policy.evaluated",
			Family: audit.FamilyPolicy, Kind: audit.KindDecision,
			Actor: audit.Actor{Type: "user", ID: "operator"}, Source: "test",
			Resource: &audit.ResourceRef{ID: "test"}, CorrelationID: "workflow-1",
			Workflow: audit.WorkflowRefs{PolicyID: "policy-1", PluginID: "policy-plugin"},
			Result:   audit.ResultAllowed, Severity: audit.SeverityInfo, Visibility: visibility,
			ReasonCode: "policy_allowed", Summary: "Policy allowed the operation",
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	request := func(path string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		return recorder
	}
	response := request("/api/audit?workflow_id=policy-1&plugin_id=policy-plugin&view=summary")
	if response.Code != http.StatusOK {
		t.Fatalf("query: %d %s", response.Code, response.Body)
	}
	var page audit.Page
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 1 || page.Events[0].Visibility != audit.VisibilitySummary {
		t.Fatalf("summary filter returned %#v", page.Events)
	}
	exported := request("/api/audit/export?correlation_id=workflow-1")
	if exported.Code != http.StatusOK || exported.Header().Get("Content-Type") != "application/x-ndjson" {
		t.Fatalf("export: %d %s", exported.Code, exported.Body)
	}
	lines := strings.Split(strings.TrimSpace(exported.Body.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("exported %d lines: %s", len(lines), exported.Body.String())
	}
	if bad := request("/api/audit?limit=0"); bad.Code != http.StatusBadRequest {
		t.Fatalf("invalid limit accepted: %d", bad.Code)
	}
	if bad := request("/api/audit?cursor=broken"); bad.Code != http.StatusBadRequest {
		t.Fatalf("invalid cursor status = %d", bad.Code)
	}
}

func TestAuditHealthAndIntegrityEndpoints(t *testing.T) {
	server, _, _ := apiFixture(t)
	for _, path := range []string{"/api/audit/health", "/api/audit/integrity"} {
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body)
		}
	}
}

func TestRecommendationAPIExposesImmutableProvenance(t *testing.T) {
	server, _, _ := apiFixture(t)
	server.cfg.Recommender.MetricsPluginID = "metrics"
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/recommendations/generate?resource_id=test", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("generate: %d %s", w.Code, w.Body)
	}
	var response struct {
		Recommendations []store.Recommendation `json:"recommendations"`
		Evidence        plugin.MetricsSnapshot `json:"evidence"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Recommendations) != 1 || len(response.Evidence.Records) == 0 {
		t.Fatal("missing API provenance")
	}
	rec := response.Recommendations[0]
	if len(rec.EvidenceSnapshot) != len(response.Evidence.Records) || rec.AlgorithmVersion == "" {
		t.Fatal("missing frozen inputs")
	}
	for i, r := range rec.EvidenceSnapshot {
		if err := r.Validate(); err != nil {
			t.Fatal(err)
		}
		if r.ID != rec.EvidenceRefs[i] || r.ID != response.Evidence.Records[i].ID {
			t.Fatal("API evidence mismatch")
		}
	}
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/dashboard", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), rec.EvidenceRefs[0]) || !strings.Contains(w.Body.String(), "evidence_snapshot") {
		t.Fatalf("list lost provenance: %d", w.Code)
	}
}

func TestResourcePagesPreserveLegacyAndBindFilters(t *testing.T) {
	server, st, _ := apiFixture(t)
	ctx := context.Background()
	original, err := st.GetResource(ctx, "test")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"page-a", "page-b", "page-c"} {
		r := original
		r.ID = id
		r.ProviderResourceID = id
		r.Owner = "paging-owner"
		if _, err = st.UpsertResource(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	request := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		return w
	}
	legacy := request("/api/resources")
	if legacy.Code != http.StatusOK || strings.Contains(legacy.Body.String(), "has_more") {
		t.Fatalf("legacy response changed: %d %s", legacy.Code, legacy.Body)
	}
	var page resource.Page
	w := request("/api/resources?owner=paging-owner&limit=2")
	if err = json.Unmarshal(w.Body.Bytes(), &page); err != nil || w.Code != 200 || len(page.Resources) != 2 || page.Resources[0].ID != "page-a" || !page.HasMore {
		t.Fatalf("first page: %d %s %v", w.Code, w.Body, err)
	}
	w = request("/api/resources?owner=paging-owner&limit=2&cursor=" + page.NextCursor)
	if err = json.Unmarshal(w.Body.Bytes(), &page); err != nil || len(page.Resources) != 1 || page.Resources[0].ID != "page-c" || page.HasMore {
		t.Fatalf("last page: %d %s", w.Code, w.Body)
	}
	for _, query := range []string{"limit=0", "limit=201", "limit=invalid", "cursor=malformed", "provider=aws&provider=kubernetes", "unknown=value", "lifecycle_state=unknown"} {
		if bad := request("/api/resources?" + query); bad.Code != 400 {
			t.Fatalf("invalid filter accepted: %s %d", query, bad.Code)
		}
	}
}
