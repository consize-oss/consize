package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/consize-oss/consize/internal/bootstrap"
	"github.com/consize-oss/consize/internal/policy"
	"github.com/consize-oss/consize/internal/store"
	"github.com/consize-oss/consize/pkg/plugin"
)

func TestOptionalPluginFailureDoesNotPreventAPIStartupOrInventory(t *testing.T) {
	manager := plugin.NewManager()
	cfg := bootstrap.Config{PluginInstances: []plugin.InstanceConfig{{ID: "optional-metrics", PackageID: "prometheus-metrics", Provider: "kubernetes", Account: "cluster", Location: "local", Configuration: json.RawMessage(`{"base_url":"http://private-token-secret@invalid"}`)}}}
	cfg.Recommender.MetricsPluginID = "optional-metrics"
	if err := bootstrap.RegisterConfiguredPlugins(context.Background(), manager, cfg); err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(store.NewMemory(), manager, policy.NewEngine(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/api/plugins", nil))
	if w.Code != 200 || strings.Contains(w.Body.String(), "private-token-secret") {
		t.Fatal(w.Code, w.Body)
	}
	var body struct {
		Plugins []PluginStatus `json:"plugins"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || len(body.Plugins) != 1 || body.Plugins[0].Runtime == nil || body.Plugins[0].Runtime.State != plugin.StateFailed {
		t.Fatal(body, err)
	}
}
