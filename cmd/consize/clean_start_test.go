package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/consize-oss/consize/internal/api"
)

func TestCleanStart(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	data, _ := json.Marshal(map[string]any{"state_path": filepath.Join(dir, "state.json")})
	if err := os.WriteFile(configPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"", configPath, configPath} {
		st, plugins, policies, cfg, err := optionalFoundation(ctx, path)
		if err != nil {
			t.Fatal(err)
		}
		server, err := initializeHTTPServer(ctx, st, plugins, policies, cfg, "", "", false)
		if err != nil {
			t.Fatal(err)
		}
		handler := server.Handler()
		for _, endpoint := range []string{"resources", "plugins", "actions", "action-records", "jobs", "audit"} {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/"+endpoint, nil))
			if w.Code != http.StatusOK {
				t.Fatalf("%s: %d %s", endpoint, w.Code, w.Body)
			}
			var response map[string]json.RawMessage
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			for key, raw := range response {
				var items []any
				if err := json.Unmarshal(raw, &items); err != nil || len(items) != 0 {
					t.Fatalf("seeded %s.%s: %s", endpoint, key, raw)
				}
			}
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/dashboard", nil))
		var dashboard api.Dashboard
		if err := json.Unmarshal(w.Body.Bytes(), &dashboard); err != nil {
			t.Fatal(err)
		}
		if w.Code != http.StatusOK || len(dashboard.Resources)+len(dashboard.Recommendations)+len(dashboard.Actions)+len(dashboard.ActionRecords)+len(dashboard.Jobs) != 0 || dashboard.Stats != (api.Stats{}) {
			t.Fatalf("seeded dashboard: %s", w.Body)
		}
		w = httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/health", nil))
		var health struct {
			Status      string
			Diagnostics []string
		}
		if err := json.Unmarshal(w.Body.Bytes(), &health); err != nil {
			t.Fatal(err)
		}
		if w.Code != http.StatusOK || health.Status != "degraded" || len(health.Diagnostics) == 0 {
			t.Fatalf("misleading health: %s", w.Body)
		}
		w = httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/discovery", nil))
		if w.Code != http.StatusBadGateway {
			t.Fatalf("missing discovery silently succeeded: %s", w.Body)
		}
		if err := st.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDemoInitializationRequiresExplicitOptIn(t *testing.T) {
	ctx := context.Background()
	for _, enabled := range []bool{false, true} {
		st, plugins, policies, cfg, err := optionalFoundation(ctx, "")
		if err != nil {
			t.Fatal(err)
		}
		defer st.Close()
		if _, err := initializeHTTPServer(ctx, st, plugins, policies, cfg, "", "", enabled); err != nil {
			t.Fatal(err)
		}
		resources, err := st.ListResources(ctx)
		if err != nil {
			t.Fatal(err)
		}
		recs, err := st.ListRecommendations(ctx)
		if err != nil {
			t.Fatal(err)
		}
		want := 0
		if enabled {
			want = 1
		}
		if len(resources) != want || len(recs) != want {
			t.Fatalf("demo=%t resources=%d recommendations=%d", enabled, len(resources), len(recs))
		}
	}
}
