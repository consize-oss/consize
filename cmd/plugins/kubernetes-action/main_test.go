package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestStandaloneKubernetesRejectsUnknownAuthFieldsWithoutEchoingInput(t *testing.T) {
	for _, raw := range []string{`{"DiscoveryAuth":{"kubeconfg":"fixture-private-path"}}`, `{"fixture-private-field":"fixture-private-credential"}`, `{} {}`, `{"DiscoveryAuth":`} {
		_, err := configuredPlugin(json.RawMessage(raw))
		if err == nil {
			t.Fatal("invalid standalone configuration accepted")
		}
		if strings.Contains(err.Error(), "fixture-private") {
			t.Fatal("configuration contents leaked")
		}
	}
}
