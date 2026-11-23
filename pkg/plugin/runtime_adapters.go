package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/consize-oss/consize/pkg/evidence"
	"github.com/consize-oss/consize/pkg/resource"
)

type managedPlugin struct{ e *runtimeEntry }

func (p managedPlugin) ID() string         { return p.e.config.ID }
func (p managedPlugin) Manifest() Manifest { v := copyManifest(p.e.manifest); v.ID = p.ID(); return v }
func (p managedPlugin) Health(ctx context.Context) Health {
	h, err := p.health(ctx)
	if err != nil {
		return Health{Status: "unavailable", Message: "Plugin health check failed", CheckedAt: time.Now().UTC()}
	}
	return h
}
func (p managedPlugin) health(ctx context.Context) (Health, error) {
	h, err := run(ctx, p.e, modeProbe, func(ctx context.Context) (Health, error) {
		h := p.e.raw.Health(ctx)
		switch h.Status {
		case "healthy", "ready", "online", "ok":
			h.Status = "healthy"
		case "degraded":
		case "unhealthy", "unavailable", "offline", "unknown":
			h.Status = "unavailable"
		default:
			return Health{}, invalidOutput()
		}
		if h.Ready != nil && !*h.Ready {
			h.Status = "unavailable"
		}
		now := time.Now().UTC()
		p.e.mu.Lock()
		p.e.status.LastHealth = &now
		if p.e.status.State == StateReady || p.e.status.State == StateDegraded || p.e.status.State == StateUnavailable {
			switch h.Status {
			case "healthy":
				p.e.status.State = StateReady
			case "degraded":
				p.e.status.State = StateDegraded
			case "unavailable":
				p.e.status.State = StateUnavailable
			}
		}
		p.e.mu.Unlock()
		if h.Status == "unavailable" {
			return h, &RuntimeError{Code: FailureUnavailable}
		}
		return h, nil
	})
	now := time.Now().UTC()
	if err != nil {
		return Health{Status: "unavailable", Message: "Plugin health check unavailable", Details: map[string]any{"runtime": p.e.snapshot()}, CheckedAt: now}, nil
	}
	h.Message = "Plugin health: " + h.Status
	h.Details = map[string]any{"runtime": p.e.snapshot()}
	h.CheckedAt = now
	return h, nil
}

func (p managedPlugin) accept(res resource.Resource) error {
	c := p.e.config
	if res.ID == "" || !Supports(p.e.manifest.SupportedResourceTypes, res.Type) {
		return &RuntimeError{Code: FailureInvalidRequest}
	}
	if (c.Provider != "" && c.Provider != res.Provider) || (c.Account != "" && c.Account != res.Account) || (c.Location != "" && c.Location != res.Region) {
		return &RuntimeError{Code: FailureUnsupported}
	}
	return nil
}
func freeze[T any](v T) (T, error) {
	var out T
	raw, err := json.Marshal(v)
	if err != nil || len(raw) > 16<<20 {
		return out, &RuntimeError{Code: FailureInvalidRequest}
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err = decoder.Decode(&out); err != nil {
		return out, &RuntimeError{Code: FailureInvalidRequest}
	}
	return out, nil
}
func invalidOutput() error { return &RuntimeError{Code: FailureInvalidResponse} }

type managedAction struct {
	managedPlugin
	raw ActionPlugin
}

func (p *managedAction) Plan(ctx context.Context, input ActionInput) (ActionPlan, error) {
	if err := p.acceptCapability(CapabilityActionPlan, input.Resource, input.ActionType, input.Mode); err != nil {
		return ActionPlan{}, err
	}
	if !Supports(p.e.manifest.SupportedActionTypes, input.ActionType) {
		return ActionPlan{}, &RuntimeError{Code: FailureUnsupported}
	}
	input, err := freeze(input)
	if err != nil {
		return ActionPlan{}, err
	}
	return run(ctx, p.e, modeInvoke, func(ctx context.Context) (ActionPlan, error) {
		plan, err := p.raw.Plan(ctx, input)
		if err != nil {
			return plan, err
		}
		if plan.PluginID != p.e.manifest.ID || plan.ResourceID != input.Resource.ID || plan.ActionType != input.ActionType {
			return ActionPlan{}, invalidOutput()
		}
		plan.RequiresApproval = plan.RequiresApproval || p.e.manifest.RequiresApproval
		plan.PluginID = p.ID()
		plan.PluginVersion = p.e.manifest.Version
		identity := p.e.status.Identity
		plan.Runtime = &identity
		plan.ResourceScope = &ResourceScope{Provider: input.Resource.Provider, Account: input.Resource.Account, Location: input.Resource.Region, ResourceID: input.Resource.ID}
		return freezeOutput(plan)
	})
}
func (p *managedAction) checkPlan(plan ActionPlan) error {
	if p.e.strict && (plan.ResourceScope == nil || plan.ResourceScope.ResourceID != plan.ResourceID) {
		return &RuntimeError{Code: FailureInvalidRequest}
	}
	if scope := plan.ResourceScope; scope != nil {
		cfg := p.e.config
		if cfg.Provider != "" && cfg.Provider != scope.Provider || cfg.Account != "" && cfg.Account != scope.Account || cfg.Location != "" && cfg.Location != scope.Location {
			return &RuntimeError{Code: FailureUnsupported}
		}
	}
	if plan.PluginID != p.ID() || !Supports(p.e.manifest.SupportedActionTypes, plan.ActionType) || plan.ResourceID == "" {
		return &RuntimeError{Code: FailureInvalidRequest}
	}
	if (p.e.strict || plan.Runtime != nil) && (plan.Runtime == nil || *plan.Runtime != p.e.status.Identity || plan.PluginVersion != p.e.manifest.Version) {
		return &RuntimeError{Code: FailureIncompatible}
	}
	return nil
}
func (p *managedAction) providerPlan(plan ActionPlan) ActionPlan {
	copied, err := freeze(plan)
	if err != nil {
		return ActionPlan{}
	}
	copied.PluginID = p.e.manifest.ID
	return copied
}
func (p *managedAction) result(plan ActionPlan, r ActionResult, err error) (ActionResult, error) {
	if err != nil {
		return r, err
	}
	if r.PluginID != p.e.manifest.ID || r.ResourceID != plan.ResourceID || r.ActionType != plan.ActionType {
		return ActionResult{}, invalidOutput()
	}
	r.PluginID = p.ID()
	identity := p.e.status.Identity
	r.Runtime = &identity
	return freezeOutput(r)
}
func (p *managedAction) Execute(ctx context.Context, plan ActionPlan) (ActionResult, error) {
	if !Supports(p.e.manifest.Capabilities, CapabilityActionExecute) {
		return ActionResult{}, &RuntimeError{Code: FailureUnsupported}
	}
	if err := p.checkPlan(plan); err != nil {
		return ActionResult{}, err
	}
	plan, err := freeze(plan)
	if err != nil {
		return ActionResult{}, err
	}
	return run(ctx, p.e, modeInvoke, func(ctx context.Context) (ActionResult, error) {
		r, err := p.raw.Execute(ctx, p.providerPlan(plan))
		return p.result(plan, r, err)
	})
}
func (p *managedAction) preflight(ctx context.Context, plan ActionPlan) ([]PreflightCheck, error) {
	if err := p.checkPlan(plan); err != nil {
		return nil, err
	}
	return run(ctx, p.e, modeInvoke, func(ctx context.Context) ([]PreflightCheck, error) {
		return p.raw.(PreflightActionPlugin).Preflight(ctx, p.providerPlan(plan))
	})
}
func (p *managedAction) inspect(ctx context.Context, plan ActionPlan) (string, error) {
	if err := p.checkPlan(plan); err != nil {
		return "", err
	}
	return run(ctx, p.e, modeInvoke, func(ctx context.Context) (string, error) {
		state, err := p.raw.(RecoverableActionPlugin).Inspect(ctx, p.providerPlan(plan))
		if err == nil && state != StateOriginal && state != StateApplied && state != StateDrifted {
			return "", invalidOutput()
		}
		return state, err
	})
}
func (p *managedAction) rollback(ctx context.Context, plan ActionPlan) (ActionResult, error) {
	if !Supports(p.e.manifest.Capabilities, CapabilityActionExecute) {
		return ActionResult{}, &RuntimeError{Code: FailureUnsupported}
	}
	if err := p.checkPlan(plan); err != nil {
		return ActionResult{}, err
	}
	return run(ctx, p.e, modeInvoke, func(ctx context.Context) (ActionResult, error) {
		r, err := p.raw.(RecoverableActionPlugin).Rollback(ctx, p.providerPlan(plan))
		return p.result(plan, r, err)
	})
}
func (p *managedAction) ready(ctx context.Context, plan ActionPlan) error {
	if err := p.checkPlan(plan); err != nil {
		return err
	}
	_, err := run(ctx, p.e, modeInvoke, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, p.raw.(RecoverableActionPlugin).Ready(ctx, p.providerPlan(plan))
	})
	return err
}

type managedMetrics struct {
	managedPlugin
	raw MetricsPlugin
}

func (p *managedMetrics) read(ctx context.Context, res resource.Resource, read func(context.Context, resource.Resource) (MetricsSnapshot, error)) (MetricsSnapshot, error) {
	if err := p.acceptCapability(CapabilityMetricsRead, res, "", ""); err != nil {
		return MetricsSnapshot{}, err
	}
	res, err := freeze(res)
	if err != nil {
		return MetricsSnapshot{}, err
	}
	return run(ctx, p.e, modeInvoke, func(ctx context.Context) (MetricsSnapshot, error) {
		s, err := read(ctx, res)
		if err != nil {
			return s, err
		}
		if p.e.strict && (s.PluginID != p.e.manifest.ID || s.ResourceID != res.ID || s.ValidateRecords() != nil) {
			return MetricsSnapshot{}, invalidOutput()
		}
		if len(s.Records) > 0 {
			var bindErr error
			s.Records, bindErr = p.bindEvidence(s.Records)
			if bindErr != nil {
				return MetricsSnapshot{}, bindErr
			}
		}
		s.PluginID = p.ID()
		identity := p.e.status.Identity
		s.Runtime = &identity
		return freezeOutput(s)
	})
}
func (p *managedMetrics) ReadMetrics(ctx context.Context, res resource.Resource) (MetricsSnapshot, error) {
	return p.read(ctx, res, p.raw.ReadMetrics)
}

type managedWindowed struct{ *managedMetrics }

func (p *managedWindowed) ReadMetricsBetween(ctx context.Context, res resource.Resource, start, end time.Time) (MetricsSnapshot, error) {
	if !start.Before(end) {
		return MetricsSnapshot{}, &RuntimeError{Code: FailureInvalidRequest}
	}
	return p.read(ctx, res, func(ctx context.Context, res resource.Resource) (MetricsSnapshot, error) {
		return p.raw.(WindowedMetricsPlugin).ReadMetricsBetween(ctx, res, start, end)
	})
}
func wrapMetrics(e *runtimeEntry, p MetricsPlugin) MetricsPlugin {
	base := &managedMetrics{managedPlugin{e}, p}
	if _, ok := p.(WindowedMetricsPlugin); ok {
		return &managedWindowed{base}
	}
	return base
}

type managedCost struct {
	managedPlugin
	raw CostPlugin
}

func (p *managedCost) Estimate(ctx context.Context, res resource.Resource, proposed map[string]any) (CostEstimate, error) {
	if err := p.acceptCapability(CapabilityCostRead, res, "", ""); err != nil {
		return CostEstimate{}, err
	}
	var err error
	res, err = freeze(res)
	if err != nil {
		return CostEstimate{}, err
	}
	proposed, err = freeze(proposed)
	if err != nil {
		return CostEstimate{}, err
	}
	return run(ctx, p.e, modeInvoke, func(ctx context.Context) (CostEstimate, error) {
		v, err := p.raw.Estimate(ctx, res, proposed)
		if err != nil {
			return v, err
		}
		if v.PluginID != p.e.manifest.ID || v.ResourceID != res.ID {
			return CostEstimate{}, invalidOutput()
		}
		v.PluginID = p.ID()
		identity := p.e.status.Identity
		v.Runtime = &identity
		return freezeOutput(v)
	})
}

type managedDiscovery struct {
	managedPlugin
	raw DiscoveryPlugin
}

func (p *managedDiscovery) observations(v []ProviderObservation) ([]ProviderObservation, error) {
	for i := range v {
		candidate := v[i].Resource
		if candidate.ID == "" {
			candidate.ID = "pending-normalization"
		}
		if v[i].PluginID != p.e.manifest.ID || p.acceptCapability(CapabilityResourceDiscover, candidate, "discover", "") != nil {
			return nil, invalidOutput()
		}
		v[i].PluginID = p.ID()
		identity := p.e.status.Identity
		v[i].Runtime = &identity
		v[i].Resource.Producer = &identity
	}
	return freezeOutput(v)
}
func (p *managedDiscovery) Discover(ctx context.Context) ([]ProviderObservation, error) {
	return run(ctx, p.e, modeInvoke, func(ctx context.Context) ([]ProviderObservation, error) {
		v, err := p.raw.Discover(ctx)
		if err != nil {
			return nil, err
		}
		return p.observations(v)
	})
}

type managedSnapshot struct{ *managedDiscovery }

func (p *managedSnapshot) DiscoverSnapshot(ctx context.Context) (DiscoverySnapshot, error) {
	return run(ctx, p.e, modeInvoke, func(ctx context.Context) (DiscoverySnapshot, error) {
		v, err := p.raw.(SnapshotDiscoveryPlugin).DiscoverSnapshot(ctx)
		if err != nil {
			return v, err
		}
		if v.Scan.PluginID != p.e.manifest.ID {
			return DiscoverySnapshot{}, invalidOutput()
		}
		v.Observations, err = p.observations(v.Observations)
		v.Scan.PluginID = p.ID()
		return v, err
	})
}

type managedScoped struct {
	*managedSnapshot
	scope map[string]string
}

func (p *managedScoped) DiscoveryScope() map[string]string {
	out := map[string]string{}
	for k, v := range p.scope {
		out[k] = v
	}
	return out
}
func wrapDiscovery(e *runtimeEntry, p DiscoveryPlugin) DiscoveryPlugin {
	base := &managedDiscovery{managedPlugin{e}, p}
	if _, ok := p.(ScopedDiscoveryPlugin); ok {
		scope := map[string]string{}
		for k, v := range e.scope {
			scope[k] = v
		}
		return &managedScoped{&managedSnapshot{base}, scope}
	}
	if _, ok := p.(SnapshotDiscoveryPlugin); ok {
		return &managedSnapshot{base}
	}
	return base
}

type actionPreflight struct{ base *managedAction }

func (p actionPreflight) Preflight(ctx context.Context, v ActionPlan) ([]PreflightCheck, error) {
	return p.base.preflight(ctx, v)
}

type actionRecovery struct{ base *managedAction }

func (p actionRecovery) Inspect(ctx context.Context, v ActionPlan) (string, error) {
	return p.base.inspect(ctx, v)
}
func (p actionRecovery) Rollback(ctx context.Context, v ActionPlan) (ActionResult, error) {
	return p.base.rollback(ctx, v)
}
func (p actionRecovery) Ready(ctx context.Context, v ActionPlan) error { return p.base.ready(ctx, v) }

type actionArtifact struct{ base *managedAction }

func (p actionArtifact) ArtifactDigest() string {
	return strings.TrimPrefix(p.base.e.status.Identity.Integrity, "sha256:")
}

// Preserve optional interfaces exactly; a basic action never acquires recovery
// or preflight methods just because it passes through a runtime wrapper.
func wrapAction(e *runtimeEntry, p ActionPlugin) ActionPlugin {
	base := &managedAction{managedPlugin{e}, p}
	variant := 0
	if _, ok := p.(PreflightActionPlugin); ok && (!e.strict || Supports(e.manifest.Capabilities, CapabilityActionPreflight)) {
		variant |= 1
	}
	if _, ok := p.(RecoverableActionPlugin); ok && (!e.strict || Supports(e.manifest.Capabilities, CapabilityActionExecute)) {
		variant |= 2
	}
	if _, ok := p.(ArtifactIdentity); ok {
		variant |= 4
	}
	switch variant {
	case 0:
		return &struct{ *managedAction }{base}
	case 1:
		return &struct {
			*managedAction
			actionPreflight
		}{base, actionPreflight{base}}
	case 2:
		return &struct {
			*managedAction
			actionRecovery
		}{base, actionRecovery{base}}
	case 3:
		return &struct {
			*managedAction
			actionPreflight
			actionRecovery
		}{base, actionPreflight{base}, actionRecovery{base}}
	case 4:
		return &struct {
			*managedAction
			actionArtifact
		}{base, actionArtifact{base}}
	case 5:
		return &struct {
			*managedAction
			actionPreflight
			actionArtifact
		}{base, actionPreflight{base}, actionArtifact{base}}
	case 6:
		return &struct {
			*managedAction
			actionRecovery
			actionArtifact
		}{base, actionRecovery{base}, actionArtifact{base}}
	case 7:
		return &struct {
			*managedAction
			actionPreflight
			actionRecovery
			actionArtifact
		}{base, actionPreflight{base}, actionRecovery{base}, actionArtifact{base}}
	}
	panic("unreachable action wrapper")
}

func (p *managedMetrics) bindEvidence(records []evidence.Record) ([]evidence.Record, error) {
	records, err := freeze(records)
	if err != nil {
		return nil, invalidOutput()
	}
	byID := map[string]int{}
	for i, r := range records {
		if r.Validate() != nil {
			return nil, invalidOutput()
		}
		byID[r.ID] = i
	}
	states := make([]uint8, len(records))
	var visit func(int) error
	visit = func(i int) error {
		if states[i] == 2 {
			return nil
		}
		if states[i] == 1 {
			return invalidOutput()
		}
		states[i] = 1
		r := records[i]
		for j, parent := range r.Parents {
			if index, ok := byID[parent]; ok {
				if err := visit(index); err != nil {
					return err
				}
				r.Parents[j] = records[index].ID
			}
		}
		identity := p.e.status.Identity
		r.Producer = &identity
		r.PluginID = p.ID()
		r.ID = ""
		sealed, err := evidence.Seal(r)
		if err != nil {
			return invalidOutput()
		}
		records[i] = sealed
		states[i] = 2
		return nil
	}
	for i := range records {
		if err := visit(i); err != nil {
			return nil, err
		}
	}
	return records, nil
}

func freezeOutput[T any](v T) (T, error) {
	out, err := freeze(v)
	if err != nil {
		return out, invalidOutput()
	}
	return out, nil
}
