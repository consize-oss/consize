package bootstrap

import (
	"fmt"

	"github.com/consize-oss/consize/pkg/plugin"
)

// ValidateCapabilities rejects enabled workflows that cannot use their configured
// plugins. Disabled workflows may leave their bindings empty.
func ValidateCapabilities(m *plugin.Manager, cfg Config) error {
	bindings := []struct {
		enabled               bool
		field, id, capability string
	}{
		{cfg.Recommender.Enabled, "recommender.metrics_plugin_id", cfg.Recommender.MetricsPluginID, plugin.CapabilityMetricsRead},
		{cfg.Recommender.Enabled, "recommender.action_plugin_id", cfg.Recommender.ActionPluginID, plugin.CapabilityActionPlan},
		{cfg.Verification.Enabled, "verification.metrics_plugin_id", cfg.Verification.MetricsPluginID, plugin.CapabilityMetricsRead},
	}
	for _, binding := range bindings {
		if !binding.enabled {
			continue
		}
		if binding.id == "" {
			return fmt.Errorf("%s is required when enabled", binding.field)
		}
		found := false
		for _, manifest := range m.Manifests() {
			if manifest.ID == binding.id && plugin.Supports(manifest.Capabilities, binding.capability) {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("%s %q requires a registered %s capability", binding.field, binding.id, binding.capability)
		}
	}
	return nil
}

// CapabilityDiagnostics describes setup gaps without inventing provider health.
func CapabilityDiagnostics(m *plugin.Manager, cfg Config) []string {
	out := []string{}
	if len(m.Manifests()) == 0 {
		out = append(out, "No plugins configured; register capabilities to collect evidence or change infrastructure.")
	}
	if len(m.DiscoveryPlugins()) == 0 {
		out = append(out, "No discovery capability configured; inventory requires explicit resource registration.")
	}
	if !cfg.Recommender.Enabled {
		out = append(out, "Recommendation generation is disabled.")
	}
	if !cfg.Verification.Enabled {
		out = append(out, "Verification is disabled; automated execution is unavailable.")
	}
	return out
}
