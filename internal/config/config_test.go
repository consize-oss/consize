package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBootstrapConfigurationRejectsSilentDefaultsFromTypos(t *testing.T) {
	for _, body := range []string{`{"kubernets":{"enabled":true}}`, `{"recommender":{"metrics_plugin":"example"}}`, `{} {}`, `{broken`, `null`} {
		path := filepath.Join(t.TempDir(), "config.json")
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadBootstrap(path); err == nil {
			t.Fatalf("invalid configuration accepted: %s", body)
		}
	}
	if _, err := LoadBootstrap(""); err == nil {
		t.Fatal("missing configuration path accepted")
	}
}
