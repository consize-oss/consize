package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStandaloneConfigurationForwardsTransportAndRejectsUnsafeFields(t *testing.T) {
	t.Setenv("CON17_PLUGIN_TOKEN", "private-test-credential")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer private-test-credential" {
			t.Error("transport credentials not forwarded")
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/-/ready" {
			fmt.Fprint(w, "ready")
			return
		}
		fmt.Fprint(w, `{"status":"success","data":{"resultType":"scalar","result":[1700000000,"1"]}}`)
	}))
	defer server.Close()
	raw, err := json.Marshal(map[string]any{"base_url": server.URL, "transport": map[string]any{"auth": map[string]any{"bearer_token_env": "CON17_PLUGIN_TOKEN"}}})
	if err != nil {
		t.Fatal(err)
	}
	p, err := configuredPlugin(raw)
	if err != nil {
		t.Fatal(err)
	}
	if health := p.Health(context.Background()); health.Status != "healthy" || health.Details == nil {
		t.Fatalf("missing configured transport health: %+v", health)
	}
	for _, raw := range []string{`{"base_url":"http://localhost","transport":{"auth":{"password":"private-test-credential"}}}`, `{"base_url":"http://localhost","window":"private-test-credential"}`, `{"base_url":"http://localhost"} {}`, `null`} {
		if _, err := configuredPlugin([]byte(raw)); err == nil || strings.Contains(err.Error(), "private-test-credential") {
			t.Fatalf("unsafe config accepted/leaked: %v", err)
		}
	}
}
