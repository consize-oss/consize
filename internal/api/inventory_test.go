package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/consize-oss/consize/internal/auth"
	"github.com/consize-oss/consize/internal/bootstrap"
	"github.com/consize-oss/consize/internal/policy"
	"github.com/consize-oss/consize/internal/store"
	"github.com/consize-oss/consize/pkg/evidence"
	"github.com/consize-oss/consize/pkg/plugin"
	"github.com/consize-oss/consize/pkg/resource"
)

type inventoryMetrics struct{ apiMetrics }

func (inventoryMetrics) ReadMetrics(context.Context, resource.Resource) (plugin.MetricsSnapshot, error) {
	panic("inventory must not query providers")
}
func (inventoryMetrics) Health(context.Context) plugin.Health { panic("health is not telemetry") }

func inventoryFixture(t *testing.T) (*Server, *store.Memory, time.Time) {
	t.Helper()
	st := store.NewMemory()
	pm := plugin.NewManager()
	if err := pm.RegisterMetrics(inventoryMetrics{}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b", "c"} {
		_, err := st.UpsertResource(context.Background(), resource.Resource{ID: id, Type: resource.TypeKubernetesDeployment, Provider: resource.ProviderKubernetes, ProviderResourceID: "ns/" + id, Name: id, Environment: resource.EnvDevelopment, Owner: "unassigned", Account: "cluster", Region: "local", Criticality: resource.CriticalityLow})
		if err != nil {
			t.Fatal(err)
		}
	}
	s, err := NewServer(st, pm, policy.NewEngine(), bootstrap.Config{})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	s.inventoryNow = func() time.Time { return now }
	return s, st, now
}
func inventoryRequest(t *testing.T, s *Server, query string) (inventoryPage, *httptest.ResponseRecorder) {
	t.Helper()
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/resources/inventory"+query, nil))
	var page inventoryPage
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
	}
	return page, w
}
func TestInventoryPaginationAndSignedScope(t *testing.T) {
	s, _, now := inventoryFixture(t)
	first, w := inventoryRequest(t, s, "?limit=1")
	if w.Code != 200 || !first.HasMore || first.Summary.Discovered != 3 || first.FilteredTotal != 3 || len(first.Resources) != 1 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if first.Resources[0].Resource.ID != "a" || first.Resources[0].Assessment.Telemetry.Status != "unknown" || first.Resources[0].Assessment.Policy.Status != "covered" {
		t.Fatal(first)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("missing private response cache control")
	}
	second, w := inventoryRequest(t, s, "?limit=2&cursor="+url.QueryEscape(first.NextCursor))
	if w.Code != 200 || second.HasMore || len(second.Resources) != 2 || second.Resources[0].Resource.ID != "b" || !reflect.DeepEqual(second.Summary, first.Summary) || second.SnapshotID != first.SnapshotID || !second.AssessedAt.Equal(first.AssessedAt) {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	for _, query := range []string{"?tab=attention&cursor=" + url.QueryEscape(first.NextCursor), "?cursor=" + url.QueryEscape(first.NextCursor+"x"), "?cursor=garbage", "?limit=201", "?owner=x&owner=y"} {
		_, w = inventoryRequest(t, s, query)
		if w.Code != 400 {
			t.Fatalf("accepted %s: %d", query, w.Code)
		}
	}
	s.inventoryNow = func() time.Time { return now.Add(inventoryCursorTTL + time.Second) }
	_, w = inventoryRequest(t, s, "?cursor="+url.QueryEscape(first.NextCursor))
	if w.Code != 409 {
		t.Fatal(w.Body.String())
	}
}
func TestInventoryConcurrentRefreshRequiresNewSnapshot(t *testing.T) {
	s, st, _ := inventoryFixture(t)
	first, _ := inventoryRequest(t, s, "?limit=1")
	r, _ := st.GetResource(context.Background(), "a")
	r.Owner = "new-owner"
	r.ObservedAt = time.Now().UTC()
	if _, err := st.UpsertResource(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	_, w := inventoryRequest(t, s, "?cursor="+url.QueryEscape(first.NextCursor))
	if w.Code != 409 || !strings.Contains(w.Body.String(), "inventory changed") {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	newPage, w := inventoryRequest(t, s, "")
	if w.Code != 200 || newPage.Summary.Reasons["unowned"] != 2 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				w := httptest.NewRecorder()
				s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/api/resources/inventory?limit=1", nil))
				if w.Code != 200 {
					t.Errorf("parallel read: %d", w.Code)
				}
			}
		}()
	}
	wg.Wait()
}
func TestInventoryRefreshesEvidenceWithoutDiscoveryAndDoesNotExposeQueries(t *testing.T) {
	s, st, now := inventoryFixture(t)
	before, _ := inventoryRequest(t, s, "")
	if before.Summary.Reporting != 0 {
		t.Fatal(before.Summary)
	}
	res, _ := st.GetResource(context.Background(), "a")
	beforeSeen := res.LastSeenAt
	records := []evidence.Record{}
	refs := []string{}
	for _, spec := range s.inventorySignals() {
		r, err := evidence.Seal(evidence.Record{ResourceID: "a", PluginID: "metrics", Signal: spec.Name, Query: "secret-api-key-in-provider-query", Kind: evidence.Aggregated, Aggregation: "p95", Unit: spec.Unit, OriginalUnit: spec.Unit, Scale: 1, Value: evidence.Number(0), SampleCount: 30, WindowStart: now.Add(-time.Hour), WindowEnd: now.Add(-time.Minute), CollectedAt: now.Add(-30 * time.Second), FreshUntil: now.Add(time.Minute), Quality: evidence.Valid})
		if err != nil {
			t.Fatal(err)
		}
		records = append(records, r)
		refs = append(refs, r.ID)
	}
	_, err := st.CreateRecommendation(context.Background(), store.Recommendation{ResourceID: "a", PluginID: "actions", ActionType: "resize", AlgorithmID: "test", AlgorithmVersion: "1", EvidenceSnapshot: records, EvidenceRefs: refs})
	if err != nil {
		t.Fatal(err)
	}
	after, w := inventoryRequest(t, s, "")
	if w.Code != 200 || after.Summary.Reporting != 1 || strings.Contains(w.Body.String(), "secret-api-key") {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if !after.Resources[0].Resource.LastSeenAt.Equal(beforeSeen) {
		t.Fatal("assessment changed discovery state")
	}
	s.inventoryNow = func() time.Time { return now.Add(2 * time.Minute) }
	stale, w := inventoryRequest(t, s, "")
	if w.Code != 200 || stale.Summary.Reporting != 0 || stale.Resources[0].Assessment.Telemetry.Status != "stale" {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}

type unavailableInventory struct{ *store.Memory }

func (unavailableInventory) ReadInventory(context.Context) (store.InventorySnapshot, error) {
	return store.InventorySnapshot{}, errors.New("postgres://admin:private-password@host/db")
}
func TestInventoryAuthorizationMethodsAndSanitizedFailures(t *testing.T) {
	t.Setenv("CON75_VIEWER_TOKEN", strings.Repeat("v", 32))
	cfg := bootstrap.Config{Auth: auth.Config{Enabled: true, Tokens: []auth.TokenConfig{{Subject: "viewer", Role: auth.RoleViewer, TokenEnv: "CON75_VIEWER_TOKEN"}}}}
	s, err := NewServer(unavailableInventory{store.NewMemory()}, plugin.NewManager(), policy.NewEngine(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		method, token string
		want          int
	}{{"GET", "", 401}, {"GET", strings.Repeat("v", 32), 503}, {"POST", strings.Repeat("v", 32), 405}} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(tc.method, "/api/resources/inventory", nil)
		if tc.token != "" {
			r.Header.Set("Authorization", "Bearer "+tc.token)
		}
		s.Handler().ServeHTTP(w, r)
		if w.Code != tc.want || strings.Contains(w.Body.String(), "private-password") {
			t.Fatalf("%s: %d %s", tc.method, w.Code, w.Body.String())
		}
	}
}
func TestInventoryCursorRejectsDifferentServerAndFutureTime(t *testing.T) {
	s, _, now := inventoryFixture(t)
	first, _ := inventoryRequest(t, s, "?limit=1")
	other, _, _ := inventoryFixture(t)
	_, w := inventoryRequest(t, other, "?cursor="+url.QueryEscape(first.NextCursor))
	if w.Code != 400 {
		t.Fatal("cursor survived server key change")
	}
	q := "?cursor=" + url.QueryEscape(s.signInventoryCursor(inventoryCursor{At: now.Add(time.Hour), After: "a", Filter: digestInventory(struct{}{})}))
	_, w = inventoryRequest(t, s, q)
	if w.Code != 400 {
		t.Fatal(fmt.Sprint(w.Code))
	}
}
