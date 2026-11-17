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
	"github.com/consize-oss/consize/internal/collection"
	"github.com/consize-oss/consize/internal/store"
)

func (s *Server) handleCollections(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if s.collector == nil {
		writeError(w, http.StatusServiceUnavailable, "durable collection store unavailable")
		return
	}
	st := s.st.(store.CollectionStore)
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	switch r.Method {
	case http.MethodPost:
		// No caller-selected provider, query, credential, source URL or attempt budget.
		if r.URL.RawQuery != "" {
			writeError(w, 400, "query parameters are not accepted")
			return
		}
		var req struct {
			ResourceID   string    `json:"resource_id"`
			EvaluationAt time.Time `json:"evaluation_at,omitempty"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&req) != nil || decoder.Decode(new(any)) != io.EOF || req.ResourceID == "" || len(req.ResourceID) > 512 {
			writeError(w, 400, "invalid collection request")
			return
		}
		actor := "local-operator"
		if identity, ok := auth.FromContext(ctx); ok {
			actor = identity.Subject
		}
		operation, err := s.collector.Request(ctx, req.ResourceID, actor, req.EvaluationAt)
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, 404, "resource not found")
			return
		}
		if errors.Is(err, store.ErrCollectionOverlap) {
			writeError(w, 409, "an overlapping collection is pending")
			return
		}
		if errors.Is(err, collection.ErrInvalidRequest) {
			writeError(w, 400, "resource or evaluation window is not eligible for collection")
			return
		}
		if err != nil {
			writeError(w, 503, "collection configuration or storage unavailable")
			return
		}
		writeJSON(w, http.StatusAccepted, operation)
	case http.MethodGet:
		if len(r.URL.RawQuery) > 2048 {
			writeError(w, 400, "invalid collection query")
			return
		}
		q, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil {
			writeError(w, 400, "invalid collection query")
			return
		}
		for key, values := range q {
			if (key != "id" && key != "resource_id") || len(values) != 1 || len(values[0]) > 512 {
				writeError(w, 400, "invalid collection query")
				return
			}
		}
		if id := q.Get("id"); id != "" {
			if q.Get("resource_id") != "" {
				writeError(w, 400, "use id or resource_id")
				return
			}
			c, e := st.GetCollection(ctx, id)
			if errors.Is(e, store.ErrNotFound) {
				writeError(w, 404, "collection not found")
				return
			}
			if e != nil {
				writeError(w, 503, "collection storage unavailable")
				return
			}
			writeJSON(w, 200, c)
			return
		}
		jobs, err := st.ListCollections(ctx, store.CollectionQuery{ResourceID: q.Get("resource_id"), Limit: 100})
		if err != nil {
			writeError(w, 503, "collection storage unavailable")
			return
		}
		writeJSON(w, 200, jobs)
	default:
		writeError(w, 405, "method not allowed")
	}
}
