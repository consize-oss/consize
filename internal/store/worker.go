package store

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"
)

var (
	ErrJobBusy      = errors.New("job is owned by another worker")
	ErrLeaseLost    = errors.New("worker lease expired or was replaced")
	ErrJobChanged   = errors.New("job changed; reload its current state")
	ErrCannotCancel = errors.New("only an unclaimed prepared job can be cancelled")
)

// Token increases on every claim. A process ID alone cannot fence a restarted worker.
type JobLease struct {
	Owner string    `json:"owner"`
	Token uint64    `json:"token"`
	Until time.Time `json:"until"`
}

// Each claim is one bounded phase attempt, not another logical action.
type JobAttempt struct {
	Token      uint64    `json:"token"`
	Worker     string    `json:"worker"`
	Phase      string    `json:"phase"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at,omitempty"`
	Outcome    string    `json:"outcome,omitempty"`
}

func validateLeaseOptions(owner string, ttl time.Duration) error {
	if owner == "" || len(owner) > 128 || ttl < time.Second || ttl > 5*time.Minute {
		return errors.New("worker owner and lease duration (1s to 5m) are required")
	}
	return nil
}

func ownsLease(existing, candidate Job, now time.Time) bool {
	return existing.Lease != nil && candidate.Lease != nil &&
		existing.Lease.Owner == candidate.Lease.Owner && existing.Lease.Token == candidate.Lease.Token &&
		now.Before(existing.Lease.Until)
}

func claimJob(existing, candidate Job, owner string, ttl time.Duration, now time.Time) (Job, error) {
	if err := validateLeaseOptions(owner, ttl); err != nil {
		return Job{}, err
	}
	if Terminal(existing.State) && existing.State != "manual_intervention" {
		return Job{}, ErrJobChanged
	}
	if existing.Lease != nil && now.Before(existing.Lease.Until) {
		return Job{}, ErrJobBusy
	}
	if existing.Version != candidate.Version {
		return Job{}, ErrJobChanged
	}
	if n := len(existing.History); n > 0 && existing.History[n-1].FinishedAt.IsZero() {
		existing.History[n-1].FinishedAt = now
		existing.History[n-1].Outcome = "lease_expired"
	}
	existing.Fence++
	existing.Version++
	existing.Lease = &JobLease{Owner: owner, Token: existing.Fence, Until: now.Add(ttl)}
	existing.History = append(existing.History, JobAttempt{Token: existing.Fence, Worker: owner, Phase: existing.State, StartedAt: now})
	existing.UpdatedAt = now
	return existing, nil
}

func validateJobWrite(existing, candidate Job, now time.Time) error {
	if existing.Lease != nil || candidate.Lease != nil {
		if !ownsLease(existing, candidate, now) {
			return ErrLeaseLost
		}
	}
	if existing.Version != candidate.Version {
		return ErrJobChanged
	}
	before, after := existing.Plan, candidate.Plan
	before.Preflight, after.Preflight = nil, nil
	before.Execution, after.Execution = nil, nil
	if !reflect.DeepEqual(before, after) || candidate.PlanDigest != existing.PlanDigest || candidate.DispatchID != existing.DispatchID {
		return errors.New("captured dispatch intent cannot be changed")
	}
	allowed := map[string]map[string]bool{
		"prepared":            {"applying": true, "cancelled": true, "manual_intervention": true, "rollback_pending": true},
		"applying":            {"waiting_rollout": true, "cancelled": true, "rollback_pending": true, "manual_intervention": true},
		"waiting_rollout":     {"verifying": true, "rollback_pending": true, "manual_intervention": true},
		"verifying":           {"verified": true, "rollback_pending": true, "manual_intervention": true},
		"rollback_pending":    {"rolling_back": true, "manual_intervention": true},
		"rolling_back":        {"rollback_verifying": true, "manual_intervention": true},
		"rollback_verifying":  {"rolled_back": true, "manual_intervention": true},
		"manual_intervention": {"rollback_pending": true},
	}
	if candidate.State != existing.State && !allowed[existing.State][candidate.State] {
		return ErrInvalidTransition
	}
	return nil
}

func preserveWorkerFields(candidate *Job, existing Job) {
	candidate.Fence, candidate.Lease, candidate.History = existing.Fence, existing.Lease, existing.History
}

func releaseJob(existing, candidate Job, outcome string, now time.Time) (Job, error) {
	if !ownsLease(existing, candidate, now) {
		return Job{}, ErrLeaseLost
	}
	if len(outcome) > 64 {
		return Job{}, errors.New("invalid attempt outcome")
	}
	n := len(existing.History)
	if n == 0 || existing.History[n-1].Token != candidate.Lease.Token {
		return Job{}, ErrLeaseLost
	}
	existing.History[n-1].FinishedAt = now
	if outcome == "completed" && existing.FailureCode != "" {
		outcome = existing.FailureCode
	}
	existing.History[n-1].Outcome = outcome
	existing.Lease = nil
	existing.Version++
	existing.UpdatedAt = now
	return existing, nil
}

func (m *Memory) ClaimJob(ctx context.Context, candidate Job, owner string, ttl time.Duration) (Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Job{}, err
	}
	if m.poison != nil {
		return Job{}, m.poison
	}
	existing, ok := m.jobs[candidate.ID]
	if !ok {
		return Job{}, ErrNotFound
	}
	job, err := claimJob(clone(existing), candidate, owner, ttl, m.now().UTC())
	if err != nil {
		return Job{}, err
	}
	m.jobs[job.ID] = clone(job)
	m.jobEventLocked(job, "Worker claimed phase "+job.State)
	return clone(job), m.persistLocked()
}

func (m *Memory) RenewJobLease(ctx context.Context, candidate Job, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.poison != nil {
		return m.poison
	}
	existing, ok := m.jobs[candidate.ID]
	if !ok {
		return ErrNotFound
	}
	if !ownsLease(existing, candidate, m.now()) {
		return ErrLeaseLost
	}
	if err := validateLeaseOptions(candidate.Lease.Owner, ttl); err != nil {
		return err
	}
	existing.Lease.Until = m.now().UTC().Add(ttl)
	m.jobs[existing.ID] = existing
	return m.persistLocked()
}

func (m *Memory) ReleaseJobLease(ctx context.Context, candidate Job, outcome string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.poison != nil {
		return m.poison
	}
	existing, ok := m.jobs[candidate.ID]
	if !ok {
		return ErrNotFound
	}
	job, err := releaseJob(clone(existing), candidate, outcome, m.now().UTC())
	if err != nil {
		return err
	}
	m.jobs[job.ID] = clone(job)
	m.jobEventLocked(job, "Worker phase attempt finished: "+outcome)
	return m.persistLocked()
}

func (m *Memory) CancelJob(ctx context.Context, id int64, actor string) (Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Job{}, err
	}
	if m.poison != nil {
		return Job{}, m.poison
	}
	job, ok := m.jobs[id]
	if !ok {
		return Job{}, ErrNotFound
	}
	if actor == "" {
		return Job{}, errors.New("authenticated actor is required")
	}
	if job.State == "cancelled" {
		return clone(job), nil
	}
	if job.State != "prepared" || (job.Lease != nil && m.now().Before(job.Lease.Until)) {
		return Job{}, ErrCannotCancel
	}
	if job.Lease != nil && len(job.History) > 0 && job.History[len(job.History)-1].FinishedAt.IsZero() {
		job.History[len(job.History)-1].FinishedAt = m.now().UTC()
		job.History[len(job.History)-1].Outcome = "cancelled_before_dispatch"
	}
	job.State, job.Actor, job.Lease = "cancelled", actor, nil
	job.Version++
	job.UpdatedAt = m.now().UTC()
	m.jobs[id] = clone(job)
	delete(m.activeJobByResource, job.Resource.ID)
	m.jobEventLocked(job, "Operator cancelled before dispatch")
	return clone(job), m.persistLocked()
}

// WorkerSummary reports durable queue state, not proof that an idle remote process is alive.
type WorkerSummary struct {
	Queued          int        `json:"queued"`
	ActiveLeases    int        `json:"active_leases"`
	ExpiredLeases   int        `json:"expired_leases"`
	ManualAttention int        `json:"manual_attention"`
	Completed       int        `json:"completed"`
	Attempts        int        `json:"attempts"`
	OldestDueAt     *time.Time `json:"oldest_due_at,omitempty"`
}

func SummarizeJobs(jobs []Job, now time.Time) WorkerSummary {
	var out WorkerSummary
	for _, job := range jobs {
		out.Attempts += len(job.History)
		if job.State == "manual_intervention" {
			out.ManualAttention++
			continue
		}
		if Terminal(job.State) {
			out.Completed++
			continue
		}
		if job.Lease != nil {
			if now.Before(job.Lease.Until) {
				out.ActiveLeases++
				continue
			}
			out.ExpiredLeases++
		}
		out.Queued++
		if !job.NextRun.After(now) && (out.OldestDueAt == nil || job.NextRun.Before(*out.OldestDueAt)) {
			t := job.NextRun
			out.OldestDueAt = &t
		}
	}
	return out
}

func leaseMessage(job Job) string {
	return fmt.Sprintf("Worker %s claimed phase %s", job.Lease.Owner, job.State)
}
