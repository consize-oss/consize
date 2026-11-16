package prometheus_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/consize-oss/consize/internal/store"
	"github.com/consize-oss/consize/pkg/plugins/prometheus"
	"github.com/consize-oss/consize/pkg/resource"
)

func TestMappingProvenanceSurvivesRecommendationPersistence(t *testing.T) {
	ctx := context.Background()
	end := time.Now().UTC().Add(-time.Second)
	start := end.Add(-2 * time.Minute)
	r := resource.Resource{Provider: resource.ProviderKubernetes, Account: "fixture-cluster", Region: "local", Type: resource.TypeKubernetesDeployment, Name: "app", ProviderResourceID: "payments/app", Owner: "platform", Environment: resource.EnvStaging, Criticality: resource.CriticalityMedium, ObservedAt: start, Metadata: map[string]any{"namespace": "payments", "name": "app", "uid": "fixture-deployment", "mapping": map[string]any{"version": "kubernetes-deployment-mapper-v1", "containers": []any{map[string]any{"name": "app", "kind": "container"}}}}}
	path := filepath.Join(t.TempDir(), "state.json")
	st, err := store.OpenDurable(path)
	if err != nil {
		t.Fatal(err)
	}
	r, err = st.UpsertResource(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		from, e := time.Parse(time.RFC3339Nano, req.URL.Query().Get("start"))
		if e != nil {
			t.Error(e)
			return
		}
		to, e := time.Parse(time.RFC3339Nano, req.URL.Query().Get("end"))
		if e != nil {
			t.Error(e)
			return
		}
		points := [][2]any{}
		for at := from; !at.After(to); at = at.Add(time.Minute) {
			points = append(points, [2]any{float64(at.UnixMilli()) / 1000, "1"})
		}
		labels := map[string]string{"cluster": "fixture-cluster", "namespace": "payments", "pod": "app-rs-pod", "uid": "fixture-pod", "deployment": "app", "deployment_uid": "fixture-deployment", "container": "app"}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{"resultType": "matrix", "result": []any{map[string]any{"metric": labels, "values": points}}}})
	}))
	defer server.Close()
	p, err := prometheus.New(prometheus.Config{BaseURL: server.URL, Step: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := p.ReadMetricsBetween(ctx, r, start, end)
	if err != nil {
		t.Fatal(err)
	}
	if err = snapshot.ValidateRecords(); err != nil {
		t.Fatal(err)
	}
	refs := []string{}
	for _, record := range snapshot.Records {
		refs = append(refs, record.ID)
	}
	rec, err := st.CreateRecommendation(ctx, store.Recommendation{ResourceID: r.ID, PluginID: "fixture-action", AlgorithmID: "fixture-algorithm", AlgorithmVersion: "1", ActionType: "resize", Title: "fixture", EvidenceRefs: refs, EvidenceSnapshot: snapshot.Records})
	if err != nil {
		t.Fatal(err)
	}
	if err = st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = store.OpenDurable(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	got, err := st.GetRecommendation(ctx, rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range got.EvidenceSnapshot {
		if record.Extensions["mapping_version"] != prometheus.MappingVersion || record.Extensions["mapping_status"] != "mapped" || len(record.Extensions["mapping_digest"]) != 64 {
			t.Fatal("mapping provenance lost", record.Extensions)
		}
	}
}
