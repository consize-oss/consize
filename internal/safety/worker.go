package safety

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/consize-oss/consize/internal/bootstrap"
	"github.com/consize-oss/consize/internal/policy"
	"github.com/consize-oss/consize/internal/store"
	"github.com/consize-oss/consize/pkg/plugin"
)

func randomWorkerID() string {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		panic("cannot create worker identity")
	}
	return "worker-" + hex.EncodeToString(id[:])
}

func digestPlan(plan plugin.ActionPlan) string {
	plan.Execution = nil
	plan.Preflight = nil // live checks are refreshed; the approved mutation must remain unchanged
	data, err := json.Marshal(plan)
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func (c *Controller) runClaimed(parent context.Context, job store.Job) error {
	job.Plan.Execution = &plugin.ExecutionEnvelope{DispatchID: job.DispatchID, PlanDigest: job.PlanDigest, AttemptToken: job.Lease.Token, LeaseUntil: job.Lease.Until}
	ctx, cancel := context.WithTimeout(parent, c.stepTimeout)
	finished := make(chan struct{})
	heartbeat := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(c.heartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-finished:
				heartbeat <- nil
				return
			case <-ctx.Done():
				heartbeat <- nil
				return
			case <-ticker.C:
				if err := c.st.RenewJobLease(ctx, job, c.leaseDuration); err != nil {
					cancel()
					heartbeat <- err
					return
				}
			}
		}
	}()
	err := c.step(ctx, job)
	close(finished)
	hbErr := <-heartbeat
	interrupted := ctx.Err() != nil
	cancel()
	outcome := "completed"
	if interrupted || err != nil || hbErr != nil {
		outcome = "interrupted"
	}
	slog.Info("worker phase finished", "worker_id", c.workerID, "job_id", job.ID, "action_id", job.ActionID, "dispatch_id", job.DispatchID, "attempt_token", job.Lease.Token, "phase", job.State, "outcome", outcome)
	releaseCtx, releaseCancel := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
	defer releaseCancel()
	releaseErr := c.st.ReleaseJobLease(releaseCtx, job, outcome)
	if hbErr != nil {
		return hbErr
	}
	if err != nil {
		return err
	}
	if releaseErr != nil && !errors.Is(releaseErr, store.ErrLeaseLost) {
		return releaseErr
	}
	return nil
}

// This is deliberately in addition to provider preflight and the store's fencing checks.
func (c *Controller) revalidateDispatch(ctx context.Context, job store.Job, action plugin.ActionPlugin) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if job.PlanDigest == "" || digestPlan(job.Plan) != job.PlanDigest {
		return errors.New("plan digest mismatch")
	}
	if job.DispatchID == "" {
		return errors.New("dispatch identity missing")
	}
	if c.now().Sub(job.Baseline.CollectedAt) > 2*time.Minute {
		return errors.New("baseline expired before dispatch")
	}
	rec, err := c.st.GetRecommendation(ctx, job.RecommendationID)
	if err != nil {
		return err
	}
	if rec.Status != store.RecommendationApproved && rec.Status != store.RecommendationExecuting {
		return errors.New("recommendation is no longer authorized")
	}
	if !rec.ExpiresAt.IsZero() && !c.now().Before(rec.ExpiresAt) {
		return errors.New("recommendation expired")
	}
	if rec.ResourceID != job.Resource.ID || rec.PluginID != job.Plan.PluginID || rec.ActionType != job.Plan.ActionType {
		return errors.New("recommendation changed")
	}
	res, err := c.st.GetResource(ctx, job.Resource.ID)
	if err != nil {
		return err
	}
	decision := c.policies.Evaluate(ctx, res, action.Manifest(), "approved", job.Actor)
	if decision.Decision != policy.DecisionApproved || decision.PolicyID != job.Policy.PolicyID {
		return errors.New("policy no longer permits approved dispatch")
	}
	return matchingRelease(job.Plan, action)
}

func retryClass(err error, state string) string {
	if errors.Is(err, context.Canceled) {
		return "interrupted"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, plugin.ErrNotFound) || errors.Is(err, plugin.ErrCapabilityMissing) {
		return "plugin_unavailable"
	}
	if state == "applying" || state == "rolling_back" {
		return "unknown_outcome"
	}
	return "provider_unavailable"
}

func retryMessage(code string) string {
	switch code {
	case "interrupted":
		return "Worker interrupted; persisted intent requires reconciliation"
	case "timeout":
		return "Provider deadline exceeded; inspect current state before retry"
	case "plugin_unavailable":
		return "Required plugin capability is unavailable"
	case "unknown_outcome":
		return "Provider acknowledgement is uncertain; reconcile live state before retry"
	default:
		return "Provider evidence or state is unavailable; retry within the fixed deadline"
	}
}

// Stable jitter is reproducible after restart and spreads different jobs without global RNG state.
func retryDelay(id int64, attempt int) time.Duration {
	base := 5 * time.Second * time.Duration(1<<min(max(attempt-1, 0), 5))
	h := sha256.Sum256([]byte(fmt.Sprintf("%d:%d", id, attempt)))
	return base + time.Duration(h[0])*base/1024
}

// ConfigureWorker is called before starting the controller. Invalid options never partially activate.
func (c *Controller) ConfigureWorker(cfg bootstrap.WorkerConfig) error {
	poll, lease, heartbeat, step := c.pollInterval, c.leaseDuration, c.heartbeatInterval, c.stepTimeout
	for _, field := range []struct {
		raw    string
		target *time.Duration
	}{{cfg.PollInterval, &poll}, {cfg.LeaseDuration, &lease}, {cfg.HeartbeatInterval, &heartbeat}, {cfg.StepTimeout, &step}} {
		if field.raw != "" {
			value, err := time.ParseDuration(field.raw)
			if err != nil {
				return errors.New("invalid worker duration")
			}
			*field.target = value
		}
	}
	attempts, batch := c.maxAttempts, c.maxJobsPerTick
	if cfg.MaxAttempts != 0 {
		attempts = cfg.MaxAttempts
	}
	if cfg.MaxJobsPerTick != 0 {
		batch = cfg.MaxJobsPerTick
	}
	if poll < 100*time.Millisecond || poll > time.Minute || lease < 3*time.Second || lease > 5*time.Minute || heartbeat < 100*time.Millisecond || heartbeat > lease/3 || step <= 0 || step > lease/2 || attempts < 1 || attempts > 32 || batch < 1 || batch > 128 {
		return errors.New("invalid worker bounds: poll 100ms–1m, lease 3s–5m, heartbeat <= lease/3, step <= lease/2, attempts 1–32, batch 1–128")
	}
	c.pollInterval, c.leaseDuration, c.heartbeatInterval, c.stepTimeout = poll, lease, heartbeat, step
	c.maxAttempts, c.maxJobsPerTick = attempts, batch
	return nil
}
