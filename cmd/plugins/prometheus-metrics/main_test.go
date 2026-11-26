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
	t.Setenv("CON17_PLUGIN_TOKEN", "fixture-private-test-credential")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-private-test-credential" {
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
	for _, raw := range []string{`{"base_url":"http://localhost","transport":{"auth":{"password":"fixture-private-test-credential"}}}`, `{"base_url":"http://localhost","window":"fixture-private-test-credential"}`, `{"base_url":"http://localhost"} {}`, `null`} {
		if _, err := configuredPlugin([]byte(raw)); err == nil || strings.Contains(err.Error(), "fixture-private-test-credential") {
			t.Fatalf("unsafe config accepted/leaked: %v", err)
		}
	}
}

func TestStandaloneCollectionSettingsValidation(t *testing.T) {
	for _, fields := range []string{
		`"percentile":100`, `"minimum_samples":1`,
		`"queries":{"cpu_cores":"fixture_cpu"}`,
		`"queries":{"cpu_cores":"fixture_cpu"},"units":{"cpu_cores":"bytes"}`,
	} {
		if _, err := configuredPlugin(json.RawMessage(`{"base_url":"https://prometheus.example.invalid",` + fields + `}`)); err == nil {
			t.Fatalf("accepted invalid collection configuration: %s", fields)
		}
	}
	if _, err := configuredPlugin(json.RawMessage(`{"base_url":"https://prometheus.example.invalid","percentile":7,"minimum_samples":4,"queries":{"cpu_cores":"fixture_cpu"},"units":{"cpu_cores":"millicores"},"mapping":{"cluster":"source_cluster"}}`)); err != nil {
		t.Fatal(err)
	}
}

func TestStandaloneSourceQualityConfiguration(t *testing.T) {
	for _, fields := range []string{
		`"source_scrape_interval":"fixture-invalid-duration"`,
		`"source_timestamp_queries":{"cpu_cores":"timestamp(up)"}`,
		`"source_timestamp_queries":{"cpu_cores":"timestamp(up)"},"source_scrape_intervals":{"cpu_cores":"-1s"}`,
		`"source_scrape_intervals":{"cpu_cores":"fixture-invalid-duration"}`,
	} {
		if _, err := configuredPlugin(json.RawMessage(`{"base_url":"https://prometheus.example.invalid",` + fields + `}`)); err == nil || strings.Contains(err.Error(), "fixture-invalid-duration") {
			t.Fatal("invalid cadence accepted or leaked", err)
		}
	}
	if _, err := configuredPlugin(json.RawMessage(`{"base_url":"https://prometheus.example.invalid","source_timestamp_queries":{"cpu_cores":"timestamp(up)"},"source_scrape_intervals":{"cpu_cores":"15s"}}`)); err != nil {
		t.Fatal(err)
	}
}
