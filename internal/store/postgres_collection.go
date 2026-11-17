package store

import (
	"context"
	"errors"
	"time"
)

func (p *Postgres) EnqueueCollection(ctx context.Context, c Collection) (Collection, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return Collection{}, err
	}
	defer tx.Rollback(ctx)
	// Serialize equivalent/overlapping scopes only during the short enqueue transaction.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,22))`, c.Resource.ID+"/"+c.PluginID); err != nil {
		return Collection{}, err
	}
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return Collection{}, err
	}
	c, err = prepareCollection(c, now.UTC())
	if err != nil {
		return Collection{}, err
	}
	prior, err := getJSONTx[Collection](ctx, tx, nil, `SELECT payload FROM evidence_collections WHERE id=$1`, c.ID)
	if err == nil {
		return prior, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Collection{}, err
	}
	rows, err := tx.Query(ctx, `SELECT payload FROM evidence_collections WHERE resource_id=$1 AND plugin_id=$2 AND instance_digest=$3 AND state IN ('pending','running','retry_wait')`, c.Resource.ID, c.PluginID, c.InstanceDigest)
	if err != nil {
		return Collection{}, err
	}
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return Collection{}, err
		}
		other, e := decode[Collection](raw)
		if e != nil {
			rows.Close()
			return Collection{}, e
		}
		if overlappingCollection(other, c) {
			rows.Close()
			return Collection{}, ErrCollectionOverlap
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return Collection{}, err
	}
	payload, err := marshal(c)
	if err != nil {
		return Collection{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO evidence_collections(id,resource_id,plugin_id,instance_digest,state,next_run,created_at,payload) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, c.ID, c.Resource.ID, c.PluginID, c.InstanceDigest, c.State, c.NextRun, c.CreatedAt, payload); err != nil {
		return Collection{}, err
	}
	if err = insertAuditEvent(ctx, tx, collectionEvent(c)); err != nil {
		return Collection{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Collection{}, err
	}
	return c, nil
}
func (p *Postgres) GetCollection(ctx context.Context, id string) (Collection, error) {
	return getJSON[Collection](ctx, p.pool, `SELECT payload FROM evidence_collections WHERE id=$1`, id)
}
func (p *Postgres) ListCollections(ctx context.Context, q CollectionQuery) ([]Collection, error) {
	// All values remain parameters. A running row is due only after its lease expires.
	rows, err := p.pool.Query(ctx, `SELECT payload FROM evidence_collections WHERE ($1='' OR resource_id=$1) AND (NOT $2 OR (state IN ('pending','running','retry_wait') AND next_run<=clock_timestamp() AND (payload->'lease' IS NULL OR (payload->'lease'->>'until')::timestamptz<=clock_timestamp()))) ORDER BY CASE WHEN $2 THEN next_run END ASC, CASE WHEN NOT $2 THEN created_at END DESC,id LIMIT $3`, q.ResourceID, q.Due, collectionLimit(q.Limit))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Collection{}
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		c, e := decode[Collection](raw)
		if e != nil {
			return nil, e
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
func (p *Postgres) mutateCollection(ctx context.Context, id string, fn func(Collection, time.Time) (Collection, error)) (Collection, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return Collection{}, err
	}
	defer tx.Rollback(ctx)
	c, err := getJSONTx[Collection](ctx, tx, nil, `SELECT payload FROM evidence_collections WHERE id=$1 FOR UPDATE`, id)
	if err != nil {
		return Collection{}, err
	}
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return Collection{}, err
	}
	c, err = fn(c, now.UTC())
	if err != nil {
		return Collection{}, err
	}
	raw, err := marshal(c)
	if err != nil {
		return Collection{}, err
	}
	var finished any
	if !c.FinishedAt.IsZero() {
		finished = c.FinishedAt
	}
	if _, err = tx.Exec(ctx, `UPDATE evidence_collections SET state=$2,next_run=$3,finished_at=$4,payload=$5 WHERE id=$1`, id, c.State, c.NextRun, finished, raw); err != nil {
		return Collection{}, err
	}
	if err = insertAuditEvent(ctx, tx, collectionEvent(c)); err != nil {
		return Collection{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Collection{}, err
	}
	return c, nil
}
func (p *Postgres) ClaimCollection(ctx context.Context, id, owner string, ttl time.Duration) (Collection, error) {
	return p.mutateCollection(ctx, id, func(c Collection, now time.Time) (Collection, error) { return claimCollection(c, owner, ttl, now) })
}
func (p *Postgres) FinishCollection(ctx context.Context, c Collection, r CollectionResult) (Collection, error) {
	return p.mutateCollection(ctx, c.ID, func(old Collection, now time.Time) (Collection, error) { return finishCollection(old, c, r, now) })
}
func (p *Postgres) PruneCollections(ctx context.Context, before time.Time) (int, error) {
	// Keep an idempotency tombstone. Workflow evidence for the resource is retained
	// conservatively regardless of workflow state, including expired recommendations.
	tag, err := p.pool.Exec(ctx, `UPDATE evidence_collections c SET payload=(payload-'snapshot'-'assessment') || '{"pruned":true}'::jsonb WHERE state IN ('succeeded','degraded','failed') AND finished_at<$1 AND COALESCE((payload->>'pruned')::boolean,false)=false AND NOT EXISTS(SELECT 1 FROM recommendations r WHERE r.resource_id=c.resource_id) AND NOT EXISTS(SELECT 1 FROM actions a WHERE a.resource_id=c.resource_id)`, before)
	return int(tag.RowsAffected()), err
}
