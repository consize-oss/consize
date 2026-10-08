package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/consize-oss/consize/pkg/resource"
	"github.com/jackc/pgx/v5"
)

func (p *Postgres) CreateJob(ctx context.Context, job Job) (Job, error) {
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Job{}, err
	}
	defer tx.Rollback(ctx)
	if existing, err := getJSONTx[Job](ctx, tx, nil, `SELECT payload FROM jobs WHERE id=$1`, job.ID); err == nil {
		return existing, nil
	} else if !errors.Is(err, ErrNotFound) {
		return Job{}, err
	}
	if job.RecommendationID == 0 {
		job.RecommendationID = job.ID
	}
	rec, err := getJSONTx[Recommendation](ctx, tx, nil, `SELECT payload FROM recommendations WHERE id=$1 FOR UPDATE`, job.RecommendationID)
	if err != nil {
		return Job{}, err
	}
	if rec.Status != RecommendationPending && rec.Status != RecommendationPlanned {
		return Job{}, errors.New("recommendation is not actionable")
	}
	action, err := getJSONTx[Action](ctx, tx, nil, `SELECT payload FROM actions WHERE id=$1 FOR UPDATE`, job.ActionID)
	if err != nil || action.RecommendationID != job.RecommendationID {
		return Job{}, errors.New("job action does not exist or belongs to another recommendation")
	}
	if job.Resource.ID != rec.ResourceID || job.Plan.ResourceID != rec.ResourceID || job.Plan.PluginID != rec.PluginID || job.Plan.ActionType != rec.ActionType {
		return Job{}, errors.New("job resource or plan does not match its recommendation")
	}
	job.State, job.UpdatedAt, job.NextRun = "prepared", p.now().UTC(), p.now().UTC()
	payload, _ := marshal(job)
	if _, err := tx.Exec(ctx, `INSERT INTO jobs(id,action_id,recommendation_id,resource_id,state,next_run,payload,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, job.ID, job.ActionID, job.RecommendationID, job.Resource.ID, job.State, job.NextRun, payload, job.UpdatedAt); err != nil {
		return Job{}, fmt.Errorf("create durable job: %w", err)
	}
	if err := p.persistJobEffects(ctx, tx, job, "Durable action prepared before mutation", rec, action); err != nil {
		return Job{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Job{}, err
	}
	return job, nil
}

func (p *Postgres) SaveJob(ctx context.Context, job Job, message string) error {
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	existing, err := getJSONTx[Job](ctx, tx, nil, `SELECT payload FROM jobs WHERE id=$1 FOR UPDATE`, job.ID)
	if err != nil {
		return err
	}
	if job.ID != existing.ID || job.ActionID != existing.ActionID || job.RecommendationID != existing.RecommendationID || job.Resource.ID != existing.Resource.ID {
		return errors.New("immutable job identity cannot be changed")
	}
	rec, err := getJSONTx[Recommendation](ctx, tx, nil, `SELECT payload FROM recommendations WHERE id=$1 FOR UPDATE`, job.RecommendationID)
	if err != nil {
		return err
	}
	action, err := getJSONTx[Action](ctx, tx, nil, `SELECT payload FROM actions WHERE id=$1 FOR UPDATE`, job.ActionID)
	if err != nil {
		return err
	}
	job.UpdatedAt = p.now().UTC()
	payload, _ := marshal(job)
	if _, err := tx.Exec(ctx, `UPDATE jobs SET state=$2,next_run=$3,payload=$4,updated_at=$5 WHERE id=$1`, job.ID, job.State, job.NextRun, payload, job.UpdatedAt); err != nil {
		return err
	}
	if err := p.persistJobEffects(ctx, tx, job, message, rec, action); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (p *Postgres) persistJobEffects(ctx context.Context, tx pgx.Tx, job Job, message string, rec Recommendation, action Action) error {
	now := p.now().UTC()
	event := ActionEvent{ActionID: job.ActionID, RecommendationID: job.RecommendationID, ResourceID: job.Resource.ID, PluginID: job.Plan.PluginID, Actor: job.Actor, Mode: "approved", Result: job.State, Message: message, PolicyDecision: job.Policy, CreatedAt: now, Plan: &job.Plan, VerificationResult: job.Result}
	if err := tx.QueryRow(ctx, `INSERT INTO action_events(action_id,recommendation_id,resource_id,payload,created_at) VALUES($1,$2,$3,'{}',$4) RETURNING id`, event.ActionID, event.RecommendationID, event.ResourceID, now).Scan(&event.ID); err != nil {
		return err
	}
	eventPayload, _ := marshal(event)
	if _, err := tx.Exec(ctx, `UPDATE action_events SET payload=$2 WHERE id=$1`, event.ID, eventPayload); err != nil {
		return err
	}
	if next := recommendationStatusForJob(job.State); next != "" && ValidRecommendationTransition(rec.Status, next) {
		rec.Status = next
	}
	rec.PolicyID, rec.UpdatedAt = job.Policy.PolicyID, now
	recPayload, _ := marshal(rec)
	if _, err := tx.Exec(ctx, `UPDATE recommendations SET status=$2,payload=$3,updated_at=$4 WHERE id=$1`, rec.ID, rec.Status, recPayload, now); err != nil {
		return err
	}
	if next := actionStatusForJob(job.State); next != "" && ValidActionTransition(action.Status, next) {
		action.Status, action.UpdatedAt = next, now
		if next == ActionExecuting && action.StartedAt.IsZero() {
			action.StartedAt = now
		}
		if ActionTerminal(next) {
			action.FinishedAt = now
		}
		if next == ActionFailed || next == ActionManualIntervention {
			action.FailureMessage = message
		}
		if job.PluginResult != nil {
			action.ExecutionResult = &ExecutionResult{Result: *job.PluginResult, StartedAt: action.StartedAt, FinishedAt: action.FinishedAt}
		}
		actionPayload, _ := marshal(action)
		if _, err := tx.Exec(ctx, `UPDATE actions SET status=$2,payload=$3,updated_at=$4 WHERE id=$1`, action.ID, action.Status, actionPayload, now); err != nil {
			return err
		}
	}
	if job.State == "waiting_rollout" || job.State == "verified" || job.State == "rollback_verifying" || job.State == "rolled_back" {
		res, err := getJSONTx[resource.Resource](ctx, tx, nil, `SELECT payload FROM resources WHERE id=$1 FOR UPDATE`, job.Resource.ID)
		if err != nil {
			return err
		}
		if job.State == "rolled_back" || job.State == "rollback_verifying" {
			res.CurrentState = job.Plan.OriginalState
		} else {
			res.CurrentState = job.Plan.AppliedState
		}
		res.UpdatedAt = now
		resPayload, _ := marshal(res)
		if _, err := tx.Exec(ctx, `UPDATE resources SET payload=$2,updated_at=$3 WHERE id=$1`, res.ID, resPayload, now); err != nil {
			return err
		}
	}
	return nil
}

func (p *Postgres) ListJobs(ctx context.Context) ([]Job, error) {
	return listJSON[Job](ctx, p.pool, `SELECT payload FROM jobs ORDER BY id`)
}
