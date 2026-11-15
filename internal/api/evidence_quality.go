package api

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"github.com/consize-oss/consize/internal/recommender"
	"github.com/consize-oss/consize/internal/verification"
	"github.com/consize-oss/consize/pkg/evidence/quality"
)

// On-demand diagnostics use configured providers and operator authorization.
// No arbitrary provider, query, endpoint or credential is accepted from callers.
func (s *Server) handleEvidenceQuality(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if len(r.URL.RawQuery) > 8192 {
		writeError(w, http.StatusBadRequest, "quality query too large")
		return
	}
	q, queryError := url.ParseQuery(r.URL.RawQuery)
	if queryError != nil {
		writeError(w, http.StatusBadRequest, "invalid quality query")
		return
	}
	for key, values := range q {
		if (key != "resource_id" && key != "purpose") || len(values) != 1 {
			writeError(w, http.StatusBadRequest, "invalid quality query")
			return
		}
	}
	id, purpose := q.Get("resource_id"), q.Get("purpose")
	if purpose == "" {
		purpose = quality.Reporting
	}
	if id == "" || len(id) > 512 || (purpose != quality.Reporting && purpose != quality.Recommendation && purpose != quality.Verification) {
		writeError(w, http.StatusBadRequest, "resource_id and a valid purpose are required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	res, err := s.st.GetResource(ctx, id)
	if err != nil {
		writeError(w, http.StatusNotFound, "resource not found")
		return
	}
	provider := s.cfg.Recommender.MetricsPluginID
	if purpose == quality.Verification {
		provider = s.cfg.Verification.MetricsPluginID
	}
	p, err := s.plugins.MetricsPlugin(provider, res.Type)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "configured evidence provider unavailable")
		return
	}
	snapshot, err := p.ReadMetrics(ctx, res)
	if err != nil {
		writeError(w, http.StatusBadGateway, "evidence collection failed")
		return
	}
	if snapshot.ResourceID != res.ID || snapshot.PluginID != provider {
		writeError(w, http.StatusBadGateway, "evidence identity mismatch")
		return
	}
	now := time.Now().UTC()
	var assessment quality.Assessment
	switch purpose {
	case quality.Recommendation:
		assessment = recommender.AssessQuality(s.cfg.Recommender, res, snapshot, now)
	case quality.Verification:
		assessment = verification.New(s.cfg.Verification).AssessQuality(snapshot, now)
	default:
		thresholds, _ := quality.Resolve(s.cfg.Recommender.Quality, quality.Reporting, false, false)
		key := s.cfg.Recommender.MetricKey
		if key == "" {
			key = "memory_working_set_bytes_p95"
		}
		assessment = snapshot.AssessQuality([]quality.Signal{{Name: key, Unit: "bytes"}}, purpose, thresholds, now)
	}
	writeJSON(w, http.StatusOK, assessment)
}
