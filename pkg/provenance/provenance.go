// Package provenance carries immutable runtime attribution without depending on
// plugin interfaces, resource models or persistence implementations.
package provenance

type Identity struct {
	PackageID       string `json:"package_id"`
	InstanceID      string `json:"instance_id"`
	Version         string `json:"version"`
	Integrity       string `json:"integrity,omitempty"`
	ConfigurationID string `json:"configuration_id,omitempty"`
}
