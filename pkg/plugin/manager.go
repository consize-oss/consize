package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"sync"
	"time"
)

var (
	ErrNotFound          = errors.New("plugin not found")
	ErrCapabilityMissing = errors.New("plugin capability missing")
	ErrUnsupported       = errors.New("plugin does not support requested resource or action")
	ErrAmbiguous         = errors.New("multiple plugin instances match; configure an explicit instance")
)

type Manager struct {
	shuttingDown bool
	mu           sync.RWMutex
	packages     map[string]PackageFactory
	recommenders map[string]RecommenderPlugin
	actions      map[string]ActionPlugin
	metrics      map[string]MetricsPlugin
	costs        map[string]CostPlugin
	discovery    map[string]DiscoveryPlugin
	instances    map[string]*runtimeEntry
	journal      RuntimeJournal
	prior        map[string]InstanceStatus
}

func NewManager() *Manager {
	return &Manager{recommenders: map[string]RecommenderPlugin{}, actions: map[string]ActionPlugin{}, metrics: map[string]MetricsPlugin{}, costs: map[string]CostPlugin{}, discovery: map[string]DiscoveryPlugin{}, instances: map[string]*runtimeEntry{}}
}

// SetJournal must precede registration, so no initial state can be lost.
func (m *Manager) SetJournal(ctx context.Context, j RuntimeJournal) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	m.mu.Lock()
	defer m.mu.Unlock()
	if j == nil {
		return errors.New("runtime journal is nil")
	}
	if len(m.instances) != 0 {
		return errors.New("journal must be configured before registration")
	}
	prior, err := j.LoadPluginStatuses(ctx)
	if err != nil {
		return err
	}
	m.prior = map[string]InstanceStatus{}
	for _, status := range prior {
		m.prior[status.Identity.InstanceID] = status
	}
	m.journal = j
	return nil
}

func copyManifest(v Manifest) Manifest {
	raw, _ := json.Marshal(v)
	var out Manifest
	_ = json.Unmarshal(raw, &out)
	return out
}
func nilPlugin(p Plugin) bool {
	return p == nil || (reflect.ValueOf(p).Kind() == reflect.Ptr && reflect.ValueOf(p).IsNil())
}

// legacyRegister permits the same pointer to expose several intentional
// capabilities, but never permits two packages to hide behind one ID.
func (m *Manager) legacyRegister(p Plugin, capability string) (resultErr error) {
	if nilPlugin(p) {
		return errors.New("plugin is nil")
	}
	id, manifest, definitionErr := describe(p)
	if definitionErr != nil {
		return definitionErr
	}
	if manifest.Runtime != nil {
		if err := ValidateRuntimeManifest(manifest); err != nil {
			return err
		}
	}
	if !runtimeID.MatchString(manifest.ID) || manifest.ID != id {
		return errors.New("plugin identity mismatch")
	}
	switch capability {
	case CapabilityActionPlan:
		if manifest.Category != CategoryAction {
			return errors.New("action category required")
		}
	case CapabilityCostRead:
		if manifest.Category != CategoryCost || manifest.CanMutateInfrastructure {
			return errors.New("read-only cost category required")
		}
	case CapabilityMetricsRead:
		if manifest.CanMutateInfrastructure || (manifest.Category != CategoryMetrics && manifest.Category != CategoryDataSource && manifest.Category != CategoryCost) {
			return errors.New("read-oriented metrics category required")
		}
	}
	if !Supports(manifest.Capabilities, capability) {
		return ErrCapabilityMissing
	}
	if manifest.CanMutateInfrastructure && !Supports(manifest.Capabilities, CapabilityActionExecute) {
		return errors.New("mutating plugin requires action.execute")
	}
	candidate := newEntry(p, manifest, InstanceConfig{ID: manifest.ID, PackageID: manifest.ID}, nil)
	if err := captureRuntimeMetadata(context.Background(), candidate); err != nil {
		return err
	}
	m.mu.Lock()
	var e *runtimeEntry
	defer func() {
		m.mu.Unlock()
		if resultErr == nil && e != nil {
			resultErr = e.persist(context.Background())
		}
	}()
	if m.shuttingDown {
		return &RuntimeError{Code: FailureUnavailable}
	}
	e = m.instances[manifest.ID]
	if e != nil {
		if !samePluginPointer(e.raw, p) || !reflect.DeepEqual(e.manifest, manifest) {
			return errors.New("conflicting plugin identity")
		}
	} else {
		e = candidate
		e.journal = m.journal
		m.restore(e)
		e.status.State = StateReady
	}
	if err := m.bind(e, capability); err != nil {
		return err
	}
	m.instances[manifest.ID] = e
	return nil
}
func (m *Manager) bind(e *runtimeEntry, capability string) error {
	id := e.config.ID
	switch capability {
	case CapabilityRecommend:
		if _, ok := m.recommenders[id]; ok {
			return errors.New("recommender capability already registered")
		}
		p, ok := e.raw.(RecommenderPlugin)
		if !ok {
			return ErrCapabilityMissing
		}
		m.recommenders[id] = &managedRecommender{managedPlugin{e}, p}
	case CapabilityActionPlan:
		if _, ok := m.actions[id]; ok {
			return errors.New("action capability already registered")
		}
		p, ok := e.raw.(ActionPlugin)
		if !ok {
			return ErrCapabilityMissing
		}
		m.actions[id] = wrapAction(e, p)
	case CapabilityMetricsRead:
		if _, ok := m.metrics[id]; ok {
			return errors.New("metrics capability already registered")
		}
		p, ok := e.raw.(MetricsPlugin)
		if !ok {
			return ErrCapabilityMissing
		}
		m.metrics[id] = wrapMetrics(e, p)
	case CapabilityCostRead:
		if _, ok := m.costs[id]; ok {
			return errors.New("cost capability already registered")
		}
		p, ok := e.raw.(CostPlugin)
		if !ok {
			return ErrCapabilityMissing
		}
		m.costs[id] = &managedCost{managedPlugin{e}, p}
	case CapabilityResourceDiscover:
		if _, ok := m.discovery[id]; ok {
			return errors.New("discovery capability already registered")
		}
		p, ok := e.raw.(DiscoveryPlugin)
		if !ok {
			return ErrCapabilityMissing
		}
		m.discovery[id] = wrapDiscovery(e, p)
	default:
		return ErrCapabilityMissing
	}
	return nil
}
func (m *Manager) RegisterAction(p ActionPlugin) error {
	return m.legacyRegister(p, CapabilityActionPlan)
}
func (m *Manager) RegisterMetrics(p MetricsPlugin) error {
	return m.legacyRegister(p, CapabilityMetricsRead)
}
func (m *Manager) RegisterCost(p CostPlugin) error { return m.legacyRegister(p, CapabilityCostRead) }
func (m *Manager) RegisterDiscovery(p DiscoveryPlugin) error {
	return m.legacyRegister(p, CapabilityResourceDiscover)
}

func (m *Manager) ActionPlugin(id, resourceType, actionType string) (ActionPlugin, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p := m.actions[id]
	if p == nil {
		return nil, ErrNotFound
	}
	v := p.Manifest()
	if !Supports(v.SupportedResourceTypes, resourceType) || !Supports(v.SupportedActionTypes, actionType) {
		return nil, ErrUnsupported
	}
	return p, nil
}
func (m *Manager) MetricsPlugin(id, resourceType string) (MetricsPlugin, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p := m.metrics[id]
	if p == nil {
		return nil, ErrNotFound
	}
	if !Supports(p.Manifest().SupportedResourceTypes, resourceType) {
		return nil, ErrUnsupported
	}
	return p, nil
}
func (m *Manager) CostPlugin(id, resourceType string) (CostPlugin, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p := m.costs[id]
	if p == nil {
		return nil, ErrNotFound
	}
	if !Supports(p.Manifest().SupportedResourceTypes, resourceType) {
		return nil, ErrUnsupported
	}
	return p, nil
}
func (m *Manager) DiscoveryPlugins() []DiscoveryPlugin {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]DiscoveryPlugin, 0, len(m.discovery))
	for _, p := range m.discovery {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID() < out[j].ID() })
	return out
}
func (m *Manager) MetricsPlugins(resourceType string) []MetricsPlugin {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]MetricsPlugin, 0, len(m.metrics))
	for _, p := range m.metrics {
		if Supports(p.Manifest().SupportedResourceTypes, resourceType) {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID() < out[j].ID() })
	return out
}
func (m *Manager) Manifests() []Manifest {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Manifest, 0, len(m.instances))
	for _, e := range m.instances {
		v := copyManifest(e.manifest)
		v.ID = e.config.ID
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
func (m *Manager) Health(ctx context.Context, id string) (Health, error) {
	m.mu.RLock()
	e := m.instances[id]
	m.mu.RUnlock()
	if e == nil {
		return Health{}, ErrNotFound
	}
	return managedPlugin{e}.health(ctx)
}
func sortStatuses(s []InstanceStatus) {
	sort.Slice(s, func(i, j int) bool { return s[i].Identity.InstanceID < s[j].Identity.InstanceID })
}

func (m *Manager) restore(e *runtimeEntry) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if prior, ok := m.prior[e.config.ID]; ok && prior.Identity == e.status.Identity {
		e.status.LastSuccess = prior.LastSuccess
		e.status.LastFailure = prior.LastFailure
		e.status.LastHealth = prior.LastHealth
		e.status.Failure = prior.Failure
		if prior.ProviderFailure != nil {
			v := *prior.ProviderFailure
			e.status.ProviderFailure = &v
		}
	}
}

func (m *Manager) InstanceIdentity(id string) (Provenance, error) {
	m.mu.RLock()
	entry := m.instances[id]
	m.mu.RUnlock()
	if entry == nil {
		return Provenance{}, ErrNotFound
	}
	return entry.snapshot().Identity, nil
}
