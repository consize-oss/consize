package bootstrap

import (
	"fmt"
	"github.com/consize-oss/consize/pkg/evidence/quality"

	"github.com/consize-oss/consize/pkg/plugin"
)

// ValidateCapabilities rejects enabled workflows that cannot use their configured
// plugins. Disabled workflows may leave their bindings empty.
func ValidateCapabilities(m *plugin.Manager, cfg Config) error {
	if _, err := quality.Resolve(cfg.Recommender.Quality, quality.Recommendation, false, cfg.Recommender.ConfidenceProfile == "lab"); err != nil {
		return fmt.Errorf("recommender.quality: %w", err)
	}
	if _, err := quality.Resolve(cfg.Verification.Quality, quality.Verification, false, false); err != nil {
		return fmt.Errorf("verification.quality: %w", err)
	}
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
				for _, status := range m.InstanceStatuses() {
					if status.Identity.InstanceID == binding.id && (status.State == plugin.StateReady || status.State == plugin.StateDegraded) {
						found = true
					}
				}
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
	for _, status := range m.InstanceStatuses() {
		if status.State != plugin.StateReady && status.State != plugin.StateDegraded {
			out = append(out, fmt.Sprintf("Plugin instance %s is %s; its capabilities are unavailable.", status.Identity.InstanceID, status.State))
		}
	}
	return out
}
