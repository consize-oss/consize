package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/consize-oss/consize/internal/auth"
	"github.com/consize-oss/consize/internal/discovery"
	"github.com/consize-oss/consize/internal/store"
)

func (s *Server) handleDiscoveryRuns(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if s.discoveryScheduler == nil {
		writeError(w, 503, "durable discovery store unavailable")
		return
	}
	st := s.st.(store.DiscoveryRunStore)
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	switch r.Method {
	case http.MethodPost:
		if r.URL.RawQuery != "" {
			writeError(w, 400, "query parameters are not accepted")
			return
		}
		var req struct {
			PluginID string `json:"plugin_id"`
		}
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		d.DisallowUnknownFields()
		if d.Decode(&req) != nil || d.Decode(new(any)) != io.EOF || req.PluginID == "" || len(req.PluginID) > 128 {
			writeError(w, 400, "invalid discovery request")
			return
		}
		identity, ok := auth.FromContext(ctx)
		if !ok {
			writeError(w, 401, "authenticated identity unavailable")
			return
		}
		run, err := s.discoveryScheduler.Request(ctx, req.PluginID, identity.Subject)
		if errors.Is(err, discovery.ErrProviderScope) {
			writeError(w, 400, "configured scoped provider required")
			return
		}
		if errors.Is(err, store.ErrDiscoveryBusy) {
			writeError(w, 409, "discovery scope already has pending work")
			return
		}
		if err != nil {
			writeError(w, 503, "discovery storage unavailable")
			return
		}
		writeJSON(w, 202, run)
	case http.MethodGet:
		if len(r.URL.RawQuery) > 2048 {
			writeError(w, 400, "invalid discovery query")
			return
		}
		q, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil {
			writeError(w, 400, "invalid discovery query")
			return
		}
		for k, v := range q {
			if (k != "id" && k != "plugin_id") || len(v) != 1 || len(v[0]) > 512 || k == "plugin_id" && len(v[0]) > 128 {
				writeError(w, 400, "invalid discovery query")
				return
			}
		}
		if id := q.Get("id"); id != "" {
			if q.Get("plugin_id") != "" {
				writeError(w, 400, "use id or plugin_id")
				return
			}
			run, e := st.GetDiscoveryRun(ctx, id)
			if errors.Is(e, store.ErrNotFound) {
				writeError(w, 404, "discovery run not found")
				return
			}
			if e != nil {
				writeError(w, 503, "discovery storage unavailable")
				return
			}
			writeJSON(w, 200, run)
			return
		}
		runs, err := st.ListDiscoveryRuns(ctx, store.DiscoveryRunQuery{PluginID: q.Get("plugin_id"), Limit: 100})
		if err != nil {
			writeError(w, 503, "discovery storage unavailable")
			return
		}
		writeJSON(w, 200, runs)
	default:
		writeError(w, 405, "method not allowed")
	}
}
