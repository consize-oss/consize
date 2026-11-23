package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/consize-oss/consize/internal/auth"
	"github.com/consize-oss/consize/internal/discovery"
	"github.com/consize-oss/consize/internal/store"
	"github.com/consize-oss/consize/pkg/plugin"
	k8splugin "github.com/consize-oss/consize/pkg/plugins/kubernetes"
)

func TestDiscoveryRunAPIRequiresAdminAndRejectsScopeOverrides(t *testing.T) {
	s, st, _ := apiFixture(t)
	s.plugins = plugin.NewManager()
	if err := s.plugins.RegisterDiscovery(k8splugin.NewWithPatcher(nil)); err != nil {
		t.Fatal(err)
	}
	var schedulerErr error
	s.discoveryScheduler, schedulerErr = discovery.NewScheduler(st, s.plugins, s.cfg.Discovery)
	if schedulerErr != nil {
		t.Fatal(schedulerErr)
	}
	t.Setenv("CON15_ADMIN_TOKEN", "fixture-admin-token-at-least-24-characters")
	t.Setenv("CON15_OPERATOR_TOKEN", "fixture-operator-token-at-least-24-characters")
	var err error
	s.auth, err = auth.New(auth.Config{Enabled: true, Tokens: []auth.TokenConfig{{Subject: "fixture-admin", Role: auth.RoleAdmin, TokenEnv: "CON15_ADMIN_TOKEN"}, {Subject: "fixture-operator", Role: auth.RoleOperator, TokenEnv: "CON15_OPERATOR_TOKEN"}}})
	if err != nil {
		t.Fatal(err)
	}
	admin := "fixture-admin-token-at-least-24-characters"
	request := func(method, path, body, token string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		s.Handler().ServeHTTP(w, r)
		return w
	}
	for _, tc := range []struct {
		method, path, body, token string
		status                    int
	}{
		{"POST", "/api/discovery/runs", `{"plugin_id":"kubernetes-action"}`, "", 401},
		{"POST", "/api/discovery/runs", `{"plugin_id":"kubernetes-action"}`, "fixture-operator-token-at-least-24-characters", 403},
		{"POST", "/api/discovery/runs", `{"plugin_id":"kubernetes-action","scope":{"namespaces":"*"}}`, admin, 400},
		{"POST", "/api/discovery/runs", `{"plugin_id":"kubernetes-action","actor":"spoof"}`, admin, 400},
		{"POST", "/api/discovery/runs", `{"plugin_id":"kubernetes-action"} {}`, admin, 400},
		{"POST", "/api/discovery/runs", `{"plugin_id":"unknown"}`, admin, 400},
		{"GET", "/api/discovery/runs?id=x&id=y", "", admin, 400},
		{"GET", "/api/discovery/runs?id=missing", "", admin, 404},
	} {
		w := request(tc.method, tc.path, tc.body, tc.token)
		if w.Code != tc.status {
			t.Fatalf("%s %s: %d %s", tc.method, tc.path, w.Code, w.Body)
		}
	}
	w := request("POST", "/api/discovery/runs", `{"plugin_id":"kubernetes-action"}`, admin)
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body)
	}
	var run store.DiscoveryRun
	if err = json.Unmarshal(w.Body.Bytes(), &run); err != nil {
		t.Fatal(err)
	}
	if run.Actor != "fixture-admin" || run.State != "pending" || run.Attempts != 0 {
		t.Fatal(run)
	}
	jobs, err := st.ListDiscoveryRuns(context.Background(), store.DiscoveryRunQuery{})
	if err != nil || len(jobs) != 1 {
		t.Fatal(jobs, err)
	}
	w = request("GET", "/api/discovery/runs?id="+run.ID, "", admin)
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, w.Body)
	}
	again := request("POST", "/api/discovery/runs", `{"plugin_id":"kubernetes-action"}`, admin)
	var duplicate store.DiscoveryRun
	json.Unmarshal(again.Body.Bytes(), &duplicate)
	if duplicate.ID != run.ID {
		t.Fatal("manual request did not deduplicate")
	}
}
