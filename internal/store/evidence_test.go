package store

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/consize-oss/consize/pkg/evidence"
)

func evidenceRecommendation(t *testing.T, resourceID string, value float64) Recommendation {
	t.Helper()
	now := time.Now().UTC()
	r, err := evidence.Seal(evidence.Record{ResourceID: resourceID, PluginID: "metrics-source", Signal: "memory_p95", Query: "synthetic selector", Kind: evidence.Aggregated, Aggregation: "p95", Unit: "bytes", OriginalUnit: "bytes", Scale: 1, Value: evidence.Number(value), SampleCount: 10, WindowStart: now.Add(-time.Hour), WindowEnd: now, CollectedAt: now, FreshUntil: now.Add(time.Minute), Quality: evidence.Valid, Extensions: map[string]string{"source.scope": "test"}})
	if err != nil {
		t.Fatal(err)
	}
	return Recommendation{ResourceID: resourceID, PluginID: "action-plugin", AlgorithmID: "headroom", AlgorithmVersion: "1", ActionType: "resize", EvidenceSnapshot: []evidence.Record{r}, EvidenceRefs: []string{r.ID}}
}

func assertEvidencePersistence(t *testing.T, st Store) int64 {
	t.Helper()
	ctx := context.Background()
	res := completeResource("evidence-resource")
	if _, err := st.UpsertResource(ctx, res); err != nil {
		t.Fatal(err)
	}
	input := evidenceRecommendation(t, res.ID, 100)
	oldID := input.EvidenceRefs[0]
	created, err := st.CreateRecommendation(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	*input.EvidenceSnapshot[0].Value = 999
	input.EvidenceSnapshot[0].Extensions["source.scope"] = "mutated"
	*created.EvidenceSnapshot[0].Value = 888
	replacement, err := st.CreateRecommendation(ctx, evidenceRecommendation(t, res.ID, 200))
	if err != nil {
		t.Fatal(err)
	}
	if err = st.SupersedeRecommendation(ctx, created.ID, replacement.ID, "new collection"); err != nil {
		t.Fatal(err)
	}
	old, err := st.GetRecommendation(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if old.EvidenceRefs[0] != oldID || *old.EvidenceSnapshot[0].Value != 100 || old.EvidenceSnapshot[0].Extensions["source.scope"] != "test" {
		t.Fatal("historical evidence rewritten")
	}
	bad := evidenceRecommendation(t, res.ID, 300)
	bad.EvidenceSnapshot[0].Query = "tampered"
	if _, err = st.CreateRecommendation(ctx, bad); err == nil {
		t.Fatal("tampered evidence stored")
	}
	return created.ID
}
func TestEvidenceDurableHistoricalSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	st, err := OpenDurable(path)
	if err != nil {
		t.Fatal(err)
	}
	id := assertEvidencePersistence(t, st)
	st.Close()
	st, err = OpenDurable(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := st.GetRecommendation(context.Background(), id)
	if err != nil || *got.EvidenceSnapshot[0].Value != 100 {
		t.Fatal("snapshot not restored", err)
	}
	st.Close()
	b, _ := os.ReadFile(path)
	var state diskState
	if err = json.Unmarshal(b, &state); err != nil {
		t.Fatal(err)
	}
	rec := state.Recommendations[id]
	rec.EvidenceSnapshot[0].Query = "altered"
	state.Recommendations[id] = rec
	b, _ = json.Marshal(state)
	if err = os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	if reopened, err := OpenDurable(path); err == nil {
		reopened.Close()
		t.Fatal("corrupt provenance restored")
	}
}
func TestEvidenceConcurrentCreates(t *testing.T) {
	st := NewMemory()
	ctx := context.Background()
	res := completeResource("evidence-concurrent")
	if _, err := st.UpsertResource(ctx, res); err != nil {
		t.Fatal(err)
	}
	rec := evidenceRecommendation(t, res.ID, 100)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := st.CreateRecommendation(ctx, rec); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	recs, err := st.ListRecommendations(ctx)
	if err != nil || len(recs) != 12 {
		t.Fatal("concurrent evidence lost", err)
	}
	for _, r := range recs {
		if r.EvidenceRefs[0] != rec.EvidenceRefs[0] {
			t.Fatal("inconsistent snapshot")
		}
	}
}

func TestEvidenceWriteFailureDoesNotPublishRecommendation(t *testing.T) {
	st := NewMemory()
	ctx := context.Background()
	res := completeResource("evidence-write-failure")
	if _, err := st.UpsertResource(ctx, res); err != nil {
		t.Fatal(err)
	}
	st.path = filepath.Join(t.TempDir(), "missing-parent", "state.json")
	if _, err := st.CreateRecommendation(ctx, evidenceRecommendation(t, res.ID, 100)); err == nil {
		t.Fatal("write failure accepted")
	}
	recs, err := st.ListRecommendations(ctx)
	if err != nil || len(recs) != 0 {
		t.Fatal("failed write remained visible", err)
	}
}
