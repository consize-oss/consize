package bootstrap

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/consize-oss/consize/pkg/plugin"
	"github.com/consize-oss/consize/pkg/plugins/prometheus"
)

func TestBootstrapForwardsPrometheusTransport(t *testing.T) {
	t.Setenv("CON17_BOOTSTRAP_TOKEN", "private-bootstrap-test-token")
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer private-bootstrap-test-token" {
			t.Error("missing configured authorization")
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/-/ready" {
			fmt.Fprint(w, "ready")
			return
		}
		fmt.Fprint(w, `{"status":"success","data":{"resultType":"scalar","result":[1700000000,"1"]}}`)
	}))
	defer s.Close()
	manager := plugin.NewManager()
	cfg := Config{Prometheus: PrometheusConfig{Enabled: true, BaseURL: s.URL, Transport: prometheus.TransportConfig{Auth: prometheus.AuthConfig{BearerTokenEnv: "CON17_BOOTSTRAP_TOKEN"}}}}
	if err := RegisterConfiguredPlugins(context.Background(), manager, cfg); err != nil {
		t.Fatal(err)
	}
	health, err := manager.Health(context.Background(), prometheus.ID)
	if err != nil || health.Status != "healthy" || health.Details == nil {
		t.Fatalf("health: %+v %v", health, err)
	}
}

func TestBootstrapRejectsInvalidMappingAliases(t *testing.T) {
	for _, mapping := range []prometheus.MappingConfig{{Cluster: "invalid-label"}, {Pod: "namespace"}, {Mode: "prefix"}} {
		err := RegisterConfiguredPlugins(context.Background(), plugin.NewManager(), Config{Prometheus: PrometheusConfig{Enabled: true, BaseURL: "http://127.0.0.1:9090", Mapping: mapping}})
		if err == nil {
			t.Fatal("invalid mapping configuration accepted")
		}
	}
}
