package kubernetes

import (
	"encoding/json"
	"sort"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

// captureDeployment extracts bounded discovery inputs, not the full manifest.
// No environment values, commands, volumes, credentials, condition messages or
// last-applied manifests cross the provider-neutral observation boundary.
func captureDeployment(deployment appsv1.Deployment, cluster string, annotationAllowlist []string) (map[string]any, error) {
	if deployment.UID == "" || deployment.Name == "" || deployment.Namespace == "" || cluster == "" {
		return nil, scanFailure("invalid_observation")
	}
	if len(deployment.Annotations) > 256 || len(deployment.Spec.Template.Annotations) > 256 || len(deployment.Labels) > 256 || len(deployment.Spec.Template.Labels) > 256 || len(deployment.OwnerReferences) > 64 || len(deployment.Spec.Template.Spec.Containers)+len(deployment.Spec.Template.Spec.InitContainers) > 256 {
		return nil, scanFailure("limit_exceeded")
	}
	annotations := map[string]string{}
	keys := []string{}
	for key := range deployment.Annotations {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range annotationAllowlist {
		if value, ok := deployment.Annotations[key]; ok {
			if len(value) > 4096 {
				return nil, scanFailure("limit_exceeded")
			}
			annotations[key] = value
		}
	}
	templateKeys := []string{}
	templateAnnotations := map[string]string{}
	for key := range deployment.Spec.Template.Annotations {
		templateKeys = append(templateKeys, key)
	}
	sort.Strings(templateKeys)
	for _, key := range annotationAllowlist {
		if value, ok := deployment.Spec.Template.Annotations[key]; ok {
			if len(value) > 4096 {
				return nil, scanFailure("limit_exceeded")
			}
			templateAnnotations[key] = value
		}
	}
	owners := []map[string]any{}
	for _, owner := range deployment.OwnerReferences {
		owners = append(owners, map[string]any{"api_version": owner.APIVersion, "kind": owner.Kind, "name": owner.Name, "uid": string(owner.UID), "controller": owner.Controller, "block_owner_deletion": owner.BlockOwnerDeletion})
	}
	sort.Slice(owners, func(i, j int) bool { return owners[i]["uid"].(string) < owners[j]["uid"].(string) })
	containers := []map[string]any{}
	add := func(kind string, list []corev1.Container) {
		for _, container := range list {
			requests, limits := map[string]string{}, map[string]string{}
			for name, quantity := range container.Resources.Requests {
				requests[string(name)] = quantity.String()
			}
			for name, quantity := range container.Resources.Limits {
				limits[string(name)] = quantity.String()
			}
			containers = append(containers, map[string]any{"kind": kind, "name": container.Name, "requests": requests, "limits": limits, "restart_policy": container.RestartPolicy})
		}
	}
	add("container", deployment.Spec.Template.Spec.Containers)
	add("init_container", deployment.Spec.Template.Spec.InitContainers)
	sort.Slice(containers, func(i, j int) bool {
		a, b := containers[i], containers[j]
		if a["kind"] != b["kind"] {
			return a["kind"].(string) < b["kind"].(string)
		}
		return a["name"].(string) < b["name"].(string)
	})
	var created any
	if !deployment.CreationTimestamp.IsZero() {
		created = deployment.CreationTimestamp.Time.UTC()
	}
	native := map[string]any{
		"version": "kubernetes-deployment-discovery-v1", "cluster_id": cluster, "namespace": deployment.Namespace, "name": deployment.Name, "uid": string(deployment.UID), "resource_version": deployment.ResourceVersion,
		"labels": cloneLabels(deployment.Labels), "annotation_keys": keys, "annotations": annotations, "annotation_values_omitted": len(keys) - len(annotations), "owner_references": owners,
		"selector": deployment.Spec.Selector, "template_labels": cloneLabels(deployment.Spec.Template.Labels), "template_annotation_keys": templateKeys, "template_annotations": templateAnnotations, "template_annotation_values_omitted": len(templateKeys) - len(templateAnnotations), "containers": containers, "created_at": created,
		"generation": deployment.Generation, "observed_generation": deployment.Status.ObservedGeneration, "desired_replicas": deployment.Spec.Replicas,
		"replicas": deployment.Status.Replicas, "ready_replicas": deployment.Status.ReadyReplicas, "updated_replicas": deployment.Status.UpdatedReplicas, "available_replicas": deployment.Status.AvailableReplicas, "unavailable_replicas": deployment.Status.UnavailableReplicas,
	}
	// Convert even native selector/quantity helper values to plain JSON so core
	// code never receives Kubernetes API types through an any-valued extension.
	encoded, err := json.Marshal(native)
	if err != nil {
		return nil, scanFailure("invalid_observation")
	}
	if len(encoded) > 64<<10 {
		return nil, scanFailure("limit_exceeded")
	}
	var out map[string]any
	if json.Unmarshal(encoded, &out) != nil {
		return nil, scanFailure("invalid_observation")
	}
	return out, nil
}
