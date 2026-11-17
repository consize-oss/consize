package kubernetes

import (
	"context"
	"strings"

	"github.com/consize-oss/consize/pkg/plugin"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
)

func (p *Plugin) Discover(ctx context.Context) ([]plugin.ProviderObservation, error) {
	snapshot, err := p.DiscoverSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	return snapshot.Observations, nil
}

func (k *K8sPatcher) ListDeploymentPage(ctx context.Context, namespace string, options metav1.ListOptions) (*appsv1.DeploymentList, error) {
	client := k.discoveryClient
	if client == nil {
		client = k.client
	}
	if client == nil {
		return nil, scanFailure("unsupported_client")
	}
	var list *appsv1.DeploymentList
	var err error
	restClient := client.AppsV1().RESTClient()
	concrete, isConcrete := restClient.(*rest.RESTClient)
	if restClient != nil && (!isConcrete || concrete != nil) {
		list = &appsv1.DeploymentList{}
		err = restClient.Get().Namespace(namespace).Resource("deployments").VersionedParams(&options, scheme.ParameterCodec).MaxRetries(0).Do(ctx).Into(list)
	} else {
		list, err = client.AppsV1().Deployments(namespace).List(ctx, options)
	}
	if err != nil {
		return nil, scopedScanError(err, namespace)
	}
	return list, nil
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
