package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/consize-oss/consize/internal/auth"
	"github.com/consize-oss/consize/internal/collection"
	"github.com/consize-oss/consize/internal/store"
)

func TestCollectionAPIQueuesDurableOperationsAndRejectsOverrides(t *testing.T) {
	s, st, _ := apiFixture(t)
	var err error
	s.collector, err = collection.New(st, st, s.plugins, collection.Settings{Config: collection.Config{PluginID: "metrics", Window: "30m", Interval: "1m"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("CON22_VIEWER_TOKEN", "fixture-viewer-token-at-least-24-characters")
	t.Setenv("CON22_OPERATOR_TOKEN", "fixture-operator-token-at-least-24-characters")
	s.auth, err = auth.New(auth.Config{Enabled: true, Tokens: []auth.TokenConfig{{Subject: "fixture-viewer", Role: auth.RoleViewer, TokenEnv: "CON22_VIEWER_TOKEN"}, {Subject: "fixture-operator", Role: auth.RoleOperator, TokenEnv: "CON22_OPERATOR_TOKEN"}}})
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path, body, token string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		s.Handler().ServeHTTP(w, r)
		return w
	}
	opToken := "fixture-operator-token-at-least-24-characters"
	for _, tc := range []struct {
		method, path, body, token string
		status                    int
	}{
		{"POST", "/api/evidence/collections", `{"resource_id":"test"}`, "", 401},
		{"POST", "/api/evidence/collections", `{"resource_id":"test"}`, "fixture-viewer-token-at-least-24-characters", 403},
		{"POST", "/api/evidence/collections", `{"resource_id":"test","actor":"spoof"}`, opToken, 400},
		{"POST", "/api/evidence/collections", `{"resource_id":"test","query":"arbitrary"}`, opToken, 400},
		{"POST", "/api/evidence/collections", `{"resource_id":"test"} {}`, opToken, 400},
		{"POST", "/api/evidence/collections", `{"resource_id":"missing"}`, opToken, 404},
		{"POST", "/api/evidence/collections", fmt.Sprintf(`{"resource_id":"test","evaluation_at":%q}`, time.Now().Add(time.Hour).Format(time.RFC3339Nano)), opToken, 400},
		{"GET", "/api/evidence/collections?id=x&id=y", "", opToken, 400},
		{"GET", "/api/evidence/collections?url=http://example.invalid", "", opToken, 400},
		{"GET", "/api/evidence/collections?id=missing", "", opToken, 404},
	} {
		w := request(tc.method, tc.path, tc.body, tc.token)
		if w.Code != tc.status {
			t.Fatalf("%s %s: %d %s", tc.method, tc.path, w.Code, w.Body)
		}
	}
	w := request("POST", "/api/evidence/collections", `{"resource_id":"test"}`, opToken)
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body)
	}
	var job store.Collection
	if err = json.Unmarshal(w.Body.Bytes(), &job); err != nil {
		t.Fatal(err)
	}
	if job.Actor != "fixture-operator" || job.State != "pending" || job.Snapshot != nil {
		t.Fatalf("operation=%+v", job)
	}
	again := request("POST", "/api/evidence/collections", `{"resource_id":"test"}`, opToken)
	var duplicate store.Collection
	json.Unmarshal(again.Body.Bytes(), &duplicate)
	if duplicate.ID != job.ID {
		t.Fatal("manual retry did not deduplicate")
	}
	if err = s.collector.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	w = request("GET", "/api/evidence/collections?id="+job.ID, "", opToken)
	if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, w.Body)
	}
	var done store.Collection
	json.Unmarshal(w.Body.Bytes(), &done)
	if done.State != "degraded" && done.State != "succeeded" {
		t.Fatalf("job=%+v", done)
	}
	// This legacy provider fixture omits several required signals: the durable result must remain degraded.
	if done.State != "degraded" || done.Snapshot == nil {
		t.Fatal("partial provider result was promoted to complete")
	}
}
