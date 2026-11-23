package recommender

import (
	"context"
	"errors"
	"time"

	"github.com/consize-oss/consize/internal/bootstrap"
	"github.com/consize-oss/consize/internal/store"
	"github.com/consize-oss/consize/pkg/plugin"
	"github.com/consize-oss/consize/pkg/resource"
)

type adapter struct{ engine *HeadroomRecommender }

func (p *adapter) ID() string { return p.engine.ID() }
func (p *adapter) Manifest() plugin.Manifest {
	return plugin.Manifest{ID: p.ID(), DisplayName: "Headroom recommender", Version: "1.0.0", Category: plugin.CategoryAlgorithm, SupportedResourceTypes: []string{resource.TypeKubernetesDeployment}, Capabilities: []string{plugin.CapabilityRecommend}, Runtime: &plugin.RuntimeManifest{SDKRange: ">=1.0.0 <2.0.0", CoreRange: ">=0.3.0-alpha <1.0.0", Provider: "kubernetes", ConfigurationVersion: "1", Distribution: "builtin", Capabilities: []plugin.CapabilityDescriptor{{Name: plugin.CapabilityRecommend, Version: "1.0.0", ResourceTypes: []string{resource.TypeKubernetesDeployment}, Providers: []string{resource.ProviderKubernetes}}}}}
}
func (p *adapter) Health(context.Context) plugin.Health {
	return plugin.Health{Status: "healthy", Message: "Configured", CheckedAt: time.Now().UTC()}
}
func (p *adapter) Recommend(ctx context.Context, in plugin.RecommendationInput) ([]plugin.RecommendationProposal, error) {
	recs, err := p.engine.Recommend(ctx, Input{Resource: in.Resource, Evidence: in.Evidence})
	if err != nil {
		return nil, err
	}
	out := make([]plugin.RecommendationProposal, 0, len(recs))
	for _, r := range recs {
		out = append(out, plugin.RecommendationProposal{ResourceID: r.ResourceID, ActionPluginID: r.PluginID, ActionType: r.ActionType, AlgorithmID: r.AlgorithmID, AlgorithmVersion: r.AlgorithmVersion, Title: r.Title, Summary: r.Summary, Current: r.Current, Proposed: r.Proposed, Parameters: r.Parameters, Confidence: r.Confidence, Risk: r.Risk, Evidence: r.Evidence, EvidenceRefs: r.EvidenceRefs, EvidenceSnapshot: r.EvidenceSnapshot})
	}
	return out, nil
}

func Register(m *plugin.Manager, cfg bootstrap.RecommenderConfig) error {
	if !cfg.Enabled {
		return nil
	}
	if cfg.AlgorithmPluginID != "" && cfg.AlgorithmPluginID != BuiltInHeadroomAlgorithmID {
		_, err := m.RecommenderPlugin(cfg.AlgorithmPluginID, resource.TypeKubernetesDeployment)
		return err
	}
	if _, err := m.RecommenderPlugin(BuiltInHeadroomAlgorithmID, resource.TypeKubernetesDeployment); err == nil {
		return nil
	} else if !errors.Is(err, plugin.ErrNotFound) {
		return err
	}
	return m.RegisterRecommender(&adapter{engine: New(cfg)})
}
func Generate(ctx context.Context, m *plugin.Manager, id string, input Input) ([]store.Recommendation, error) {
	if id == "" {
		id = BuiltInHeadroomAlgorithmID
	}
	p, err := m.RecommenderPlugin(id, input.Resource.Type)
	if err != nil {
		return nil, err
	}
	proposals, err := p.Recommend(ctx, plugin.RecommendationInput{Resource: input.Resource, Evidence: input.Evidence})
	if err != nil {
		return nil, err
	}
	out := make([]store.Recommendation, 0, len(proposals))
	for _, v := range proposals {
		out = append(out, store.Recommendation{Runtime: v.Runtime, ResourceID: v.ResourceID, PluginID: v.ActionPluginID, ActionType: v.ActionType, AlgorithmID: v.AlgorithmID, AlgorithmVersion: v.AlgorithmVersion, Title: v.Title, Summary: v.Summary, Current: v.Current, Proposed: v.Proposed, Parameters: v.Parameters, Confidence: v.Confidence, Risk: v.Risk, Evidence: v.Evidence, EvidenceRefs: v.EvidenceRefs, EvidenceSnapshot: v.EvidenceSnapshot})
	}
	return out, nil
}
