package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBootstrapConfigurationRejectsSilentDefaultsFromTypos(t *testing.T) {
	for _, body := range []string{`{"kubernets":{"enabled":true}}`, `{"kubernetes":{"discovery_auth":{"kubeconfg":"fixture"}}}`, `{"recommender":{"metrics_plugin":"example"}}`, `{} {}`, `{broken`, `null`} {
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

func TestKubernetesDiscoveryConfigurationIsStrictAndPreserved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "discovery.json")
	content := `{"kubernetes":{"enabled":false,"kubeconfig":"fixture-kubeconfig","context":"fixture-context","cluster_id":"fixture-cluster","location":"local","discovery":{"namespaces":["alpha"],"label_selector":"app=chosen","page_size":17,"max_objects":100,"max_bytes":1048576,"timeout":"20s","annotation_allowlist":["example.io/team"]}}}`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadBootstrap(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Kubernetes.Context != "fixture-context" || cfg.Kubernetes.Discovery.PageSize != 17 || cfg.Kubernetes.Discovery.Namespaces[0] != "alpha" || cfg.Kubernetes.Discovery.MaxBytes != 1048576 {
		t.Fatal("discovery configuration lost")
	}
	if err := os.WriteFile(path, []byte(`{"kubernetes":{"discovery":{"unknown_option":true}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadBootstrap(path); err == nil {
		t.Fatal("unknown discovery setting accepted")
	}
}

func TestPrometheusMappingConfigurationIsStrictAndPreserved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"prometheus":{"mapping":{"mode":"owner_chain","cluster":"source_cluster","pod_uid":"pod_uid"}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadBootstrap(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Prometheus.Mapping.Mode != "owner_chain" || cfg.Prometheus.Mapping.Cluster != "source_cluster" || cfg.Prometheus.Mapping.PodUID != "pod_uid" {
		t.Fatal("mapping settings lost")
	}
	if err = os.WriteFile(path, []byte(`{"prometheus":{"mapping":{"pod_prefix":"guess"}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = LoadBootstrap(path); err == nil {
		t.Fatal("unknown mapping setting accepted")
	}
}

func TestDedicatedDiscoveryAuthConfigurationPreserved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fixture-config.json")
	if err := os.WriteFile(path, []byte(`{"kubernetes":{"discovery_auth":{"kubeconfig":"/run/secrets/fixture-reader","context":"fixture-reader-context"}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadBootstrap(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Kubernetes.DiscoveryAuth == nil || cfg.Kubernetes.DiscoveryAuth.Kubeconfig != "/run/secrets/fixture-reader" || cfg.Kubernetes.DiscoveryAuth.Context != "fixture-reader-context" {
		t.Fatal("dedicated discovery auth was dropped")
	}
}
