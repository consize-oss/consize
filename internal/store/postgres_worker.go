package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// The row lock covers only the claim/state transaction, never a provider call.
// Lease comparisons use the database clock so worker host clock skew cannot extend ownership.
func (p *Postgres) updateWorkerJob(ctx context.Context, id int64, mutate func(Job, time.Time) (Job, string, error)) (Job, error) {
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Job{}, err
	}
	defer tx.Rollback(ctx)
	existing, err := getJSONTx[Job](ctx, tx, nil, `SELECT payload FROM jobs WHERE id=$1 FOR UPDATE`, id)
	if err != nil {
		return Job{}, err
	}
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return Job{}, err
	}
	job, message, err := mutate(existing, now.UTC())
	if err != nil {
		return Job{}, err
	}
	payload, err := marshal(job)
	if err != nil {
		return Job{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE jobs SET state=$2,next_run=$3,payload=$4,updated_at=$5 WHERE id=$1`, id, job.State, job.NextRun, payload, job.UpdatedAt); err != nil {
		return Job{}, err
	}
	if message != "" {
		rec, err := getJSONTx[Recommendation](ctx, tx, nil, `SELECT payload FROM recommendations WHERE id=$1 FOR UPDATE`, job.RecommendationID)
		if err != nil {
			return Job{}, err
		}
		action, err := getJSONTx[Action](ctx, tx, nil, `SELECT payload FROM actions WHERE id=$1 FOR UPDATE`, job.ActionID)
		if err != nil {
			return Job{}, err
		}
		if err = p.persistJobEffects(ctx, tx, job, message, rec, action); err != nil {
			return Job{}, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return Job{}, err
	}
	return job, nil
}

func (p *Postgres) ClaimJob(ctx context.Context, candidate Job, owner string, ttl time.Duration) (Job, error) {
	return p.updateWorkerJob(ctx, candidate.ID, func(existing Job, now time.Time) (Job, string, error) {
		job, err := claimJob(existing, candidate, owner, ttl, now)
		if err != nil {
			return Job{}, "", err
		}
		return job, leaseMessage(job), nil
	})
}

func (p *Postgres) RenewJobLease(ctx context.Context, candidate Job, ttl time.Duration) error {
	_, err := p.updateWorkerJob(ctx, candidate.ID, func(existing Job, now time.Time) (Job, string, error) {
		if !ownsLease(existing, candidate, now) {
			return Job{}, "", ErrLeaseLost
		}
		if err := validateLeaseOptions(candidate.Lease.Owner, ttl); err != nil {
			return Job{}, "", err
		}
		existing.Lease.Until = now.Add(ttl)
		return existing, "", nil
	})
	return err
}

func (p *Postgres) ReleaseJobLease(ctx context.Context, candidate Job, outcome string) error {
	_, err := p.updateWorkerJob(ctx, candidate.ID, func(existing Job, now time.Time) (Job, string, error) {
		job, err := releaseJob(existing, candidate, outcome, now)
		return job, "Worker phase attempt finished: " + outcome, err
	})
	return err
}

func (p *Postgres) CancelJob(ctx context.Context, id int64, actor string) (Job, error) {
	return p.updateWorkerJob(ctx, id, func(job Job, now time.Time) (Job, string, error) {
		if actor == "" {
			return Job{}, "", errors.New("authenticated actor is required")
		}
		if job.State == "cancelled" {
			return job, "", nil
		}
		if job.State != "prepared" || (job.Lease != nil && now.Before(job.Lease.Until)) {
			return Job{}, "", ErrCannotCancel
		}
		if job.Lease != nil && len(job.History) > 0 && job.History[len(job.History)-1].FinishedAt.IsZero() {
			job.History[len(job.History)-1].FinishedAt = now
			job.History[len(job.History)-1].Outcome = "cancelled_before_dispatch"
		}
		job.State, job.Actor, job.Lease = "cancelled", actor, nil
		job.Version++
		job.UpdatedAt = now
		return job, "Operator cancelled before dispatch", nil
	})
}
