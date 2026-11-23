package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/consize-oss/consize/internal/auth"
	"github.com/consize-oss/consize/internal/recommender"
	"github.com/consize-oss/consize/pkg/evidence/quality"
	"github.com/consize-oss/consize/pkg/plugin"
	"github.com/consize-oss/consize/pkg/resource"
)

func TestQualityAPIReportsReasonsWithoutProviderPayloads(t *testing.T) {
	s, _, _ := apiFixture(t)
	s.cfg.Recommender.MetricsPluginID = "metrics"
	s.cfg.Recommender.ConfidenceProfile = "lab"
	for _, purpose := range []string{quality.Reporting, quality.Recommendation, quality.Verification} {
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/evidence/quality?resource_id=test&purpose="+purpose, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", purpose, w.Code, w.Body)
		}
		var report quality.Assessment
		if err := json.Unmarshal(w.Body.Bytes(), &report); err != nil {
			t.Fatal(err)
		}
		if !report.Eligible || report.Version != quality.Version || len(report.Dimensions) == 0 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("invalid report: %+v", report)
		}
		if strings.Contains(w.Body.String(), "synthetic fixture") {
			t.Fatal("query leaked")
		}
	}
	for _, query := range []string{"", "resource_id=test&purpose=bad", "resource_id=test&resource_id=other", "resource_id=test&source=http://example.com"} {
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/evidence/quality?"+query, nil))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s: %d", query, w.Code)
		}
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/evidence/quality?resource_id=test", nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatal(w.Code)
	}
}
func TestQualityAPIRequiresOperatorAndRedactsCollectionErrors(t *testing.T) {
	s, _, _ := apiFixture(t)
	s.cfg.Recommender.MetricsPluginID = "quality-error"
	p := &qualityErrorMetrics{}
	if err := s.plugins.RegisterMetrics(p); err != nil {
		t.Fatal(err)
	}
	t.Setenv("QUALITY_VIEWER_TOKEN", "viewer-token-with-24-characters")
	t.Setenv("QUALITY_OPERATOR_TOKEN", "operator-token-with-24-characters")
	var err error
	s.auth, err = auth.New(auth.Config{Enabled: true, Tokens: []auth.TokenConfig{{Subject: "reader", Role: auth.RoleViewer, TokenEnv: "QUALITY_VIEWER_TOKEN"}, {Subject: "operator", Role: auth.RoleOperator, TokenEnv: "QUALITY_OPERATOR_TOKEN"}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		token  string
		status int
	}{{"", 401}, {"viewer-token-with-24-characters", 403}, {"operator-token-with-24-characters", 502}} {
		r := httptest.NewRequest(http.MethodGet, "/api/evidence/quality?resource_id=test", nil)
		if tc.token != "" {
			r.Header.Set("Authorization", "Bearer "+tc.token)
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("got %d want %d: %s", w.Code, tc.status, w.Body)
		}
		if strings.Contains(w.Body.String(), "credential-secret") {
			t.Fatal("credential leaked")
		}
	}
	if p.calls != 1 || !p.deadline {
		t.Fatalf("unauthorized collection or missing deadline: %+v", p)
	}
}

type qualityErrorMetrics struct {
	calls    int
	deadline bool
}

func (*qualityErrorMetrics) ID() string { return "quality-error" }
func (*qualityErrorMetrics) Manifest() plugin.Manifest {
	return plugin.Manifest{ID: "quality-error", Category: plugin.CategoryMetrics, Capabilities: []string{plugin.CapabilityMetricsRead}, SupportedResourceTypes: []string{resource.TypeKubernetesDeployment}}
}
func (*qualityErrorMetrics) Health(context.Context) plugin.Health {
	return plugin.Health{Status: "healthy"}
}
func (p *qualityErrorMetrics) ReadMetrics(ctx context.Context, _ resource.Resource) (plugin.MetricsSnapshot, error) {
	p.calls++
	_, p.deadline = ctx.Deadline()
	return plugin.MetricsSnapshot{}, errors.New("http://credential-secret@example.test")
}

func TestQualityFailureDoesNotCreateRecommendation(t *testing.T) {
	s, st, _ := apiFixture(t)
	s.cfg.Recommender.Enabled = true
	s.cfg.Recommender.MetricsPluginID = "metrics"
	s.cfg.Recommender.ActionPluginID = "kubernetes-action"
	if err := recommender.Register(s.plugins, s.cfg.Recommender); err != nil {
		t.Fatal(err)
	}
	before, err := st.ListRecommendations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/recommendations/generate?resource_id=test", nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("%d: %s", w.Code, w.Body)
	}
	var body struct {
		Quality quality.Assessment `json:"quality"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Quality.Eligible || body.Quality.Version != quality.Version {
		t.Fatal("missing quality rejection")
	}
	after, err := st.ListRecommendations(context.Background())
	if err != nil || len(before) != len(after) {
		t.Fatal("quality rejection created a recommendation")
	}
}
