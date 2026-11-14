package store

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWorkerClaimFencingHeartbeatAndCancellation(t *testing.T) {
	ctx := context.Background()
	st, _, _, job := persistedWorkflow(t, filepath.Join(t.TempDir(), "state.json"))
	defer st.Close()
	now := time.Now().UTC()
	st.now = func() time.Time { return now }
	claimed, err := st.ClaimJob(ctx, job, "worker-a", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.Lease.Token != 1 || len(claimed.History) != 1 {
		t.Fatalf("missing durable claim: %+v", claimed)
	}
	if _, err := st.ClaimJob(ctx, job, "worker-b", time.Minute); !errors.Is(err, ErrJobBusy) {
		t.Fatalf("duplicate claim: %v", err)
	}
	if _, err := st.CancelJob(ctx, job.ID, "operator"); !errors.Is(err, ErrCannotCancel) {
		t.Fatalf("cancel raced through claim: %v", err)
	}
	if err := st.SaveJob(ctx, job, "stale unowned save"); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("unowned write: %v", err)
	}
	now = now.Add(30 * time.Second)
	if err := st.RenewJobLease(ctx, claimed, time.Minute); err != nil {
		t.Fatal(err)
	}
	now = now.Add(31 * time.Second)
	if _, err := st.ClaimJob(ctx, claimed, "worker-b", time.Minute); !errors.Is(err, ErrJobBusy) {
		t.Fatalf("heartbeat ignored: %v", err)
	}
	now = now.Add(30 * time.Second)
	if err := st.RenewJobLease(ctx, claimed, time.Minute); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("expired owner renewed: %v", err)
	}
	next, err := st.ClaimJob(ctx, claimed, "worker-b", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if next.Lease.Token != 2 || next.History[0].Outcome != "lease_expired" {
		t.Fatalf("takeover not recorded: %+v", next)
	}
	claimed.State = "applying"
	if err := st.SaveJob(ctx, claimed, "stale transition"); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("stale fenced write: %v", err)
	}
	if err := st.ReleaseJobLease(ctx, claimed, "completed"); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("stale release: %v", err)
	}
	if err := st.ReleaseJobLease(ctx, next, "interrupted"); err != nil {
		t.Fatal(err)
	}
	jobs, _ := st.ListJobs(ctx)
	if jobs[0].Lease != nil || jobs[0].History[1].Outcome != "interrupted" {
		t.Fatalf("release not durable: %+v", jobs[0])
	}
	if _, err := st.CancelJob(ctx, job.ID, "operator"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CancelJob(ctx, job.ID, "operator"); err != nil {
		t.Fatalf("cancel not idempotent: %v", err)
	}
}

func TestWorkerClaimsConcurrentAndWritesCAS(t *testing.T) {
	ctx := context.Background()
	st, _, _, job := persistedWorkflow(t, filepath.Join(t.TempDir(), "state.json"))
	defer st.Close()
	var winners atomic.Int32
	var winner Job
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			got, err := st.ClaimJob(ctx, job, fmt.Sprintf("worker-%d", i), time.Minute)
			if err == nil {
				winners.Add(1)
				mu.Lock()
				winner = got
				mu.Unlock()
			} else if !errors.Is(err, ErrJobBusy) && !errors.Is(err, ErrJobChanged) {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	if winners.Load() != 1 {
		t.Fatalf("claim winners=%d", winners.Load())
	}
	winner.State = "applying"
	if err := st.SaveJob(ctx, winner, "apply intent"); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveJob(ctx, winner, "duplicate old revision"); !errors.Is(err, ErrJobChanged) {
		t.Fatalf("stale revision accepted: %v", err)
	}
	if err := st.ReleaseJobLease(ctx, winner, "completed"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CancelJob(ctx, job.ID, "operator"); !errors.Is(err, ErrCannotCancel) {
		t.Fatalf("dispatched intent cancelled: %v", err)
	}
}

func TestWorkerIntentImmutableAndIllegalStateRejected(t *testing.T) {
	ctx := context.Background()
	st, _, _, job := persistedWorkflow(t, filepath.Join(t.TempDir(), "state.json"))
	defer st.Close()
	claimed, err := st.ClaimJob(ctx, job, "worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	tampered := clone(claimed)
	tampered.Plan.Diff["arbitrary"] = "mutation"
	if err := st.SaveJob(ctx, tampered, "tamper"); err == nil {
		t.Fatal("captured intent changed")
	}
	claimed.State = "verified"
	if err := st.SaveJob(ctx, claimed, "skip mutation"); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("illegal transition=%v", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	claimed.State = "applying"
	if err := st.SaveJob(cancelled, claimed, "cancelled context"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled write=%v", err)
	}
}

func TestWorkerClaimCancelRace(t *testing.T) {
	for n := 0; n < 30; n++ {
		st, _, _, job := persistedWorkflow(t, filepath.Join(t.TempDir(), "state.json"))
		start := make(chan struct{})
		var claimErr, cancelErr error
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, claimErr = st.ClaimJob(context.Background(), job, "worker", time.Minute)
		}()
		go func() {
			defer wg.Done()
			<-start
			_, cancelErr = st.CancelJob(context.Background(), job.ID, "operator")
		}()
		close(start)
		wg.Wait()
		st.Close()
		if (claimErr == nil) == (cancelErr == nil) {
			t.Fatalf("both or neither won: claim=%v cancel=%v", claimErr, cancelErr)
		}
	}
}

func TestWorkerSummarySeparatesOwnershipAndOutcomes(t *testing.T) {
	now := time.Now().UTC()
	due := now.Add(-time.Minute)
	got := SummarizeJobs([]Job{{State: "prepared", NextRun: due}, {State: "applying", Lease: &JobLease{Until: now.Add(time.Minute)}}, {State: "applying", Lease: &JobLease{Until: now.Add(-time.Second)}}, {State: "manual_intervention"}, {State: "verified"}}, now)
	if got.Queued != 2 || got.ActiveLeases != 1 || got.ExpiredLeases != 1 || got.ManualAttention != 1 || got.Completed != 1 || got.OldestDueAt == nil {
		t.Fatalf("summary=%+v", got)
	}
}
