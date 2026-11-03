// Package demo contains opt-in synthetic data for design review only.
package demo

import (
	"context"
	"github.com/consize-oss/consize/internal/recommender"
	"github.com/consize-oss/consize/internal/store"
	"github.com/consize-oss/consize/pkg/resource"
)

func Seed(ctx context.Context, st store.Store) error {
	resources, err := st.ListResources(ctx)
	if err != nil {
		return err
	}
	if len(resources) > 0 {
		return nil
	}
	res := resource.Resource{
		Type:               resource.TypeKubernetesDeployment,
		Provider:           resource.ProviderKubernetes,
		ProviderResourceID: "payments/payment-service-deployment",
		Name:               "payment-service-deployment",
		Environment:        resource.EnvProduction,
		Owner:              "platform",
		Region:             "us-east-1",
		Account:            "acme-prod",
		Criticality:        resource.CriticalityHigh,
		MonthlyCost:        15000,
		Labels:             map[string]string{"namespace": "payments", "service": "payments"},
		Metadata:           map[string]any{"namespace": "payments", "name": "payment-service-deployment", "pod_regex": "payment-service-.+"},
		CurrentState:       map[string]any{"memory_request_bytes": 8589934592, "memory_limit_bytes": 17179869184},
	}
	res, err = st.UpsertResource(ctx, res)
	if err != nil {
		return err
	}
	_, err = st.CreateRecommendation(ctx, store.Recommendation{
		ResourceID:              res.ID,
		PluginID:                "kubernetes-action",
		AlgorithmID:             recommender.BuiltInHeadroomAlgorithmID,
		AlgorithmVersion:        "1",
		EvidenceRefs:            []string{"demo:metrics:payment-service-deployment"},
		ActionType:              "k8s.patch_resources",
		Title:                   "Reduce memory request for payment-service-deployment",
		Summary:                 "Reduce memory request while retaining 35% P95 headroom and requiring policy approval for production.",
		EstimatedSavingsMonthly: 4200,
		Confidence:              "medium",
		Risk:                    "low",
		PolicyID:                "oss-foundation-v1",
		Evidence:                []string{"Datadog/Prometheus memory P95 is stable", "No restart increase detected", "Production policy requires PR or approval path"},
		Current:                 map[string]any{"resource": "memory", "request": 8589934592, "limit": 17179869184},
		Proposed:                map[string]any{"resource": "memory", "request": 6442450944, "limit": 12884901888},
		Parameters: map[string]any{
			"patch": map[string]any{
				"resource":         "memory",
				"current_request":  8589934592,
				"proposed_request": 6442450944,
				"current_limit":    17179869184,
				"proposed_limit":   12884901888,
			},
			"proposed": map[string]any{"resource": "memory", "request": 6442450944, "limit": 12884901888},
		},
	})
	return err
}
