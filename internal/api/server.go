package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/consize-oss/consize/internal/audit"
	"github.com/consize-oss/consize/internal/auth"
	"github.com/consize-oss/consize/internal/bootstrap"
	"github.com/consize-oss/consize/internal/cost"
	"github.com/consize-oss/consize/internal/discovery"
	"github.com/consize-oss/consize/internal/orchestrator"
	"github.com/consize-oss/consize/internal/policy"
	"github.com/consize-oss/consize/internal/recommender"
	"github.com/consize-oss/consize/internal/safety"
	"github.com/consize-oss/consize/internal/store"
	"github.com/consize-oss/consize/pkg/plugin"
	"github.com/consize-oss/consize/pkg/resource"
)

type Server struct {
	controller *safety.Controller
	auth       *auth.Authorizer
	actionMu   sync.Mutex
	st         store.Store
	plugins    *plugin.Manager
	policies   *policy.Engine
	cfg        bootstrap.Config
}

var requestSequence atomic.Uint64

type Dashboard struct {
	Verification    bootstrap.VerificationConfig `json:"verification"`
	Jobs            []store.Job                  `json:"jobs"`
	Stats           Stats                        `json:"stats"`
	Resources       []resource.Resource          `json:"resources"`
	Recommendations []store.Recommendation       `json:"recommendations"`
	Plugins         []PluginStatus               `json:"plugins"`
	Actions         []store.ActionEvent          `json:"actions"`
	ActionRecords   []store.Action               `json:"action_records"`
	Policy          PolicySummary                `json:"policy"`
}

type Stats struct {
	ProjectedSavingsMonthly float64 `json:"projected_savings_monthly"`
	VerifiedSavingsMonthly  float64 `json:"verified_savings_monthly"`
	PendingApprovals        int     `json:"pending_approvals"`
	RollbackRate            float64 `json:"rollback_rate"`
	OpenRecommendations     int     `json:"open_recommendations"`
	ExecutedActions         int     `json:"executed_actions"`
}

type PluginStatus struct {
	Manifest plugin.Manifest `json:"manifest"`
	Health   plugin.Health   `json:"health"`
}

type PolicySummary struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Mode          string `json:"mode"`
	Verification  string `json:"verification"`
	ApprovalModel string `json:"approval_model"`
}

type actionRequest struct {
	Mode string `json:"mode"`
}

func NewServer(st store.Store, plugins *plugin.Manager, policies *policy.Engine, cfg bootstrap.Config) (*Server, error) {
	authorizer, err := auth.New(cfg.Auth)
	if err != nil {
		return nil, err
	}
	s := &Server{st: st, plugins: plugins, policies: policies, cfg: cfg, auth: authorizer}
	if durable, ok := st.(store.JobStore); ok {
		s.controller = safety.New(durable, plugins, policies, cfg.Verification, cfg.Recommender)
	}
	return s, nil
}

func (s *Server) RunWorker(ctx context.Context) error {
	if s.controller == nil {
		return fmt.Errorf("durable worker store unavailable")
	}
	return s.controller.Run(ctx)
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", s.withCORS(s.handleHealth))
	mux.HandleFunc("/api/dashboard", s.protect(auth.RoleViewer, s.handleDashboard))
	mux.HandleFunc("/api/resources", s.protect(s.resourceRole, s.handleResources))
	mux.HandleFunc("/api/discovery", s.protect(auth.RoleAdmin, s.handleDiscovery))
	mux.HandleFunc("/api/plugins", s.protect(auth.RoleViewer, s.handlePlugins))
	mux.HandleFunc("/api/recommendations/generate", s.protect(auth.RoleOperator, s.handleGenerateRecommendation))
	mux.HandleFunc("/api/recommendations/", s.protect(s.recommendationRole, s.handleRecommendationAction))
	mux.HandleFunc("/api/actions", s.protect(auth.RoleViewer, s.handleActions))
	mux.HandleFunc("/api/action-records", s.protect(auth.RoleViewer, s.handleActionRecords))
	mux.HandleFunc("/api/jobs", s.protect(auth.RoleViewer, s.handleJobs))
	return s.logRequests(mux)
}

func (s *Server) handleDiscovery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	results, err := discovery.New(s.st, s.plugins).Run(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if err := s.writeAudit("discovery", results); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"providers": results})
}

func (s *Server) handleJobs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	st, ok := s.st.(store.JobStore)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "job store unavailable")
		return
	}
	jobs, err := st.ListJobs(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": jobs})
}

func (s *Server) SeedDemo(ctx context.Context) error {
	resources, err := s.st.ListResources(ctx)
	if err != nil {
		return err
	}
	if len(resources) > 0 {
		return nil
	}
	res := resource.Resource{
		Type:               resource.TypeKubernetesDeployment,
		Provider:           resource.ProviderKubernetes,
		ProviderResourceID: "payments/payment-service-deployment",
		Name:               "payment-service-deployment",
		Environment:        resource.EnvProduction,
		Owner:              "platform",
		Region:             "us-east-1",
		Account:            "acme-prod",
		Criticality:        resource.CriticalityHigh,
		MonthlyCost:        15000,
		Labels:             map[string]string{"namespace": "payments", "service": "payments"},
		Metadata:           map[string]any{"namespace": "payments", "name": "payment-service-deployment", "pod_regex": "payment-service-.+"},
		CurrentState:       map[string]any{"memory_request_bytes": 8589934592, "memory_limit_bytes": 17179869184},
	}
	res, err = s.st.UpsertResource(ctx, res)
	if err != nil {
		return err
	}
	_, err = s.st.CreateRecommendation(ctx, store.Recommendation{
		ResourceID:              res.ID,
		PluginID:                "kubernetes-action",
		AlgorithmID:             recommender.BuiltInHeadroomAlgorithmID,
		AlgorithmVersion:        "1",
		EvidenceRefs:            []string{"demo:metrics:payment-service-deployment"},
		ActionType:              "k8s.patch_resources",
		Title:                   "Reduce memory request for payment-service-deployment",
		Summary:                 "Reduce memory request while retaining 35% P95 headroom and requiring policy approval for production.",
		EstimatedSavingsMonthly: 4200,
		Confidence:              "medium",
		Risk:                    "low",
		PolicyID:                "oss-foundation-v1",
		Evidence:                []string{"Datadog/Prometheus memory P95 is stable", "No restart increase detected", "Production policy requires PR or approval path"},
		Current:                 map[string]any{"resource": "memory", "request": 8589934592, "limit": 17179869184},
		Proposed:                map[string]any{"resource": "memory", "request": 6442450944, "limit": 12884901888},
		Parameters: map[string]any{
			"patch": map[string]any{
				"resource":         "memory",
				"current_request":  8589934592,
				"proposed_request": 6442450944,
				"current_limit":    17179869184,
				"proposed_limit":   12884901888,
			},
			"proposed": map[string]any{"resource": "memory", "request": 6442450944, "limit": 12884901888},
		},
	})
	return err
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if err := s.st.Health(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "unhealthy", "store": "unavailable", "error": err.Error()})
		return
	}
	statuses, err := s.pluginStatuses(r.Context())
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "unhealthy", "store": "healthy", "error": err.Error()})
		return
	}
	status := "healthy"
	for _, candidate := range statuses {
		if candidate.Health.Status != "healthy" {
			status = "degraded"
			break
		}
	}
	response := map[string]any{
		"status":         status,
		"store":          "healthy",
		"durable_worker": s.controller != nil,
		"plugins":        statuses,
	}
	if diagnostics, ok := s.st.(store.DiagnosticsStore); ok {
		response["storage"] = diagnostics.StorageDiagnostics(r.Context())
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	dashboard, err := s.dashboard(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, dashboard)
}

func (s *Server) handleResources(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		resources, err := s.st.ListResources(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"resources": resources})
	case http.MethodPost:
		var res resource.Resource
		if err := json.NewDecoder(r.Body).Decode(&res); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		out, err := s.st.UpsertResource(r.Context(), res)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, out)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handlePlugins(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	plugins, err := s.pluginStatuses(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"plugins": plugins})
}

func (s *Server) handleActions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	actions, err := s.st.ListActionEvents(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"actions": actions})
}

func (s *Server) handleActionRecords(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	actions, err := s.st.ListActions(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"actions": actions})
}

func (s *Server) handleGenerateRecommendation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	resourceID := r.URL.Query().Get("resource_id")
	if resourceID == "" {
		writeError(w, http.StatusBadRequest, "resource_id is required")
		return
	}
	res, err := s.st.GetResource(r.Context(), resourceID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	metricsPluginID := s.cfg.Recommender.MetricsPluginID
	if metricsPluginID == "" {
		metricsPluginID = "prometheus-metrics"
	}
	p, err := s.plugins.MetricsPlugin(metricsPluginID, res.Type)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	snapshot, err := p.ReadMetrics(r.Context(), res)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	recs, err := recommender.New(s.cfg.Recommender).Recommend(r.Context(), recommender.Input{Resource: res, Evidence: []plugin.MetricsSnapshot{snapshot}})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	created := make([]store.Recommendation, 0, len(recs))
	for _, rec := range recs {
		rec.ResourceID = res.ID
		costPluginID := ""
		if s.cfg.Pricing.Enabled {
			costPluginID = s.cfg.Pricing.EffectivePluginID()
		}
		rec, err = cost.Enrich(r.Context(), s.plugins, costPluginID, res, rec)
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		out, err := s.st.CreateRecommendation(r.Context(), rec)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		created = append(created, out)
	}
	writeJSON(w, http.StatusOK, map[string]any{"recommendations": created, "evidence": snapshot})
}

func (s *Server) handleRecommendationAction(w http.ResponseWriter, r *http.Request) {
	s.actionMu.Lock()
	defer s.actionMu.Unlock()
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	id, op, err := parseRecommendationPath(r.URL.Path)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	mode := "dry_run"
	if op == "execute" {
		mode = "approved"
	}
	var req actionRequest
	if r.Body != nil {
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&req); err != nil && err != io.EOF {
			writeError(w, http.StatusBadRequest, "invalid action request")
			return
		}
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			writeError(w, http.StatusBadRequest, "expected one JSON object")
			return
		}
	}
	if req.Mode != "" && op == "execute" {
		mode = req.Mode
	}
	identity, ok := auth.FromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authenticated identity is unavailable")
		return
	}
	actor := identity.Subject
	if op == "recover" {
		if req.Mode != "approved" || s.controller == nil {
			writeError(w, http.StatusBadRequest, "recovery requires explicit approval and durable controller")
			return
		}
		job, err := s.controller.Recover(r.Context(), id, actor)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"job": job})
		return
	}
	if op == "execute" {
		if mode != "approved" {
			writeError(w, http.StatusBadRequest, "execution requires explicit approved mode")
			return
		}
		if s.controller == nil {
			writeError(w, http.StatusServiceUnavailable, "durable worker unavailable")
			return
		}
		job, err := s.controller.Submit(r.Context(), id, actor, r.Header.Get("Idempotency-Key"))
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"job": job})
		return
	}
	out, err := orchestrator.New(s.st, s.plugins, s.policies).ExecuteRecommendation(r.Context(), id, mode, actor, r.Header.Get("Idempotency-Key"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.writeAudit("action", out); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) dashboard(ctx context.Context) (Dashboard, error) {
	var jobs []store.Job
	if st, ok := s.st.(store.JobStore); ok {
		var err error
		jobs, err = st.ListJobs(ctx)
		if err != nil {
			return Dashboard{}, err
		}
	}
	resources, err := s.st.ListResources(ctx)
	if err != nil {
		return Dashboard{}, err
	}
	recs, err := s.st.ListRecommendations(ctx)
	if err != nil {
		return Dashboard{}, err
	}
	actions, err := s.st.ListActionEvents(ctx)
	if err != nil {
		return Dashboard{}, err
	}
	actionRecords, err := s.st.ListActions(ctx)
	if err != nil {
		return Dashboard{}, err
	}
	plugins, err := s.pluginStatuses(ctx)
	if err != nil {
		return Dashboard{}, err
	}
	return Dashboard{
		Verification:    s.cfg.Verification,
		Jobs:            jobs,
		Stats:           stats(recs, actions),
		Resources:       resources,
		Recommendations: recs,
		Plugins:         plugins,
		Actions:         actions,
		ActionRecords:   actionRecords,
		Policy: PolicySummary{
			ID:            "oss-foundation-v1",
			Name:          "OSS Foundation Policy",
			Mode:          "Production approval required",
			Verification:  "Metrics-based when configured",
			ApprovalModel: "Review then explicitly approve; policy-controlled rollback",
		},
	}, nil
}

func (s *Server) pluginStatuses(ctx context.Context) ([]PluginStatus, error) {
	manifests := s.plugins.Manifests()
	out := make([]PluginStatus, 0, len(manifests))
	for _, manifest := range manifests {
		health, err := s.plugins.Health(ctx, manifest.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, PluginStatus{Manifest: manifest, Health: health})
	}
	return out, nil
}

func stats(recs []store.Recommendation, actions []store.ActionEvent) Stats {
	var out Stats
	for _, rec := range recs {
		if !store.RecommendationTerminal(rec.Status) && rec.Status != store.RecommendationExecuting {
			out.ProjectedSavingsMonthly += rec.EstimatedSavingsMonthly
			out.OpenRecommendations++
		}
		// Health verification alone does not establish realized billing savings.
		if rec.Status == store.RecommendationPending || rec.Status == store.RecommendationPlanned {
			out.PendingApprovals++
		}
	}
	applied := map[int64]bool{}
	rolledBack := map[int64]bool{}
	for _, action := range actions {
		if action.Result == string(store.ActionExecuting) || action.Result == "waiting_rollout" || action.Result == "verifying" || action.Result == "verified" {
			applied[action.RecommendationID] = true
		}
		if action.Result == "rolled_back" {
			rolledBack[action.RecommendationID] = true
		}
	}
	out.ExecutedActions = len(applied)
	if len(applied) > 0 {
		out.RollbackRate = float64(len(rolledBack)) / float64(len(applied)) * 100
	}
	return out
}

func parseRecommendationPath(path string) (int64, string, error) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 4 || parts[0] != "api" || parts[1] != "recommendations" {
		return 0, "", fmt.Errorf("unknown recommendation route")
	}
	id, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || id <= 0 {
		return 0, "", fmt.Errorf("invalid recommendation id")
	}
	if parts[3] != "plan" && parts[3] != "execute" && parts[3] != "recover" {
		return 0, "", fmt.Errorf("unknown recommendation operation")
	}
	return id, parts[3], nil
}

func (s *Server) writeAudit(recordType string, payload any) error {
	if s.cfg.Audit.Path == "" {
		return nil
	}
	sink, err := audit.NewJSONLSink(s.cfg.Audit.Path)
	if err != nil {
		return err
	}
	return sink.Write(recordType, payload)
}

func (s *Server) withCORS(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			allowed := []string{"http://127.0.0.1:3030", "http://localhost:3030", "http://127.0.0.1:3031", "http://localhost:3031", "http://127.0.0.1:3000", "http://localhost:3000"}
			if len(s.cfg.AllowedOrigins) > 0 {
				allowed = s.cfg.AllowedOrigins
			}
			matched := false
			for _, candidate := range allowed {
				if origin == candidate {
					matched = true
					break
				}
			}
			if !matched {
				writeError(w, http.StatusForbidden, "browser origin is not allowed")
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
		}
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next(w, r)
	}
}

func (s *Server) protect(requiredRole any, next http.HandlerFunc) http.HandlerFunc {
	return s.withCORS(func(w http.ResponseWriter, r *http.Request) {
		var role string
		switch required := requiredRole.(type) {
		case string:
			role = required
		case func(*http.Request) string:
			role = required(r)
		default:
			writeError(w, http.StatusInternalServerError, "route authorization is misconfigured")
			return
		}
		authorized, _, err := s.auth.Authorize(r, role)
		if err != nil {
			status := http.StatusUnauthorized
			if _, authErr := s.auth.Authenticate(r); authErr == nil {
				status = http.StatusForbidden
			}
			writeError(w, status, err.Error())
			return
		}
		next(w, authorized)
	})
}

func (s *Server) resourceRole(r *http.Request) string {
	if r.Method == http.MethodGet {
		return auth.RoleViewer
	}
	return auth.RoleAdmin
}

func (s *Server) recommendationRole(r *http.Request) string {
	if strings.HasSuffix(r.URL.Path, "/recover") {
		return auth.RoleAdmin
	}
	return auth.RoleOperator
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (w *statusRecorder) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = fmt.Sprintf("req-%d", requestSequence.Add(1))
		}
		w.Header().Set("X-Request-ID", requestID)
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		slog.Info("http request", "request_id", requestID, "method", r.Method, "path", r.URL.Path, "status", recorder.status, "duration_ms", time.Since(started).Milliseconds())
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
