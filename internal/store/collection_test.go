package store

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/consize-oss/consize/pkg/evidence"
	"github.com/consize-oss/consize/pkg/evidence/quality"
	"github.com/consize-oss/consize/pkg/plugin"
)

func collectionFixture(t *testing.T, st Store) Collection {
	t.Helper()
	r, err := st.UpsertResource(context.Background(), completeResource("collection-resource"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	return Collection{Resource: r, PluginID: "fixture-metrics", InstanceDigest: strings.Repeat("a", 64), Signals: []string{"cpu_cores_p95"}, WindowStart: now.Add(-time.Hour), WindowEnd: now, Actor: "fixture-operator", Trigger: "manual", MaxAttempts: 3}
}
func collectionResult(t *testing.T, c Collection) CollectionResult {
	t.Helper()
	now := time.Now().UTC()
	r, err := evidence.Seal(evidence.Record{ResourceID: c.Resource.ID, PluginID: c.PluginID, Signal: "cpu_cores_p95", Query: "fixture_cpu", Kind: evidence.Aggregated, Aggregation: "p95", Unit: "cores", OriginalUnit: "cores", Scale: 1, Value: evidence.Number(.5), SampleCount: 3, WindowStart: c.WindowStart, WindowEnd: c.WindowEnd, CollectedAt: now, FreshUntil: now.Add(time.Minute), Quality: evidence.Valid})
	if err != nil {
		t.Fatal(err)
	}
	s := plugin.MetricsSnapshot{ResourceID: c.Resource.ID, PluginID: c.PluginID, Window: c.WindowEnd.Sub(c.WindowStart).String(), CollectedAt: now, Signals: map[string]any{"cpu_cores_p95": .5}, Records: []evidence.Record{r}}
	return CollectionResult{Snapshot: &s, Assessment: &quality.Assessment{ResourceID: c.Resource.ID, PluginID: c.PluginID, Eligible: true}}
}
func exerciseCollectionStore(t *testing.T, st Store, cs CollectionStore) {
	t.Helper()
	ctx := context.Background()
	req := collectionFixture(t, st)
	var wins atomic.Int32
	var id string
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, err := cs.EnqueueCollection(ctx, req)
			if err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			if id != "" && id != c.ID {
				t.Error("duplicate operation")
			}
			id = c.ID
			mu.Unlock()
		}()
	}
	wg.Wait()
	overlapping := req
	overlapping.WindowEnd = overlapping.WindowEnd.Add(-time.Second)
	overlapping.WindowStart = overlapping.WindowStart.Add(-time.Second)
	if _, err := cs.EnqueueCollection(ctx, overlapping); !errors.Is(err, ErrCollectionOverlap) {
		t.Fatalf("overlap: %v", err)
	}
	var claimed Collection
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, err := cs.ClaimCollection(ctx, id, "fixture-worker", time.Minute)
			if err == nil {
				wins.Add(1)
				mu.Lock()
				claimed = c
				mu.Unlock()
			} else if !errors.Is(err, ErrJobBusy) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("claims=%d", wins.Load())
	}
	bad := collectionResult(t, claimed)
	bad.Snapshot.Records[0].WindowEnd = bad.Snapshot.Records[0].WindowEnd.Add(time.Second)
	if _, err := cs.FinishCollection(ctx, claimed, bad); err == nil {
		t.Fatal("accepted invalid evidence")
	}
	got, _ := cs.GetCollection(ctx, id)
	if got.State != "running" || got.Snapshot != nil {
		t.Fatal("invalid write corrupted operation")
	}
	result := collectionResult(t, claimed)
	done, err := cs.FinishCollection(ctx, claimed, result)
	if err != nil {
		t.Fatal(err)
	}
	if done.State != "succeeded" || done.Lease != nil || done.Snapshot == nil {
		t.Fatalf("completion=%+v", done)
	}
	if _, err := cs.FinishCollection(ctx, claimed, result); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("duplicate completion=%v", err)
	}
	same, err := cs.EnqueueCollection(ctx, req)
	if err != nil || same.State != "succeeded" || same.ID != id {
		t.Fatalf("idempotency=%+v %v", same, err)
	}
	jobs, err := cs.ListCollections(ctx, CollectionQuery{Due: true})
	if err != nil || len(jobs) != 0 {
		t.Fatalf("completed jobs due=%v %v", jobs, err)
	}
	// Getter results cannot mutate stored evidence.
	done.Snapshot.Records[0].Value = evidence.Number(99)
	stored, _ := cs.GetCollection(ctx, id)
	if *stored.Snapshot.Records[0].Value != .5 {
		t.Fatal("shared evidence")
	}
}
func TestMemoryCollectionLifecycleAndRaces(t *testing.T) {
	st := NewMemory()
	exerciseCollectionStore(t, st, st)
}
func TestDurableCollectionRestartAndRetention(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.json")
	st, err := OpenDurable(path)
	if err != nil {
		t.Fatal(err)
	}
	req := collectionFixture(t, st)
	job, err := st.EnqueueCollection(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	first, err := st.ClaimCollection(ctx, job.ID, "old-worker", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err = st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = OpenDurable(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	later := time.Now().Add(2 * time.Second)
	st.now = func() time.Time { return later }
	next, err := st.ClaimCollection(ctx, job.ID, "new-worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if next.Attempts != 2 || next.Fence <= first.Fence || next.History[0].Outcome != "interrupted" {
		t.Fatalf("recovery=%+v", next)
	}
	if _, err = st.FinishCollection(ctx, first, CollectionResult{ErrorCode: "provider_failure"}); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("old owner=%v", err)
	}
	result := collectionResult(t, next)
	done, err := st.FinishCollection(ctx, next, result)
	if err != nil {
		t.Fatal(err)
	}
	// A workflow reference pins historical evidence even when retention expires.
	_, err = st.CreateRecommendation(ctx, Recommendation{ResourceID: req.Resource.ID, PluginID: req.PluginID, ActionType: "resize", AlgorithmID: "fixture-algorithm", AlgorithmVersion: "1", EvidenceSnapshot: done.Snapshot.Records, EvidenceRefs: []string{done.Snapshot.Records[0].ID}})
	if err != nil {
		t.Fatal(err)
	}
	n, err := st.PruneCollections(ctx, later.Add(time.Hour))
	if err != nil || n != 0 {
		t.Fatalf("pruned referenced evidence=%d %v", n, err)
	}
}
func TestCollectionRetryBudgetAndAtomicFailure(t *testing.T) {
	st := NewMemory()
	ctx := context.Background()
	now := time.Now().UTC()
	st.now = func() time.Time { return now }
	req := collectionFixture(t, st)
	req.WindowEnd = now.Truncate(time.Millisecond)
	req.WindowStart = req.WindowEnd.Add(-time.Hour)
	req.MaxAttempts = 2
	job, err := st.EnqueueCollection(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	first, _ := st.ClaimCollection(ctx, job.ID, "worker", time.Minute)
	wait, err := st.FinishCollection(ctx, first, CollectionResult{ErrorCode: "provider_timeout", RetryAt: now.Add(time.Second)})
	if err != nil || wait.State != "retry_wait" {
		t.Fatal(wait, err)
	}
	if _, err = st.ClaimCollection(ctx, job.ID, "worker", time.Minute); !errors.Is(err, ErrJobChanged) {
		t.Fatal("retry was not delayed", err)
	}
	now = now.Add(2 * time.Second)
	second, _ := st.ClaimCollection(ctx, job.ID, "worker", time.Minute)
	done, err := st.FinishCollection(ctx, second, CollectionResult{ErrorCode: "provider_timeout", RetryAt: now.Add(time.Second)})
	if err != nil || done.State != "failed" {
		t.Fatal(done, err)
	}
	n, err := st.PruneCollections(ctx, now.Add(time.Hour))
	if err != nil || n != 1 {
		t.Fatal(n, err)
	}
	again, _ := st.EnqueueCollection(ctx, req)
	if !again.Pruned || again.ID != job.ID {
		t.Fatal("retention lost idempotency tombstone")
	}
	// A failed file write rolls back operation and audit changes together.
	disk, err := OpenDurable(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer disk.Close()
	request := collectionFixture(t, disk)
	priorAudits := len(disk.auditEvents)
	disk.path = filepath.Join(t.TempDir(), "missing", "state.json")
	if _, err = disk.EnqueueCollection(ctx, request); err == nil {
		t.Fatal("expected storage failure")
	}
	if len(disk.collections) != 0 || len(disk.auditEvents) != priorAudits {
		t.Fatal("failed persistence exposed partial operation/audit")
	}
}
func TestPostgresCollectionLifecycle(t *testing.T) {
	st, err := OpenPostgres(context.Background(), postgresTestURL(t))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	exerciseCollectionStore(t, st, st)
}

func TestPostgresCollectionAtomicAuditAndLeaseRecovery(t *testing.T) {
	ctx := context.Background()
	st, err := OpenPostgres(ctx, postgresTestURL(t))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	req := collectionFixture(t, st)
	job, err := st.EnqueueCollection(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	first, err := st.ClaimCollection(ctx, job.ID, "old-worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = st.pool.Exec(ctx, `UPDATE evidence_collections SET payload=jsonb_set(payload,'{lease,until}',to_jsonb(clock_timestamp()-interval '1 second')) WHERE id=$1`, job.ID); err != nil {
		t.Fatal(err)
	}
	next, err := st.ClaimCollection(ctx, job.ID, "new-worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = st.FinishCollection(ctx, first, collectionResult(t, first)); !errors.Is(err, ErrLeaseLost) {
		t.Fatal("expired owner committed", err)
	}
	if _, err = st.pool.Exec(ctx, `CREATE FUNCTION reject_collection_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.family='evidence' THEN RAISE EXCEPTION 'fixture audit failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_collection_audit BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION reject_collection_audit()`); err != nil {
		t.Fatal(err)
	}
	if _, err = st.FinishCollection(ctx, next, collectionResult(t, next)); err == nil {
		t.Fatal("audit failure should roll back evidence and completion")
	}
	stored, err := st.GetCollection(ctx, job.ID)
	if err != nil || stored.State != "running" || stored.Snapshot != nil {
		t.Fatal("partial transaction committed", stored, err)
	}
	if _, err = st.pool.Exec(ctx, `DROP TRIGGER reject_collection_audit ON audit_events`); err != nil {
		t.Fatal(err)
	}
	done, err := st.FinishCollection(ctx, next, collectionResult(t, next))
	if err != nil || done.State != "succeeded" {
		t.Fatal(done, err)
	}
}

func TestCollectionScopeAndCorruptState(t *testing.T) {
	st := NewMemory()
	req := collectionFixture(t, st)
	req.Resource.Metadata = map[string]any{"uid": "fixture-uid-1", "mapping": map[string]any{"version": "fixture-v1"}}
	one, err := st.EnqueueCollection(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	changed := clone(req)
	changed.Resource.ObservedAt = time.Now().Add(time.Second)
	if CollectionID(one) != CollectionID(Collection{Resource: changed.Resource, PluginID: one.PluginID, InstanceDigest: one.InstanceDigest, Signals: one.Signals, WindowStart: one.WindowStart, WindowEnd: one.WindowEnd}) {
		t.Fatal("observation time changed operation identity")
	}
	changed.Resource.ObservedAt = req.Resource.ObservedAt
	changed.Resource.Metadata["uid"] = "fixture-uid-2"
	two, err := st.EnqueueCollection(context.Background(), changed)
	if err != nil || two.ID == one.ID {
		t.Fatal("workload recreation was deduplicated", err)
	}
	invalid := one
	invalid.State = "unknown"
	if validateStoredCollection(invalid) == nil {
		t.Fatal("accepted corrupt collection state")
	}
}
