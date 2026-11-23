package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

func validateInstanceSettings(cfg InstanceConfig, p Plugin) error {
	if nilPlugin(p) {
		return errors.New("plugin is nil")
	}
	id, manifest, definitionErr := describe(p)
	if definitionErr != nil {
		return definitionErr
	}
	if !runtimeID.MatchString(cfg.ID) || cfg.PackageID != id || manifest.ID != id {
		return errors.New("instance and package identity are required")
	}
	if cfg.CallTimeout != "" {
		duration, err := time.ParseDuration(cfg.CallTimeout)
		if err != nil || duration <= 0 || duration > 5*time.Minute || cfg.Timeout != 0 {
			return errors.New("invalid instance timeout")
		}
	}
	if cfg.Timeout < 0 || cfg.Timeout > 5*time.Minute {
		return errors.New("instance timeout must be within five minutes")
	}
	if len(cfg.Configuration) > 1<<20 {
		return errors.New("plugin configuration exceeds limit")
	}
	if len(cfg.Configuration) > 0 {
		var object map[string]json.RawMessage
		if json.Unmarshal(cfg.Configuration, &object) != nil || object == nil {
			return errors.New("plugin configuration must be a JSON object")
		}
	}
	if len(cfg.SecretRefs) > 64 {
		return errors.New("too many secret references")
	}
	for name, ref := range cfg.SecretRefs {
		if !runtimeID.MatchString(name) || !runtimeID.MatchString(ref) {
			return errors.New("invalid secret reference")
		}
	}
	if err := ValidateRuntimeManifest(manifest); err != nil {
		return err
	}
	return nil
}
func validateInstance(cfg InstanceConfig, p Plugin) error {
	if err := validateInstanceSettings(cfg, p); err != nil {
		return err
	}
	_, manifest, err := describe(p)
	if err != nil {
		return err
	}
	for _, capability := range manifest.Capabilities {
		supported := false
		switch capability {
		case CapabilityActionPlan, CapabilityActionExecute:
			_, supported = p.(ActionPlugin)
		case CapabilityActionPreflight:
			_, supported = p.(PreflightActionPlugin)
		case CapabilityMetricsRead:
			_, supported = p.(MetricsPlugin)
		case CapabilityResourceDiscover:
			_, supported = p.(DiscoveryPlugin)
		case CapabilityCostRead:
			_, supported = p.(CostPlugin)
		case CapabilityRecommend:
			_, supported = p.(RecommenderPlugin)
		}
		if !supported {
			return ErrCapabilityMissing
		}
	}
	if cfg.Provider != "" && cfg.Provider != manifest.Runtime.Provider {
		return errors.New("instance provider disagrees with package")
	}
	return nil
}

// RegisterInstance reserves identity before initialization. Failed optional
// instances remain visible, but do not provide callable capabilities.
func (m *Manager) RegisterInstance(ctx context.Context, cfg InstanceConfig, p Plugin, secrets map[string]string) error {
	if err := validateInstance(cfg, p); err != nil {
		if nilPlugin(p) {
			return err
		}
		_, manifest, definitionErr := describe(p)
		if definitionErr != nil {
			return definitionErr
		}
		return m.retainIncompatible(ctx, cfg, p, manifest, err)
	}
	_, manifest, err := describe(p)
	if err != nil {
		return err
	}
	e := newEntry(p, manifest, cfg, m.journal)
	e.strict = true
	if err := m.reserve(e); err != nil {
		return err
	}
	defer func() { <-e.drainGate }()
	return m.activate(ctx, e, secrets, false)
}

type Route struct {
	InstanceID   string
	Capability   string
	VersionRange string
	ResourceType string
	Provider     string
	Account      string
	Location     string
	Operation    string
	Mode         string
}

func selected(values []string, value string) bool { return len(values) == 0 || Supports(values, value) }

// Resolve returns only a stable instance ID; typed selectors return managed
// handles. Explicit instance selection still enforces all routing constraints.
func (m *Manager) Resolve(route Route) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	id := ""
	for key, e := range m.instances {
		if route.InstanceID != "" && key != route.InstanceID {
			continue
		}
		state := e.snapshot().State
		if state != StateReady && state != StateDegraded {
			continue
		}
		cfg := e.config
		if cfg.Provider != "" && cfg.Provider != route.Provider || cfg.Account != "" && cfg.Account != route.Account || cfg.Location != "" && cfg.Location != route.Location {
			continue
		}
		if e.manifest.Runtime == nil {
			continue
		}
		for _, descriptor := range e.manifest.Runtime.Capabilities {
			if descriptor.Name != route.Capability || !selected(descriptor.ResourceTypes, route.ResourceType) || !selected(descriptor.Providers, route.Provider) || !selected(descriptor.Operations, route.Operation) || !selected(descriptor.Modes, route.Mode) {
				continue
			}
			if route.VersionRange != "" {
				ok, err := MatchVersionRange(descriptor.Version, route.VersionRange)
				if err != nil {
					return "", err
				}
				if !ok {
					continue
				}
			}
			if id != "" && id != key {
				return "", ErrAmbiguous
			}
			id = key
		}
	}
	if id == "" {
		return "", ErrNotFound
	}
	return id, nil
}

func (m *Manager) Shutdown(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	m.mu.Lock()
	m.shuttingDown = true
	for _, entry := range m.instances {
		entry.mu.Lock()
		if entry.status.State != StateStopped && entry.status.State != StateInstalled && entry.status.State != StateConfiguring {
			entry.status.State = StateDraining
		}
		entry.mu.Unlock()
	}
	m.mu.Unlock()
	var failures []error
	for _, status := range m.InstanceStatuses() {
		if err := m.Drain(ctx, status.Identity.InstanceID); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}
