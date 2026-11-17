package kubernetes

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/consize-oss/consize/pkg/plugin"
	"github.com/consize-oss/consize/pkg/resource"
)

const ID = "kubernetes-action"

type DiscoveryAuthConfig struct {
	Kubeconfig string `json:"kubeconfig,omitempty"`
	Context    string `json:"context,omitempty"`
}

type Config struct {
	DiscoveryAuth *DiscoveryAuthConfig
	Context       string
	Discovery     DiscoveryConfig
	Kubeconfig    string
	ClusterID     string
	Location      string
}

type PatchDiff struct {
	Resource      string `json:"resource"`
	CurrentReq    int64  `json:"current_request"`
	ProposedReq   int64  `json:"proposed_request"`
	CurrentLimit  int64  `json:"current_limit"`
	ProposedLimit int64  `json:"proposed_limit"`
}

type Patcher interface {
	PatchDeployment(ctx context.Context, namespace, name string, diff PatchDiff) error
	ReadDeploymentResources(ctx context.Context, namespace, name, kind string) (req, lim int64, err error)
	Health(ctx context.Context) error
}

type Plugin struct {
	patcher   Patcher
	clusterID string
	location  string
	discovery discoveryOptions
	scans     scanState
}

func New(cfg Config) (*Plugin, error) {
	if cfg.ClusterID == "" || cfg.Location == "" {
		return nil, errors.New("kubernetes cluster_id and location are required for stable resource identity")
	}
	if _, err := discoveryDefaults(cfg.Discovery); err != nil {
		return nil, err
	}
	patcher, err := newK8sPatcher(cfg.Kubeconfig, cfg.Context, cfg.DiscoveryAuth)
	if err != nil {
		return nil, err
	}
	return NewWithPatcherAndConfig(patcher, cfg)
}

func NewWithPatcher(patcher Patcher) *Plugin {
	return newWithIdentity(patcher, "test-cluster", "local")
}

func newWithIdentity(patcher Patcher, clusterID, location string) *Plugin {
	options, _ := discoveryDefaults(DiscoveryConfig{})
	return &Plugin{patcher: patcher, clusterID: clusterID, location: location, discovery: options}
}

// NewWithPatcherAndConfig provides the same validated immutable discovery scope
// with an explicitly supplied provider client (including fake clients in tests).
func NewWithPatcherAndConfig(patcher Patcher, cfg Config) (*Plugin, error) {
	cfg.ClusterID = strings.TrimSpace(cfg.ClusterID)
	cfg.Location = strings.TrimSpace(cfg.Location)
	if _, err := resource.BuildID(resource.Identity{Provider: resource.ProviderKubernetes, Account: cfg.ClusterID, Location: cfg.Location, Type: resource.TypeKubernetesDeployment, ProviderResourceID: "validation/probe"}); err != nil {
		return nil, scanFailure("configuration")
	}
	options, err := discoveryDefaults(cfg.Discovery)
	if err != nil {
		return nil, err
	}
	return &Plugin{patcher: patcher, clusterID: cfg.ClusterID, location: cfg.Location, discovery: options}, nil
}

func (p *Plugin) ID() string { return ID }

func (p *Plugin) Manifest() plugin.Manifest {
	return plugin.Manifest{
		ID:                      ID,
		DisplayName:             "Kubernetes Action",
		Version:                 "0.3.0-alpha",
		Category:                plugin.CategoryAction,
		SupportedResourceTypes:  []string{resource.TypeKubernetesDeployment},
		SupportedActionTypes:    []string{"k8s.patch_resources"},
		Capabilities:            []string{plugin.CapabilityActionPlan, plugin.CapabilityActionExecute, plugin.CapabilityActionPreflight, plugin.CapabilityResourceDiscover},
		CanMutateInfrastructure: true,
		RequiresApproval:        true,
	}
}

func (p *Plugin) Health(ctx context.Context) plugin.Health {
	if p.patcher == nil {
		return plugin.Health{Status: "unhealthy", Message: "kubernetes patcher is not configured", CheckedAt: time.Now().UTC()}
	}
	return p.discoveryHealth(ctx)
}

func (p *Plugin) Plan(ctx context.Context, input plugin.ActionInput) (plugin.ActionPlan, error) {
	if input.Resource.Type != resource.TypeKubernetesDeployment {
		return plugin.ActionPlan{}, fmt.Errorf("unsupported resource type %q", input.Resource.Type)
	}
	ns, name, err := deploymentIdentity(input.Resource)
	if err != nil {
		return plugin.ActionPlan{}, err
	}
	patch, err := patchDiffFromParameters(input.Parameters)
	if err != nil {
		return plugin.ActionPlan{}, err
	}
	if patch.CurrentReq <= 0 || patch.ProposedReq > patch.CurrentReq || patch.ProposedReq < patch.CurrentReq-patch.CurrentReq/4 {
		return plugin.ActionPlan{}, errors.New("rightsizing requires a positive request and a reduction of no more than 25%")
	}
	if patch.ProposedLimit != patch.CurrentLimit {
		return plugin.ActionPlan{}, errors.New("rightsizing preserves limits; limit changes require a separate action capability")
	}
	if p.patcher != nil {
		req, lim, err := p.patcher.ReadDeploymentResources(ctx, ns, name, patch.Resource)
		if err != nil {
			return plugin.ActionPlan{}, err
		}
		if req != patch.CurrentReq || lim != patch.CurrentLimit {
			return plugin.ActionPlan{}, fmt.Errorf("current %s resources changed: expected request=%d limit=%d, got request=%d limit=%d", patch.Resource, patch.CurrentReq, patch.CurrentLimit, req, lim)
		}
	}
	plan := plugin.ActionPlan{
		OriginalState: input.Resource.CurrentState,
		PluginID:      ID,
		ResourceID:    input.Resource.ID,
		ActionType:    input.ActionType,
		Summary:       fmt.Sprintf("Patch %s/%s %s resources", ns, name, patch.Resource),
		Diff: map[string]any{
			"namespace": ns,
			"name":      name,
			"patch":     patch,
			"current":   input.Resource.CurrentState,
			"proposed":  input.Parameters["proposed"],
		},
		RollbackAvailable: false,
		RequiresApproval:  true,
	}
	plan.AppliedState = map[string]any{}
	for key, value := range input.Resource.CurrentState {
		plan.AppliedState[key] = value
	}
	suffix := "_bytes"
	if patch.Resource == "cpu" {
		suffix = "_millicores"
	}
	plan.AppliedState[patch.Resource+"_request"+suffix] = patch.ProposedReq
	plan.AppliedState[patch.Resource+"_limit"+suffix] = patch.ProposedLimit
	if recoverable, ok := p.patcher.(snapshotPatcher); ok {
		original, target, err := recoverable.PrepareSnapshot(ctx, ns, name, patch)
		if err != nil {
			return plugin.ActionPlan{}, err
		}
		plan.Diff["original"] = original
		plan.Diff["target"] = target
		plan.RollbackAvailable = true
		plan.Preflight, err = recoverable.PreflightSnapshot(ctx, ns, name, original, target)
		if err != nil {
			return plugin.ActionPlan{}, err
		}
	}
	return plan, nil
}

func (p *Plugin) Execute(ctx context.Context, plan plugin.ActionPlan) (plugin.ActionResult, error) {
	if p.patcher == nil {
		return plugin.ActionResult{}, errors.New("kubernetes patcher is not configured")
	}
	ns, _ := plan.Diff["namespace"].(string)
	name, _ := plan.Diff["name"].(string)
	_, err := patchDiffFromAny(plan.Diff["patch"])
	if err != nil {
		return plugin.ActionResult{}, err
	}
	if ns == "" || name == "" {
		return plugin.ActionResult{}, errors.New("kubernetes action plan is missing namespace or name")
	}
	if _, ok := plan.Diff["original"]; ok {
		return p.applySnapshot(ctx, plan, false)
	}
	return plugin.ActionResult{}, errors.New("execution requires an original and target recovery snapshot")
}

func deploymentIdentity(res resource.Resource) (string, string, error) {
	ns, _ := res.Metadata["namespace"].(string)
	name, _ := res.Metadata["name"].(string)
	if ns == "" {
		ns, _ = res.Labels["namespace"]
	}
	if name == "" {
		name = res.Name
	}
	if ns == "" || name == "" {
		return "", "", errors.New("kubernetes deployment resource requires metadata.namespace and metadata.name")
	}
	return ns, name, nil
}
