package bootstrap

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/consize-oss/consize/pkg/plugin"
)

func TestConfiguredInstancesIsolateOptionalFactoryFailure(t *testing.T) {
	for _, required := range []bool{false, true} {
		m := plugin.NewManager()
		cfg := Config{PluginInstances: []plugin.InstanceConfig{{ID: "broken", PackageID: "prometheus-metrics", Provider: "kubernetes", Account: "production", Location: "local", Required: required, Configuration: json.RawMessage(`{"base_url":"http://private-token-secret@invalid"}`)}, {ID: "prices", PackageID: "configured-rate-card", Provider: "kubernetes", Account: "production", Location: "local", Required: true, Configuration: json.RawMessage(`{"currency":"USD","source":"fixture","memory_gib_monthly":1}`)}}}
		err := RegisterConfiguredPlugins(context.Background(), m, cfg)
		if (err != nil) != required {
			t.Fatal(required, err)
		}
		if err != nil && strings.Contains(err.Error(), "private-token-secret") {
			t.Fatal("factory error leaked configuration")
		}
		if !required {
			statuses := m.InstanceStatuses()
			if len(statuses) != 2 || statuses[0].State != plugin.StateFailed || statuses[1].State != plugin.StateReady {
				t.Fatal(statuses)
			}
			if _, err := m.CostPlugin("prices", "kubernetes.deployment"); err != nil {
				t.Fatal("optional failure hid working capability", err)
			}
		}
	}
}
