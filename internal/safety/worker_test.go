package safety

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/consize-oss/consize/internal/bootstrap"
	"github.com/consize-oss/consize/internal/policy"
	"github.com/consize-oss/consize/internal/store"
	"github.com/consize-oss/consize/pkg/plugin"
)

type gatedAction struct {
	*testAction
	entered  chan struct{}
	resume   chan struct{}
	once     sync.Once
	inspects atomic.Int32
}

type failedHeartbeatStore struct{ *store.Memory }

func (s failedHeartbeatStore) RenewJobLease(context.Context, store.Job, time.Duration) error {
	return store.ErrLeaseLost
}

func TestWorkerCancelsProviderWhenHeartbeatOwnershipIsLost(t *testing.T) {
	h := newHarness(t)
	h.submit(t)
	action := &gatedAction{testAction: h.action, entered: make(chan struct{}), resume: make(chan struct{})}
	manager := plugin.NewManager()
	_ = manager.RegisterAction(action)
	_ = manager.RegisterMetrics(h.metrics)
	c := New(failedHeartbeatStore{h.st}, manager, policy.NewEngine(), h.c.cfg)
	if err := c.ConfigureWorker(bootstrap.WorkerConfig{LeaseDuration: "3s", HeartbeatInterval: "100ms", StepTimeout: "1s"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Tick(context.Background()); !errors.Is(err, store.ErrLeaseLost) {
		t.Fatalf("ownership loss=%v", err)
	}
	if h.action.applies != 0 || h.job(t).State != "prepared" {
		t.Fatal("ownership loss crossed dispatch boundary")
	}
}

func TestWorkerConfigurationFailsClosed(t *testing.T) {
	h := newHarness(t)
	for _, cfg := range []bootstrap.WorkerConfig{{PollInterval: "0s"}, {LeaseDuration: "2s"}, {HeartbeatInterval: "1m"}, {StepTimeout: "1m"}, {MaxAttempts: -1}, {MaxJobsPerTick: 129}} {
		if err := h.c.ConfigureWorker(cfg); err == nil {
			t.Fatalf("invalid options accepted: %+v", cfg)
		}
		if h.c.leaseDuration != time.Minute || h.c.maxAttempts != 8 {
			t.Fatal("invalid options partially activated")
		}
	}
}

func TestOperatorRecoveryUsesFencedClaimAndUpdatesLinkedState(t *testing.T) {
	h := newHarness(t)
	h.submit(t)
	job := h.job(t)
	job.Attempts = h.c.maxAttempts - 1
	if err := h.c.retry(h.ctx, job, errors.New("temporary provider failure")); err != nil {
		t.Fatal(err)
	}
	current := h.job(t)
	claimed, err := h.st.ClaimJob(h.ctx, current, "other-recovery", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.c.Recover(h.ctx, h.id, "operator"); !errors.Is(err, store.ErrJobBusy) {
		t.Fatalf("concurrent recovery=%v", err)
	}
	if err := h.st.ReleaseJobLease(h.ctx, claimed, "interrupted"); err != nil {
		t.Fatal(err)
	}
	recovered, err := h.c.Recover(h.ctx, h.id, "operator")
	if err != nil {
		t.Fatal(err)
	}
	if recovered.State != "rollback_pending" || recovered.Lease != nil {
		t.Fatalf("stale recovery response=%+v", recovered)
	}
	action, err := h.st.GetAction(h.ctx, recovered.ActionID)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := h.st.GetRecommendation(h.ctx, h.id)
	if err != nil {
		t.Fatal(err)
	}
	if action.Status != store.ActionRollbackPending || rec.Status != store.RecommendationExecuting {
		t.Fatalf("linked states: action=%s rec=%s", action.Status, rec.Status)
	}
}

func TestWorkerRestartRecoversExpiredClaimAtEveryPhase(t *testing.T) {
	for _, phase := range []string{"prepared", "applying", "waiting_rollout", "verifying", "rollback_pending", "rolling_back", "rollback_verifying"} {
		t.Run(phase, func(t *testing.T) {
			h := newHarness(t)
			rollback := phase == "rollback_pending" || phase == "rolling_back" || phase == "rollback_verifying"
			h.metrics.failed = rollback
			h.submit(t)
			switch phase {
			case "applying":
				h.tick(t)
			case "waiting_rollout":
				h.tick(t)
				h.tick(t)
			case "verifying":
				h.toWindow(t)
			case "rollback_pending", "rolling_back", "rollback_verifying":
				h.toWindow(t)
				h.observe(t)
				if phase != "rollback_pending" {
					h.tick(t)
				}
				if phase == "rollback_verifying" {
					h.toWindow(t)
				}
			}
			job := h.job(t)
			if job.State != phase {
				t.Fatalf("fixture phase=%s want=%s", job.State, phase)
			}
			stale, err := h.st.ClaimJob(h.ctx, job, "crashed-worker", time.Second)
			if err != nil {
				t.Fatal(err)
			}
			if err := h.st.Close(); err != nil {
				t.Fatal(err)
			}
			h.st, err = store.OpenDurable(h.path)
			if err != nil {
				t.Fatal(err)
			}
			h.rebuild()
			// A still-valid predecessor lease must not be stolen on startup.
			if _, err := h.st.ClaimJob(h.ctx, stale, "replacement", time.Second); !errors.Is(err, store.ErrJobBusy) {
				t.Fatalf("live lease stolen: %v", err)
			}
			time.Sleep(1050 * time.Millisecond)
			h.toWindow(t)
			h.observe(t)
			want := "verified"
			if rollback {
				want = "rolled_back"
			}
			final := h.job(t)
			if final.State != want || final.Lease != nil {
				t.Fatalf("recovery=%s want=%s", final.State, want)
			}
			if h.action.applies != 1 || rollback && h.action.rollbacks != 1 {
				t.Fatalf("duplicated effect: apply=%d rollback=%d", h.action.applies, h.action.rollbacks)
			}
			if err := h.st.SaveJob(h.ctx, stale, "late old process"); !errors.Is(err, store.ErrLeaseLost) {
				t.Fatalf("crashed owner not fenced: %v", err)
			}
		})
	}
}

func (a *gatedAction) Inspect(ctx context.Context, plan plugin.ActionPlan) (string, error) {
	a.inspects.Add(1)
	a.once.Do(func() { close(a.entered) })
	select {
	case <-a.resume:
		return a.testAction.Inspect(ctx, plan)
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func TestTwoControllersCannotEnterOneProviderPhase(t *testing.T) {
	h := newHarness(t)
	h.submit(t)
	action := &gatedAction{testAction: h.action, entered: make(chan struct{}), resume: make(chan struct{})}
	manager := plugin.NewManager()
	if err := manager.RegisterAction(action); err != nil {
		t.Fatal(err)
	}
	if err := manager.RegisterMetrics(h.metrics); err != nil {
		t.Fatal(err)
	}
	a := New(h.st, manager, policy.NewEngine(), h.c.cfg)
	b := New(h.st, manager, policy.NewEngine(), h.c.cfg)
	var wg sync.WaitGroup
	wg.Add(1)
	firstErr := make(chan error, 1)
	go func() { defer wg.Done(); firstErr <- a.Tick(context.Background()) }()
	select {
	case <-action.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("first worker did not enter")
	}
	if err := b.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if action.inspects.Load() != 1 {
		t.Fatalf("second worker reached provider: %d", action.inspects.Load())
	}
	close(action.resume)
	wg.Wait()
	if err := <-firstErr; err != nil {
		t.Fatal(err)
	}
	if len(h.job(t).History) != 1 {
		t.Fatal("duplicate claim history")
	}
}

func TestWorkerShutdownReleasesClaimWithoutInventingSuccess(t *testing.T) {
	h := newHarness(t)
	h.submit(t)
	action := &gatedAction{testAction: h.action, entered: make(chan struct{}), resume: make(chan struct{})}
	manager := plugin.NewManager()
	_ = manager.RegisterAction(action)
	_ = manager.RegisterMetrics(h.metrics)
	c := New(h.st, manager, policy.NewEngine(), h.c.cfg)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.Tick(ctx) }()
	select {
	case <-action.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not enter")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not drain")
	}
	job := h.job(t)
	if job.Lease != nil || job.State != "prepared" || job.History[0].Outcome != "interrupted" || h.action.applies != 0 {
		t.Fatalf("shutdown=%+v", job)
	}
}

func TestDispatchStopsOnExpiredBaselineOrTamperedIntent(t *testing.T) {
	for _, name := range []string{"stale-baseline", "tampered-plan"} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.submit(t)
			if name == "stale-baseline" {
				h.now = h.now.Add(3 * time.Minute)
			} else {
				job := h.job(t)
				job.Plan.Diff["target"] = 1
				// Verify the dispatch boundary independently of the store's immutable-intent guard.
				if err := h.c.revalidateDispatch(h.ctx, job, h.action); err == nil {
					t.Fatal("tampered digest passed")
				}
				return
			}
			h.tick(t)
			h.tick(t)
			if h.action.applies != 0 || h.job(t).State != "cancelled" {
				t.Fatalf("expired baseline dispatched: %+v", h.job(t))
			}
		})
	}
}

func TestWorkerBackoffBoundedAndErrorsSanitized(t *testing.T) {
	for i := 1; i <= 8; i++ {
		delay := retryDelay(1, i)
		if delay < 5*time.Second || delay > 200*time.Second {
			t.Fatalf("bad delay %v", delay)
		}
		if delay != retryDelay(1, i) {
			t.Fatal("unstable restart jitter")
		}
	}
	h := newHarness(t)
	h.submit(t)
	job := h.job(t)
	job.Attempts = 7
	if err := h.c.retry(h.ctx, job, errors.New("Bearer live-token-value https://user:password@provider")); err != nil {
		t.Fatal(err)
	}
	got := h.job(t)
	if got.State != "manual_intervention" || got.LastError == "" {
		t.Fatalf("retry cap=%+v", got)
	}
	if got.LastError == "Bearer live-token-value https://user:password@provider" {
		t.Fatal("provider secret leaked")
	}
}

func TestWorkerAttemptTimelineSurvivesFullRecovery(t *testing.T) {
	h := newHarness(t)
	h.action.lostAck = true
	h.submit(t)
	h.toWindow(t)
	h.observe(t)
	job := h.job(t)
	if job.State != "verified" || job.DispatchID == "" || job.PlanDigest == "" || job.Lease != nil {
		t.Fatalf("missing intent/outcome: %+v", job)
	}
	if h.action.applies != 1 {
		t.Fatal("ambiguous write duplicated")
	}
	for _, attempt := range job.History {
		if attempt.Epoch == 0 || attempt.Worker == "" || attempt.FinishedAt.IsZero() {
			t.Fatalf("incomplete attempt: %+v", attempt)
		}
	}
	if job.Plan.Execution == nil || job.Plan.Execution.DispatchID != job.DispatchID {
		t.Fatal("plugin did not receive constrained dispatch identity")
	}
	if summary := store.SummarizeJobs([]store.Job{job}, h.now); summary.Completed != 1 || summary.ActiveLeases != 0 {
		t.Fatalf("summary=%+v", summary)
	}
}
