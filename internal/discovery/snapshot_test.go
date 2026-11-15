package discovery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/consize-oss/consize/internal/store"
	"github.com/consize-oss/consize/pkg/plugin"
	"github.com/consize-oss/consize/pkg/resource"
)

type snapshotDiscovery struct {
	fakeDiscovery
	snapshot plugin.DiscoverySnapshot
	err      error
}

func (p snapshotDiscovery) DiscoverSnapshot(context.Context) (plugin.DiscoverySnapshot, error) {
	return p.snapshot, p.err
}
func snapshotFixture(now time.Time) plugin.DiscoverySnapshot {
	scope := map[string]string{"namespace": "alpha"}
	encoded, _ := json.Marshal(scope)
	digest := sha256.Sum256(encoded)
	observation := plugin.ProviderObservation{PluginID: "test-discovery", Provider: resource.ProviderKubernetes, ObservedAt: now, Resource: resource.Resource{Type: resource.TypeKubernetesDeployment, Provider: resource.ProviderKubernetes, ProviderResourceID: "alpha/api", Name: "api", Account: "cluster-one", Region: "local", Environment: resource.EnvDevelopment, Owner: "team", Criticality: resource.CriticalityLow}}
	return plugin.DiscoverySnapshot{Scan: plugin.DiscoveryScan{Version: plugin.DiscoveryScanVersion, ID: "fixture-scan", PluginID: "test-discovery", Scope: scope, ScopeDigest: hex.EncodeToString(digest[:]), StartedAt: now.Add(-time.Second), FinishedAt: now, Complete: true, Count: 1, Pages: 1}, Observations: []plugin.ProviderObservation{observation}}
}
func TestIncompleteOrInvalidSnapshotCannotWriteRegistry(t *testing.T) {
	now := time.Now().UTC()
	for _, tc := range []struct {
		name   string
		mutate func(*plugin.DiscoverySnapshot)
		err    error
	}{
		{"incomplete", func(s *plugin.DiscoverySnapshot) { s.Scan.Complete = false }, nil},
		{"failed after a partial page", func(*plugin.DiscoverySnapshot) {}, errors.New("fixture discovery failed")},
		{"count mismatch", func(s *plugin.DiscoverySnapshot) { s.Scan.Count = 0 }, nil},
		{"wrong source", func(s *plugin.DiscoverySnapshot) { s.Scan.PluginID = "other" }, nil},
		{"scope tampered", func(s *plugin.DiscoverySnapshot) { s.Scan.Scope["namespace"] = "beta" }, nil},
		{"complete with error", func(s *plugin.DiscoverySnapshot) { s.Scan.ErrorCode = "forbidden" }, nil},
		{"bad observation after good one", func(s *plugin.DiscoverySnapshot) {
			bad := s.Observations[0]
			bad.PluginID = "other"
			s.Observations = append(s.Observations, bad)
			s.Scan.Count = 2
		}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snapshot := snapshotFixture(now)
			tc.mutate(&snapshot)
			manager := plugin.NewManager()
			if err := manager.RegisterDiscovery(snapshotDiscovery{snapshot: snapshot, err: tc.err}); err != nil {
				t.Fatal(err)
			}
			registry := store.NewMemory()
			service := New(registry, manager)
			service.now = func() time.Time { return now }
			if _, err := service.Run(context.Background()); err == nil {
				t.Fatal("invalid snapshot accepted")
			}
			resources, err := registry.ListResources(context.Background())
			if err != nil || len(resources) != 0 {
				t.Fatal("partial snapshot wrote resources")
			}
		})
	}
}
func TestEmptyCompleteSnapshotAndLegacyRemainDistinct(t *testing.T) {
	now := time.Now().UTC()
	snapshot := snapshotFixture(now)
	snapshot.Observations = []plugin.ProviderObservation{}
	snapshot.Scan.Count = 0
	manager := plugin.NewManager()
	if err := manager.RegisterDiscovery(snapshotDiscovery{snapshot: snapshot}); err != nil {
		t.Fatal(err)
	}
	registry := store.NewMemory()
	service := New(registry, manager)
	service.now = func() time.Time { return now }
	results, err := service.Run(context.Background())
	if err != nil || len(results) != 1 || results[0].Scan == nil || !results[0].Scan.Complete || results[0].Count != 0 {
		t.Fatalf("empty scan: %+v %v", results, err)
	}
	legacy := fakeDiscovery{observation: snapshotFixture(now).Observations[0]}
	manager = plugin.NewManager()
	if err := manager.RegisterDiscovery(legacy); err != nil {
		t.Fatal(err)
	}
	service = New(registry, manager)
	service.now = func() time.Time { return now }
	results, err = service.Run(context.Background())
	if err != nil || results[0].Scan != nil {
		t.Fatal("legacy observations fabricated complete scan metadata")
	}
}
