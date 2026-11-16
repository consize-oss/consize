package resource

import (
	"fmt"
	"strings"
	"unicode"
)

// ValidateInstanceUpdate preserves logical canonical IDs while rejecting silent
// replacement or loss of a known provider instance, including legacy UID metadata.
func ValidateInstanceUpdate(existing, next Resource) error {
	previous, err := providerInstance(existing)
	if err != nil {
		return err
	}
	current, err := providerInstance(next)
	if err != nil {
		return err
	}
	if previous != "" && current != previous {
		return fmt.Errorf("%w: provider instance changed or is missing; replacement requires explicit identity review", ErrIdentityConflict)
	}
	return nil
}
func providerInstance(r Resource) (string, error) {
	read := func(key string) (string, error) {
		raw, present := r.Metadata[key]
		if !present {
			return "", nil
		}
		value, ok := raw.(string)
		if !ok || value == "" || strings.TrimSpace(value) != value || len(value) > 512 || strings.IndexFunc(value, unicode.IsControl) >= 0 {
			return "", fmt.Errorf("%w: invalid provider instance metadata", ErrInvalidResource)
		}
		return value, nil
	}
	instance, err := read("provider_instance_id")
	if err != nil {
		return "", err
	}
	if r.Provider == ProviderKubernetes && r.Type == TypeKubernetesDeployment {
		uid, err := read("uid")
		if err != nil {
			return "", err
		}
		if instance != "" && uid != "" && instance != uid {
			return "", fmt.Errorf("%w: conflicting provider instance metadata", ErrInvalidResource)
		}
		if instance == "" {
			instance = uid
		}
	}
	return instance, nil
}
