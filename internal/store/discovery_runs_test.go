package store

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/consize-oss/consize/pkg/plugin"
	"github.com/consize-oss/consize/pkg/resource"
)

func runRequest(now time.Time) DiscoveryRun {
	return DiscoveryRun{PluginID: "fixture-discovery", Scope: map[string]string{"cluster_id": "test-cluster", "location": "local", "namespaces": "default", "label_selector": ""}, RequestedAt: now.Add(-time.Second), Actor: "fixture-operator", Trigger: "manual", MaxAttempts: 3, StaleAfter: "0s", TombstoneAfter: "1h"}
}
func runCompletion(run DiscoveryRun, now time.Time, resources ...resource.Resource) DiscoveryCompletion {
	for i := range resources {
		resources[i].SourcePluginID = run.PluginID
		resources[i].ObservedAt = now
		resources[i].Metadata = map[string]any{"namespace": "default"}
	}
	return DiscoveryCompletion{Scan: &plugin.DiscoveryScan{Version: plugin.DiscoveryScanVersion, ID: "fixture-scan", PluginID: run.PluginID, Scope: run.Scope, ScopeDigest: run.ScopeDigest, StartedAt: run.History[len(run.History)-1].StartedAt, FinishedAt: now, Complete: true, Count: len(resources)}, Resources: resources}
}
func runCycle(t *testing.T, st DiscoveryRunStore, request DiscoveryRun, now time.Time, resources ...resource.Resource) DiscoveryRun {
	t.Helper()
	ctx := context.Background()
	r, err := st.EnqueueDiscovery(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	r, err = st.ClaimDiscovery(ctx, r.ID, "fixture-worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	r, err = st.CompleteDiscovery(ctx, r, runCompletion(r, now, resources...))
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func TestDiscoveryScopedLifecycleAndFailedScans(t *testing.T) {
	ctx := context.Background()
	st := NewMemory()
	now := time.Now().UTC()
	st.now = func() time.Time { return now }
	req := runRequest(now)
	row := completeResource("fixture-resource")
	first := runCycle(t, st, req, now, row)
	if first.State != "succeeded" || len(first.ResourceIDs) != 1 {
		t.Fatal(first)
	}
	stored, _ := st.GetResource(ctx, row.ID)
	if stored.LifecycleState != resource.LifecycleActive {
		t.Fatal(stored)
	}
	// Partial scans cannot update observations, absence clocks or lifecycle.
	now = now.Add(time.Minute)
	req.RequestedAt = now.Add(-time.Second)
	bad, _ := st.EnqueueDiscovery(ctx, req)
	bad, _ = st.ClaimDiscovery(ctx, bad.ID, "worker", time.Minute)
	partial := runCompletion(bad, now)
	partial.Scan.Complete = false
	if _, err := st.CompleteDiscovery(ctx, bad, partial); !errors.Is(err, ErrDiscoveryInvalid) {
		t.Fatal("accepted partial scan", err)
	}
	if _, err := st.CompleteDiscovery(ctx, bad, DiscoveryCompletion{ErrorCode: "incomplete_scan"}); err != nil {
		t.Fatal(err)
	}
	stored, _ = st.GetResource(ctx, row.ID)
	if stored.LifecycleState != resource.LifecycleActive || stored.Metadata["discovery_absence"] != nil {
		t.Fatal("partial scan changed absence")
	}
	// Another namespace/selector authority cannot affect the original resource.
	other := req
	other.Scope = map[string]string{"cluster_id": "test-cluster", "location": "local", "namespaces": "other", "label_selector": ""}
	runCycle(t, st, other, now)
	stored, _ = st.GetResource(ctx, row.ID)
	if stored.LifecycleState != resource.LifecycleActive {
		t.Fatal("other namespace changed lifecycle")
	}
	now = now.Add(time.Minute)
	req.RequestedAt = now.Add(-time.Second)
	missing := runCycle(t, st, req, now)
	if len(missing.Reconciliation.Stale) != 1 {
		t.Fatal("absence not reconciled", missing)
	}
	// Elapsed time alone cannot tombstone: a second authoritative absence is required.
	now = now.Add(2 * time.Hour)
	req.RequestedAt = now.Add(-time.Second)
	deleted := runCycle(t, st, req, now)
	if len(deleted.Reconciliation.Deleted) != 1 {
		t.Fatal("tombstone threshold not applied", deleted)
	}
	now = now.Add(time.Minute)
	req.RequestedAt = now.Add(-time.Second)
	row.CurrentState["size"] = 20
	runCycle(t, st, req, now, row)
	stored, _ = st.GetResource(ctx, row.ID)
	if stored.LifecycleState != resource.LifecycleActive || stored.Metadata["discovery_absence"] != nil || stored.FirstSeenAt.IsZero() {
		t.Fatal("fresh upsert did not restore resource")
	}
}
func discoveryRaceContract(t *testing.T, cs DiscoveryRunStore) {
	t.Helper()
	ctx := context.Background()
	req := runRequest(time.Now().UTC())
	var id string
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := cs.EnqueueDiscovery(ctx, req)
			if err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			if id != "" && id != r.ID {
				t.Error("duplicate operation")
			}
			id = r.ID
			mu.Unlock()
		}()
	}
	wg.Wait()
	other := req
	other.RequestedAt = other.RequestedAt.Add(-time.Second)
	if _, err := cs.EnqueueDiscovery(ctx, other); !errors.Is(err, ErrDiscoveryBusy) {
		t.Fatal("overlapping scope not excluded", err)
	}
	var wins atomic.Int32
	var winner DiscoveryRun
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := cs.ClaimDiscovery(ctx, id, "worker", time.Minute)
			if err == nil {
				wins.Add(1)
				mu.Lock()
				winner = r
				mu.Unlock()
			} else if !errors.Is(err, ErrJobBusy) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatal("multiple claim winners", wins.Load())
	}
	done, err := cs.CompleteDiscovery(ctx, winner, runCompletion(winner, time.Now().UTC(), completeResource("fixture-race-resource")))
	if err != nil || done.State != "succeeded" {
		t.Fatal(done, err)
	}
	if _, err = cs.CompleteDiscovery(ctx, winner, runCompletion(winner, time.Now().UTC())); !errors.Is(err, ErrLeaseLost) {
		t.Fatal("duplicate completion succeeded", err)
	}
}
func TestMemoryDiscoveryRunRaces(t *testing.T) { discoveryRaceContract(t, NewMemory()) }
func TestPostgresDiscoveryRunRaces(t *testing.T) {
	st, err := OpenPostgres(context.Background(), postgresTestURL(t))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	discoveryRaceContract(t, st)
}
func TestDiscoveryDurableRestartAndAtomicFailure(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.json")
	st, err := OpenDurable(path)
	if err != nil {
		t.Fatal(err)
	}
	req := runRequest(time.Now().UTC())
	r, err := st.EnqueueDiscovery(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	first, err := st.ClaimDiscovery(ctx, r.ID, "crashed", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	st.Close()
	st, err = OpenDurable(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	now := time.Now().Add(2 * time.Second).UTC()
	st.now = func() time.Time { return now }
	recovered, err := st.ClaimDiscovery(ctx, r.ID, "restarted", time.Minute)
	if err != nil || recovered.Attempts != 2 || recovered.History[0].Outcome != "interrupted" {
		t.Fatal(recovered, err)
	}
	if _, err = st.CompleteDiscovery(ctx, first, DiscoveryCompletion{ErrorCode: "provider_failure"}); !errors.Is(err, ErrLeaseLost) {
		t.Fatal("expired owner committed", err)
	}
	// Force the atomic file write to fail after registry planning.
	st.path = filepath.Join(t.TempDir(), "missing", "state.json")
	priorAudits := len(st.auditEvents)
	if _, err = st.CompleteDiscovery(ctx, recovered, runCompletion(recovered, now, completeResource("fixture-recovery-resource"))); err == nil {
		t.Fatal("expected persistence failure")
	}
	if len(st.resources) != 0 || st.discoveryRuns[r.ID].State != "running" || len(st.auditEvents) != priorAudits {
		t.Fatal("partial discovery committed")
	}
}

func TestPostgresDiscoveryAtomicAuditAndFencing(t *testing.T) {
	ctx := context.Background()
	st, err := OpenPostgres(ctx, postgresTestURL(t))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	r, err := st.EnqueueDiscovery(ctx, runRequest(time.Now().UTC()))
	if err != nil {
		t.Fatal(err)
	}
	first, err := st.ClaimDiscovery(ctx, r.ID, "old-worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = st.pool.Exec(ctx, `UPDATE discovery_runs SET payload=jsonb_set(payload,'{lease,until}',to_jsonb(clock_timestamp()-interval '1 second')) WHERE id=$1`, r.ID); err != nil {
		t.Fatal(err)
	}
	next, err := st.ClaimDiscovery(ctx, r.ID, "new-worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = st.CompleteDiscovery(ctx, first, runCompletion(first, time.Now().UTC())); !errors.Is(err, ErrLeaseLost) {
		t.Fatal("expired worker committed", err)
	}
	if _, err = st.pool.Exec(ctx, `CREATE FUNCTION reject_discovery_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.family='discovery' THEN RAISE EXCEPTION 'fixture audit failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_discovery_audit BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION reject_discovery_audit()`); err != nil {
		t.Fatal(err)
	}
	completion := runCompletion(next, time.Now().UTC(), completeResource("fixture-atomic-resource"))
	if _, err = st.CompleteDiscovery(ctx, next, completion); err == nil {
		t.Fatal("audit failure did not abort registry commit")
	}
	rows, err := st.ListResources(ctx)
	if err != nil || len(rows) != 0 {
		t.Fatal("resource committed without audit", rows, err)
	}
	got, _ := st.GetDiscoveryRun(ctx, r.ID)
	if got.State != "running" {
		t.Fatal("completion committed without registry", got)
	}
	if _, err = st.pool.Exec(ctx, `DROP TRIGGER reject_discovery_audit ON audit_events`); err != nil {
		t.Fatal(err)
	}
	done, err := st.CompleteDiscovery(ctx, next, completion)
	if err != nil || done.State != "succeeded" {
		t.Fatal(done, err)
	}
}

func TestDiscoveryRetryBudgetAndCorruptState(t *testing.T) {
	st := NewMemory()
	now := time.Now().UTC()
	st.now = func() time.Time { return now }
	ctx := context.Background()
	req := runRequest(now)
	req.MaxAttempts = 2
	job, err := st.EnqueueDiscovery(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	first, err := st.ClaimDiscovery(ctx, job.ID, "worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	waiting, err := st.CompleteDiscovery(ctx, first, DiscoveryCompletion{ErrorCode: "timeout", RetryAt: now.Add(time.Second)})
	if err != nil || waiting.Health != "degraded" {
		t.Fatal(waiting, err)
	}
	if _, err = st.ClaimDiscovery(ctx, job.ID, "worker", time.Minute); !errors.Is(err, ErrJobChanged) {
		t.Fatal("retried before next_run", err)
	}
	now = now.Add(2 * time.Second)
	last, err := st.ClaimDiscovery(ctx, job.ID, "worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	failed, err := st.CompleteDiscovery(ctx, last, DiscoveryCompletion{ErrorCode: "timeout", RetryAt: now.Add(time.Second)})
	if err != nil || failed.State != "failed" || failed.Health != "failed" {
		t.Fatal(failed, err)
	}
	if validateDiscoveryRun(failed) != nil {
		t.Fatal("valid run cannot be restored")
	}
	failed.State = "invented"
	if validateDiscoveryRun(failed) == nil {
		t.Fatal("accepted corrupt state")
	}
}
