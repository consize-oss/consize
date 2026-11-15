package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/consize-oss/consize/internal/auth"
	"github.com/consize-oss/consize/pkg/plugin"
	"github.com/consize-oss/consize/pkg/resource"
)

type apiDiscoverySnapshot struct {
	complete bool
	calls    int
}

func (*apiDiscoverySnapshot) ID() string { return "discovery-fixture" }
func (p *apiDiscoverySnapshot) Manifest() plugin.Manifest {
	return plugin.Manifest{ID: p.ID(), Category: plugin.CategoryDataSource, Capabilities: []string{plugin.CapabilityResourceDiscover}, SupportedResourceTypes: []string{resource.TypeKubernetesDeployment}}
}
func (*apiDiscoverySnapshot) Health(context.Context) plugin.Health {
	return plugin.Health{Status: "healthy"}
}
func (p *apiDiscoverySnapshot) Discover(ctx context.Context) ([]plugin.ProviderObservation, error) {
	snapshot, err := p.DiscoverSnapshot(ctx)
	return snapshot.Observations, err
}
func (p *apiDiscoverySnapshot) DiscoverSnapshot(context.Context) (plugin.DiscoverySnapshot, error) {
	p.calls++
	now := time.Now().UTC()
	scope := map[string]string{"namespace": "fixture"}
	encoded, _ := json.Marshal(scope)
	digest := sha256.Sum256(encoded)
	observation := plugin.ProviderObservation{PluginID: p.ID(), Provider: resource.ProviderKubernetes, ObservedAt: now, Resource: resource.Resource{Type: resource.TypeKubernetesDeployment, Provider: resource.ProviderKubernetes, ProviderResourceID: "fixture/api", Name: "api", Account: "fixture-cluster", Region: "local", Environment: resource.EnvDevelopment, Owner: "team", Criticality: resource.CriticalityLow}}
	return plugin.DiscoverySnapshot{Scan: plugin.DiscoveryScan{Version: plugin.DiscoveryScanVersion, ID: "fixture-api-scan", PluginID: p.ID(), Scope: scope, ScopeDigest: hex.EncodeToString(digest[:]), StartedAt: now, FinishedAt: now, Complete: p.complete, Count: 1, Pages: 1, SnapshotVersions: map[string]string{"fixture": "100"}}, Observations: []plugin.ProviderObservation{observation}}, nil
}
func TestDiscoveryAPIExposesCompleteScanAndRejectsIncompleteWithoutWrites(t *testing.T) {
	server, store, _ := apiFixture(t)
	provider := &apiDiscoverySnapshot{complete: true}
	if err := server.plugins.RegisterDiscovery(provider); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CON12_ADMIN", "fixture-admin-token-over-24-characters")
	t.Setenv("CON12_OPERATOR", "fixture-operator-token-over-24-characters")
	var err error
	server.auth, err = auth.New(auth.Config{Enabled: true, Tokens: []auth.TokenConfig{{Subject: "admin", Role: auth.RoleAdmin, TokenEnv: "CON12_ADMIN"}, {Subject: "operator", Role: auth.RoleOperator, TokenEnv: "CON12_OPERATOR"}}})
	if err != nil {
		t.Fatal(err)
	}
	request := func(token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/api/discovery", nil)
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, r)
		return w
	}
	if w := request(""); w.Code != 401 {
		t.Fatal(w.Code)
	}
	if w := request("fixture-operator-token-over-24-characters"); w.Code != 403 {
		t.Fatal(w.Code)
	}
	if provider.calls != 0 {
		t.Fatal("unauthorized discovery reached provider")
	}
	w := request("fixture-admin-token-over-24-characters")
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	var body struct {
		Providers []struct {
			Scan  plugin.DiscoveryScan `json:"scan"`
			Count int                  `json:"count"`
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Providers) != 1 || !body.Providers[0].Scan.Complete || body.Providers[0].Scan.Version != plugin.DiscoveryScanVersion || body.Providers[0].Count != 1 {
		t.Fatal("complete scan metadata missing")
	}
	before, err := store.ListResources(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	provider.complete = false
	if w = request("fixture-admin-token-over-24-characters"); w.Code != 502 {
		t.Fatalf("accepted incomplete snapshot: %d %s", w.Code, w.Body)
	}
	after, err := store.ListResources(context.Background())
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("incomplete scan changed registry")
	}
}
