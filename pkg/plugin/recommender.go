package plugin

import (
	"context"
	"github.com/consize-oss/consize/pkg/evidence"
	"github.com/consize-oss/consize/pkg/resource"
)

type RecommendationInput struct {
	Resource resource.Resource
	Evidence []MetricsSnapshot
}

// RecommendationProposal is a provider proposal. IDs, lifecycle, approval,
// policy and savings classification remain core-owned persistent contracts.
type RecommendationProposal struct {
	Runtime          *Provenance       `json:"runtime,omitempty"`
	ResourceID       string            `json:"resource_id"`
	ActionPluginID   string            `json:"action_plugin_id"`
	ActionType       string            `json:"action_type"`
	AlgorithmID      string            `json:"algorithm_id"`
	AlgorithmVersion string            `json:"algorithm_version"`
	Title            string            `json:"title"`
	Summary          string            `json:"summary"`
	Current          map[string]any    `json:"current"`
	Proposed         map[string]any    `json:"proposed"`
	Parameters       map[string]any    `json:"parameters"`
	Confidence       string            `json:"confidence"`
	Risk             string            `json:"risk"`
	Evidence         []string          `json:"evidence"`
	EvidenceRefs     []string          `json:"evidence_refs"`
	EvidenceSnapshot []evidence.Record `json:"evidence_snapshot"`
}
type RecommenderPlugin interface {
	Plugin
	Recommend(context.Context, RecommendationInput) ([]RecommendationProposal, error)
}
type managedRecommender struct {
	managedPlugin
	raw RecommenderPlugin
}

func (p *managedRecommender) Recommend(ctx context.Context, input RecommendationInput) ([]RecommendationProposal, error) {
	if err := p.acceptCapability(CapabilityRecommend, input.Resource, "", ""); err != nil {
		return nil, err
	}
	input, err := freeze(input)
	if err != nil {
		return nil, err
	}
	return run(ctx, p.e, modeInvoke, func(ctx context.Context) ([]RecommendationProposal, error) {
		proposals, err := p.raw.Recommend(ctx, input)
		if err != nil {
			return nil, err
		}
		if len(proposals) > 256 {
			return nil, invalidOutput()
		}
		for i := range proposals {
			v := &proposals[i]
			if v.ResourceID != input.Resource.ID || v.AlgorithmID != p.e.manifest.ID || v.ActionPluginID == "" || v.ActionType == "" || v.Title == "" {
				return nil, invalidOutput()
			}
			v.AlgorithmID = p.ID()
			identity := p.e.status.Identity
			v.Runtime = &identity
		}
		return freezeOutput(proposals)
	})
}
func (m *Manager) RegisterRecommender(p RecommenderPlugin) error {
	return m.legacyRegister(p, CapabilityRecommend)
}
func (m *Manager) RecommenderPlugin(id, resourceType string) (RecommenderPlugin, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p := m.recommenders[id]
	if p == nil {
		return nil, ErrNotFound
	}
	if !Supports(p.Manifest().SupportedResourceTypes, resourceType) {
		return nil, ErrUnsupported
	}
	return p, nil
}
