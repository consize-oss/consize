package kubernetes

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/consize-oss/consize/pkg/plugin"
	"github.com/consize-oss/consize/pkg/resource"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (p *Plugin) Discover(ctx context.Context) ([]plugin.ProviderObservation, error) {
	snapshot, err := p.DiscoverSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	return snapshot.Observations, nil
}

func (p *Plugin) deploymentResource(deployment appsv1.Deployment, observedAt time.Time) resource.Resource {
	var cpuRequest, cpuLimit, memoryRequest, memoryLimit int64
	for _, container := range deployment.Spec.Template.Spec.Containers {
		cpuRequest += container.Resources.Requests.Cpu().MilliValue()
		cpuLimit += container.Resources.Limits.Cpu().MilliValue()
		memoryRequest += container.Resources.Requests.Memory().Value()
		memoryLimit += container.Resources.Limits.Memory().Value()
	}
	labels := cloneLabels(deployment.Labels)
	environment := firstNonEmpty(labels["consize.io/environment"], labels["environment"], resource.EnvDevelopment)
	owner := firstNonEmpty(labels["consize.io/owner"], labels["owner"], "unassigned")
	criticality := firstNonEmpty(labels["consize.io/criticality"], resource.CriticalityMedium)
	providerID := fmt.Sprintf("%s/%s", deployment.Namespace, deployment.Name)
	return resource.Resource{
		Type: resource.TypeKubernetesDeployment, Provider: resource.ProviderKubernetes,
		ProviderResourceID: providerID, Name: deployment.Name,
		Account: p.clusterID, Region: p.location,
		Environment: environment, Owner: owner, Criticality: criticality,
		Labels: labels, SourcePluginID: ID, ObservedAt: observedAt,
		Metadata: map[string]any{
			"namespace": deployment.Namespace, "name": deployment.Name,
			"uid": string(deployment.UID), "resource_version": deployment.ResourceVersion,
			"generation": deployment.Generation, "replicas": valueOrDefault(deployment.Spec.Replicas, 1),
			"pod_regex": deployment.Name + "-.+",
		},
		CurrentState: map[string]any{
			"cpu_request_millicores": cpuRequest, "cpu_limit_millicores": cpuLimit,
			"memory_request_bytes": memoryRequest, "memory_limit_bytes": memoryLimit,
		},
	}
}

func (k *K8sPatcher) ListDeploymentPage(ctx context.Context, namespace string, options metav1.ListOptions) (*appsv1.DeploymentList, error) {
	if k.client == nil {
		return nil, scanFailure("unsupported_client")
	}
	return k.client.AppsV1().Deployments(namespace).List(ctx, options)
}

// ListDeployments retains the existing convenience API with bounded enumeration.
func (k *K8sPatcher) ListDeployments(ctx context.Context) ([]appsv1.Deployment, error) {
	options, _ := discoveryDefaults(DiscoveryConfig{})
	ctx, cancel := context.WithTimeout(ctx, options.timeout)
	defer cancel()
	scan := plugin.DiscoveryScan{SnapshotVersions: map[string]string{}}
	items, err := collectDeployments(ctx, k, options, &scan)
	if err != nil {
		return nil, safeScanError(err)
	}
	return items, nil
}

func cloneLabels(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func valueOrDefault(value *int32, fallback int32) int32 {
	if value == nil {
		return fallback
	}
	return *value
}
