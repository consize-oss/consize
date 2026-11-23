package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"time"
)

// PackageFactory is registered only by the host composition root. Configure
// validates local settings and returns an unshared configured provider instance.
// It must not start background work before Initialize is called.
type PackageFactory struct {
	Manifest  Manifest
	Configure func(context.Context, json.RawMessage) (Plugin, error)
}
type packagePlaceholder struct{ manifest Manifest }

func (p *packagePlaceholder) ID() string         { return p.manifest.ID }
func (p *packagePlaceholder) Manifest() Manifest { return p.manifest }
func (*packagePlaceholder) Health(context.Context) Health {
	return Health{Status: "unavailable", Message: "Package has not initialized"}
}

func (m *Manager) RegisterPackage(factory PackageFactory) error {
	if err := ValidateRuntimeManifest(factory.Manifest); err != nil {
		var failure *RuntimeError
		if !errors.As(err, &failure) || failure.Code != FailureIncompatible {
			return err
		}
	}
	if factory.Configure == nil {
		return errors.New("package factory is required")
	}
	factory.Manifest = copyManifest(factory.Manifest)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.shuttingDown {
		return &RuntimeError{Code: FailureUnavailable}
	}
	if m.packages == nil {
		m.packages = map[string]PackageFactory{}
	}
	if _, ok := m.packages[factory.Manifest.ID]; ok {
		return errors.New("package already installed")
	}
	m.packages[factory.Manifest.ID] = factory
	return nil
}

func (m *Manager) Install(ctx context.Context, cfg InstanceConfig, secrets map[string]string) error {
	m.mu.RLock()
	factory, ok := m.packages[cfg.PackageID]
	m.mu.RUnlock()
	if !ok {
		return ErrNotFound
	}
	placeholder := &packagePlaceholder{manifest: factory.Manifest}
	if err := validateInstanceSettings(cfg, placeholder); err != nil {
		return m.retainIncompatible(ctx, cfg, placeholder, factory.Manifest, err)
	}
	e := newEntry(placeholder, copyManifest(factory.Manifest), cfg, m.journal)
	e.strict = true
	if err := m.reserve(e); err != nil {
		return err
	}
	defer func() { <-e.drainGate }()
	if cfg.Disabled {
		e.setState(StateDisabled)
		return e.persist(ctx)
	}
	e.setState(StateConfiguring)
	p, err := run(ctx, e, modeLifecycle, func(ctx context.Context) (Plugin, error) {
		return factory.Configure(ctx, append(json.RawMessage(nil), cfg.Configuration...))
	})
	if err != nil {
		return m.activationFailure(ctx, e, err)
	}
	if nilPlugin(p) {
		return m.activationFailure(ctx, e, invalidOutput())
	}
	id, manifest, definitionErr := describe(p)
	if definitionErr != nil {
		return m.activationFailure(ctx, e, definitionErr)
	}
	if id != cfg.PackageID || !reflect.DeepEqual(manifest, factory.Manifest) {
		return m.activationFailure(ctx, e, invalidOutput())
	}
	if err := validateInstance(cfg, p); err != nil {
		return m.activationFailure(ctx, e, &RuntimeError{Code: FailureInvalidRequest})
	}
	m.mu.Lock()
	for _, other := range m.instances {
		if other != e && samePluginPointer(other.raw, p) {
			m.mu.Unlock()
			return m.activationFailure(ctx, e, &RuntimeError{Code: FailureInvalidRequest})
		}
	}
	e.raw = p
	m.mu.Unlock()
	return m.activate(ctx, e, secrets, true)
}

func (m *Manager) reserve(e *runtimeEntry) error {
	e.drainGate <- struct{}{}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.shuttingDown {
		<-e.drainGate
		return &RuntimeError{Code: FailureUnavailable}
	}
	if len(m.instances) >= 128 {
		<-e.drainGate
		return errors.New("plugin instance limit reached")
	}
	if m.instances[e.config.ID] != nil {
		<-e.drainGate
		return errors.New("instance id already registered")
	}
	for _, other := range m.instances {
		if samePluginPointer(other.raw, e.raw) {
			<-e.drainGate
			return errors.New("configured instances must not share a mutable provider")
		}
	}
	m.restore(e)
	m.instances[e.config.ID] = e
	return nil
}
func (m *Manager) activationFailure(ctx context.Context, e *runtimeEntry, err error) error {
	e.mu.Lock()
	e.status.State = StateFailed
	var failure *RuntimeError
	if errors.As(err, &failure) {
		e.status.Failure = safeFailureCode(failure.Code)
	} else {
		e.status.Failure = FailureInternal
	}
	e.mu.Unlock()
	if persistErr := e.persist(ctx); persistErr != nil {
		return &RuntimeError{Code: FailureInternal}
	}
	if e.config.Required {
		return err
	}
	return nil
}

func (m *Manager) activate(ctx context.Context, e *runtimeEntry, secrets map[string]string, prepared bool) error {
	cfg := e.config
	p := e.raw
	if cfg.Disabled {
		e.setState(StateDisabled)
		return e.persist(ctx)
	}
	e.setState(StateConfiguring)
	if err := captureRuntimeMetadata(ctx, e); err != nil {
		return m.activationFailure(ctx, e, err)
	}
	m.mu.Lock()
	m.restore(e)
	m.mu.Unlock()
	if validator, ok := p.(ConfigValidator); ok {
		if _, err := run(ctx, e, modeLifecycle, func(ctx context.Context) (struct{}, error) {
			return struct{}{}, validator.ValidateConfiguration(ctx, append(json.RawMessage(nil), cfg.Configuration...))
		}); err != nil {
			return m.activationFailure(ctx, e, err)
		}
	} else if !prepared && len(cfg.Configuration) > 0 {
		return m.activationFailure(ctx, e, &RuntimeError{Code: FailureInvalidRequest})
	}
	resolved := map[string]string{}
	for name, ref := range cfg.SecretRefs {
		value, ok := secrets[ref]
		if !ok {
			return m.activationFailure(ctx, e, &RuntimeError{Code: FailureInvalidRequest})
		}
		resolved[name] = value
	}
	if initializer, ok := p.(Initializer); ok {
		if _, err := run(ctx, e, modeLifecycle, func(ctx context.Context) (struct{}, error) {
			return struct{}{}, initializer.Initialize(ctx, append(json.RawMessage(nil), cfg.Configuration...), resolved)
		}); err != nil {
			return m.activationFailure(ctx, e, err)
		}
	} else if len(resolved) > 0 {
		return m.activationFailure(ctx, e, &RuntimeError{Code: FailureInvalidRequest})
	}

	if declarer, ok := p.(ScopeDeclarer); ok {
		scope, err := run(ctx, e, modeLifecycle, func(context.Context) (ResourceScope, error) { return declarer.InstanceScope(), nil })
		if err != nil {
			return m.activationFailure(ctx, e, err)
		}
		if cfg.Provider != "" && cfg.Provider != scope.Provider || cfg.Account != "" && cfg.Account != scope.Account || cfg.Location != "" && cfg.Location != scope.Location {
			return m.activationFailure(ctx, e, &RuntimeError{Code: FailureInvalidRequest})
		}
	}
	if cfg.Required {
		if _, err := run(ctx, e, modeLifecycle, func(ctx context.Context) (struct{}, error) {
			h := p.Health(ctx)
			if h.Status != "healthy" && h.Status != "ready" && h.Status != "online" && h.Status != "ok" {
				return struct{}{}, &RuntimeError{Code: FailureUnavailable}
			}
			return struct{}{}, nil
		}); err != nil {
			return m.activationFailure(ctx, e, err)
		}
	}
	m.mu.Lock()
	if m.shuttingDown {
		m.mu.Unlock()
		return m.activationFailure(ctx, e, &RuntimeError{Code: FailureUnavailable})
	}
	for _, capability := range e.manifest.Capabilities {
		switch capability {
		case CapabilityActionPlan, CapabilityMetricsRead, CapabilityResourceDiscover, CapabilityCostRead, CapabilityRecommend:
			if err := m.bind(e, capability); err != nil {
				m.mu.Unlock()
				return m.activationFailure(ctx, e, err)
			}
		}
	}
	e.setState(StateReady)
	m.mu.Unlock()
	if err := e.persist(ctx); err != nil {
		e.setState(StateFailed)
		return &RuntimeError{Code: FailureInternal}
	}
	return nil
}

// Reload is deliberately additive. Existing instances are immutable, including
// when idle: a durable workflow may still refer to an approved plan. Changing or
// removing one requires a controlled restart, never a live replacement.
func (m *Manager) Reload(ctx context.Context, configs []InstanceConfig, secrets map[string]string) error {
	seen := map[string]bool{}
	m.mu.RLock()
	for _, cfg := range configs {
		if seen[cfg.ID] {
			m.mu.RUnlock()
			return errors.New("duplicate instance in reload")
		}
		seen[cfg.ID] = true
		if existing := m.instances[cfg.ID]; existing != nil {
			oldRaw, _ := json.Marshal(existing.config)
			newRaw, _ := json.Marshal(cfg)
			if !reflect.DeepEqual(oldRaw, newRaw) || existing.config.Timeout != cfg.Timeout {
				m.mu.RUnlock()
				return errors.New("existing instance configuration is immutable; restart explicitly")
			}
		}
	}
	for id := range m.instances {
		if !seen[id] {
			m.mu.RUnlock()
			return errors.New("reload cannot remove an existing instance")
		}
	}
	m.mu.RUnlock()
	for _, cfg := range configs {
		m.mu.RLock()
		existing := m.instances[cfg.ID]
		m.mu.RUnlock()
		if existing == nil {
			if err := m.Install(ctx, cfg, secrets); err != nil {
				return err
			}
		}
	}
	return nil
}

func samePluginPointer(a, b Plugin) bool {
	return a != nil && b != nil && reflect.TypeOf(a) == reflect.TypeOf(b) && reflect.ValueOf(a).Kind() == reflect.Ptr && a == b
}

func (m *Manager) retainIncompatible(ctx context.Context, cfg InstanceConfig, p Plugin, manifest Manifest, err error) error {
	var failure *RuntimeError
	if !errors.As(err, &failure) || failure.Code != FailureIncompatible {
		return err
	}
	e := newEntry(p, copyManifest(manifest), cfg, m.journal)
	e.strict = true
	e.status.State = StateIncompatible
	e.status.Failure = FailureIncompatible
	now := time.Now().UTC()
	e.status.LastFailure = &now
	if reserveErr := m.reserve(e); reserveErr != nil {
		return reserveErr
	}
	defer func() { <-e.drainGate }()
	if persistErr := e.persist(ctx); persistErr != nil {
		return &RuntimeError{Code: FailureInternal}
	}
	if cfg.Required {
		return &RuntimeError{Code: FailureIncompatible}
	}
	return nil
}
