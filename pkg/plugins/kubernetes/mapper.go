package kubernetes

import (
	"encoding/json"
	"math/big"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/consize-oss/consize/pkg/resource"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	quantity "k8s.io/apimachinery/pkg/api/resource"
	validation "k8s.io/apimachinery/pkg/util/validation"
)

const DeploymentMapperVersion = "kubernetes-deployment-mapper-v1"

// MappingError contains stable, safe codes, never provider payloads or values.
type MappingError struct{ Code string }

func (e *MappingError) Error() string  { return "kubernetes deployment mapping failed: " + e.Code }
func (e *MappingError) Unwrap() error  { return resource.ErrInvalidResource }
func mappingFailure(code string) error { return &MappingError{Code: code} }

type MappingConfig struct {
	ClusterID           string
	Location            string
	AnnotationAllowlist []string
}

type metadataSource struct {
	Source    string `json:"source"`
	Confirmed bool   `json:"confirmed"`
}
type mappedQuantity struct {
	Resource string `json:"resource"`
	Unit     string `json:"unit"`
	Value    string `json:"value"`
	Quantity string `json:"quantity"`
}
type mappedContainer struct {
	Name          string           `json:"name"`
	Kind          string           `json:"kind"`
	RestartPolicy string           `json:"restart_policy,omitempty"`
	Requests      []mappedQuantity `json:"requests"`
	Limits        []mappedQuantity `json:"limits"`
}

// MapDeployment is a pure transformation. Core still derives canonical IDs and
// owns registry timestamps. Native UID is an incarnation guard, not a new ID scheme.
func MapDeployment(deployment appsv1.Deployment, cfg MappingConfig, observedAt time.Time) (resource.Resource, error) {
	fail := func(code string) (resource.Resource, error) { return resource.Resource{}, mappingFailure(code) }
	if observedAt.IsZero() {
		return fail("observation_time_required")
	}
	if len(validation.IsDNS1123Label(deployment.Namespace)) != 0 || len(validation.IsDNS1123Subdomain(deployment.Name)) != 0 {
		return fail("invalid_object_identity")
	}
	uid := string(deployment.UID)
	if uid == "" || strings.TrimSpace(uid) != uid || len(uid) > 128 || strings.IndexFunc(uid, unicode.IsControl) >= 0 {
		return fail("uid_required_or_invalid")
	}
	cfg.ClusterID = strings.TrimSpace(cfg.ClusterID)
	cfg.Location = strings.TrimSpace(cfg.Location)
	if _, err := resource.BuildID(resource.Identity{Provider: resource.ProviderKubernetes, Account: cfg.ClusterID, Location: cfg.Location, Type: resource.TypeKubernetesDeployment, ProviderResourceID: deployment.Namespace + "/" + deployment.Name}); err != nil {
		return fail("invalid_cluster_identity")
	}
	if deployment.Generation < 0 || deployment.Status.ObservedGeneration < 0 || deployment.Spec.Replicas != nil && *deployment.Spec.Replicas < 0 {
		return fail("invalid_replica_or_generation")
	}
	// Resolve allowlist validation through the same bounded discovery contract.
	options, err := discoveryDefaults(DiscoveryConfig{AnnotationAllowlist: cfg.AnnotationAllowlist})
	if err != nil {
		return fail("invalid_annotation_allowlist")
	}
	native, err := captureDeployment(deployment, cfg.ClusterID, options.annotations)
	if err != nil {
		return fail("native_metadata_limit_or_identity")
	}
	labels := cloneLabels(deployment.Labels)
	environment, envSource := mappedMetadata(labels, "consize.io/environment", "environment", resource.EnvUnknown)
	environment = strings.ToLower(environment)
	if environment != resource.EnvProduction && environment != resource.EnvStaging && environment != resource.EnvDevelopment && environment != resource.EnvUnknown {
		return fail("invalid_environment")
	}
	if environment == resource.EnvUnknown {
		envSource.Confirmed = false
	}
	owner, ownerSource := mappedMetadata(labels, "consize.io/owner", "owner", resource.OwnerUnassigned)
	if strings.EqualFold(owner, resource.OwnerUnassigned) {
		owner = resource.OwnerUnassigned
		ownerSource.Confirmed = false
	}
	if len(owner) > 256 || strings.IndexFunc(owner, unicode.IsControl) >= 0 {
		return fail("invalid_owner")
	}
	criticality, criticalitySource := mappedMetadata(labels, "consize.io/criticality", "criticality", resource.CriticalityMedium)
	criticality = strings.ToLower(criticality)
	if criticality != resource.CriticalityLow && criticality != resource.CriticalityMedium && criticality != resource.CriticalityHigh {
		return fail("invalid_criticality")
	}
	containers := []mappedContainer{}
	names := map[string]bool{}
	totals := map[string]int64{"cpu_request_millicores": 0, "cpu_limit_millicores": 0, "memory_request_bytes": 0, "memory_limit_bytes": 0}
	complete := map[string]bool{"cpu_request_millicores": true, "cpu_limit_millicores": true, "memory_request_bytes": true, "memory_limit_bytes": true}
	for _, group := range []struct {
		kind string
		list []corev1.Container
	}{{"container", deployment.Spec.Template.Spec.Containers}, {"init_container", deployment.Spec.Template.Spec.InitContainers}} {
		for _, container := range group.list {
			if len(validation.IsDNS1123Label(container.Name)) != 0 || names[container.Name] {
				return fail("invalid_or_duplicate_container")
			}
			names[container.Name] = true
			requests, e := mapQuantities(container.Resources.Requests)
			if e != nil {
				return resource.Resource{}, e
			}
			limits, e := mapQuantities(container.Resources.Limits)
			if e != nil {
				return resource.Resource{}, e
			}
			for name, request := range container.Resources.Requests {
				if limit, ok := container.Resources.Limits[name]; ok && request.Cmp(limit) > 0 {
					return fail("request_exceeds_limit")
				}
			}
			entry := mappedContainer{Name: container.Name, Kind: group.kind, Requests: requests, Limits: limits}
			if container.RestartPolicy != nil {
				entry.RestartPolicy = string(*container.RestartPolicy)
			}
			containers = append(containers, entry)
			if group.kind != "container" {
				continue
			}
			for _, list := range []struct {
				values corev1.ResourceList
				suffix string
			}{{container.Resources.Requests, "request"}, {container.Resources.Limits, "limit"}} {
				for _, metric := range []struct {
					name         corev1.ResourceName
					prefix, unit string
				}{{corev1.ResourceCPU, "cpu", "millicores"}, {corev1.ResourceMemory, "memory", "bytes"}} {
					key := metric.prefix + "_" + list.suffix + "_" + metric.unit
					value, ok := list.values[metric.name]
					if !ok {
						complete[key] = false
						continue
					}
					number, e := boundedQuantity(value, metric.name)
					if e != nil {
						return resource.Resource{}, e
					}
					sum := new(big.Int).Add(big.NewInt(totals[key]), big.NewInt(number))
					if !sum.IsInt64() {
						return fail("quantity_overflow")
					}
					totals[key] = sum.Int64()
				}
			}
		}
	}
	if len(deployment.Spec.Template.Spec.Containers) == 0 {
		return fail("container_required")
	}
	sort.Slice(containers, func(i, j int) bool {
		if containers[i].Kind != containers[j].Kind {
			return containers[i].Kind < containers[j].Kind
		}
		return containers[i].Name < containers[j].Name
	})
	state := map[string]any{}
	for key, value := range totals {
		state[key] = value
	}
	metadata := map[string]any{"namespace": deployment.Namespace, "name": deployment.Name, "uid": uid, "provider_instance_id": uid, "resource_version": deployment.ResourceVersion, "generation": deployment.Generation, "replicas": valueOrDefault(deployment.Spec.Replicas, 1), "pod_regex": deployment.Name + "-.+", "discovery": native}
	// JSON conversion ensures typed helper values never leak through any-valued extensions.
	extension, err := json.Marshal(map[string]any{"version": DeploymentMapperVersion, "provenance": map[string]metadataSource{"environment": envSource, "owner": ownerSource, "criticality": criticalitySource}, "containers": containers, "aggregate_complete": complete, "aggregate_scope": "regular_containers_per_replica", "provider_created_at": native["created_at"]})
	if err != nil || len(extension) > 128<<10 {
		return fail("mapping_metadata_limit")
	}
	var mapped map[string]any
	if json.Unmarshal(extension, &mapped) != nil {
		return fail("mapping_metadata_invalid")
	}
	metadata["mapping"] = mapped
	res := resource.Resource{Type: resource.TypeKubernetesDeployment, Provider: resource.ProviderKubernetes, ProviderResourceID: deployment.Namespace + "/" + deployment.Name, Name: deployment.Name, Account: cfg.ClusterID, Region: cfg.Location, Environment: environment, Owner: owner, Criticality: criticality, Labels: labels, SourcePluginID: ID, ObservedAt: observedAt.UTC(), Metadata: metadata, CurrentState: state}
	if _, err := res.Normalize(observedAt); err != nil {
		return fail("invalid_universal_resource")
	}
	return res, nil
}

func mappedMetadata(labels map[string]string, primary, secondary, fallback string) (string, metadataSource) {
	for _, key := range []string{primary, secondary} {
		if value := strings.TrimSpace(labels[key]); value != "" {
			return value, metadataSource{Source: "label:" + key, Confirmed: true}
		}
	}
	return fallback, metadataSource{Source: "fallback", Confirmed: false}
}
func mapQuantities(values corev1.ResourceList) ([]mappedQuantity, error) {
	out := []mappedQuantity{}
	for name, value := range values {
		if value.Sign() < 0 {
			return nil, mappingFailure("negative_quantity")
		}
		if _, err := boundedQuantity(value, name); err != nil {
			return nil, err
		}
		unit := "units"
		decimal := value.DeepCopy()
		switch {
		case name == corev1.ResourceCPU:
			unit = "millicores"
			d := decimal.AsDec()
			d.SetScale(d.Scale() - 3)
		case name == corev1.ResourceMemory || name == corev1.ResourceEphemeralStorage || strings.HasPrefix(string(name), corev1.ResourceHugePagesPrefix):
			unit = "bytes"
		}
		out = append(out, mappedQuantity{Resource: string(name), Unit: unit, Value: decimal.AsDec().String(), Quantity: value.String()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Resource < out[j].Resource })
	return out, nil
}

// Match Kubernetes integer-unit rounding without overflowing Quantity.Value/MilliValue.
func boundedQuantity(value quantity.Quantity, name corev1.ResourceName) (int64, error) {
	if value.Sign() < 0 {
		return 0, mappingFailure("negative_quantity")
	}
	mantissa, exponent := value.AsCanonicalBytes(nil)
	if len(mantissa) > 24 || exponent < -18 || exponent > 18 {
		return 0, mappingFailure("quantity_overflow")
	}
	rational, ok := new(big.Rat).SetString(value.AsDec().String())
	if !ok {
		return 0, mappingFailure("invalid_quantity")
	}
	if name == corev1.ResourceCPU {
		rational.Mul(rational, big.NewRat(1000, 1))
	}
	integer, remainder := new(big.Int), new(big.Int)
	integer.QuoRem(rational.Num(), rational.Denom(), remainder)
	if remainder.Sign() != 0 {
		integer.Add(integer, big.NewInt(1))
	}
	if !integer.IsInt64() {
		return 0, mappingFailure("quantity_overflow")
	}
	return integer.Int64(), nil
}
