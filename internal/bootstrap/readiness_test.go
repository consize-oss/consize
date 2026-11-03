package bootstrap

import (
	"context"
	"strings"
	"testing"

	"github.com/consize-oss/consize/pkg/plugin"
)

func TestEmptyConfigurationDoesNotRegisterPlugins(t *testing.T) {
	m := plugin.NewManager()
	if err := RegisterConfiguredPlugins(context.Background(), m, Config{}); err != nil {
		t.Fatal(err)
	}
	if len(m.Manifests()) != 0 || len(CapabilityDiagnostics(m, Config{})) == 0 {
		t.Fatal("empty setup fabricated capabilities or omitted diagnostics")
	}
}

func TestEnabledWorkflowsRequireExplicitRegisteredCapabilities(t *testing.T) {
	for _, tc := range []struct {
		cfg  Config
		want string
	}{
		{Config{Recommender: RecommenderConfig{Enabled: true}}, "recommender.metrics_plugin_id"},
		{Config{Recommender: RecommenderConfig{Enabled: true, MetricsPluginID: "missing"}}, "registered metrics.read"},
		{Config{Verification: VerificationConfig{Enabled: true}}, "verification.metrics_plugin_id"},
		{Config{Verification: VerificationConfig{Enabled: true, MetricsPluginID: "missing"}}, "registered metrics.read"},
	} {
		err := RegisterConfiguredPlugins(context.Background(), plugin.NewManager(), tc.cfg)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("expected %q, got %v", tc.want, err)
		}
	}
}
