package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/consize-oss/consize/pkg/resource"
	"github.com/jackc/pgx/v5"
)

func writeDiscoveryTx(ctx context.Context, tx pgx.Tx, r DiscoveryRun, create bool) error {
	raw, err := marshal(r)
	if err != nil {
		return err
	}
	if create {
		_, err = tx.Exec(ctx, `INSERT INTO discovery_runs(id,plugin_id,scope_key,state,next_run,created_at,payload) VALUES($1,$2,$3,$4,$5,$6,$7)`, r.ID, r.PluginID, r.ScopeKey, r.State, r.NextRun, r.CreatedAt, raw)
	} else {
		_, err = tx.Exec(ctx, `UPDATE discovery_runs SET state=$2,next_run=$3,payload=$4 WHERE id=$1`, r.ID, r.State, r.NextRun, raw)
	}
	if err != nil {
		return err
	}
	return insertAuditEvent(ctx, tx, discoveryEvent(r))
}
func (p *Postgres) EnqueueDiscovery(ctx context.Context, r DiscoveryRun) (DiscoveryRun, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return DiscoveryRun{}, err
	}
	defer tx.Rollback(ctx)
	_, key := DiscoveryScopeIdentity(r.PluginID, r.Scope)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,15))`, key); err != nil {
		return DiscoveryRun{}, err
	}
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return DiscoveryRun{}, err
	}
	r, err = prepareDiscovery(r, now.UTC())
	if err != nil {
		return DiscoveryRun{}, err
	}
	prior, err := getJSONTx[DiscoveryRun](ctx, tx, nil, `SELECT payload FROM discovery_runs WHERE id=$1`, r.ID)
	if err == nil {
		return prior, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return DiscoveryRun{}, err
	}
	var busy bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM discovery_runs WHERE scope_key=$1 AND state IN ('pending','running','retry_wait'))`, r.ScopeKey).Scan(&busy); err != nil {
		return DiscoveryRun{}, err
	}
	if busy {
		return DiscoveryRun{}, ErrDiscoveryBusy
	}
	if err = writeDiscoveryTx(ctx, tx, r, true); err != nil {
		return DiscoveryRun{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return DiscoveryRun{}, err
	}
	return r, nil
}
func (p *Postgres) GetDiscoveryRun(ctx context.Context, id string) (DiscoveryRun, error) {
	return getJSON[DiscoveryRun](ctx, p.pool, `SELECT payload FROM discovery_runs WHERE id=$1`, id)
}
func (p *Postgres) ListDiscoveryRuns(ctx context.Context, q DiscoveryRunQuery) ([]DiscoveryRun, error) {
	rows, err := p.pool.Query(ctx, `SELECT payload FROM discovery_runs WHERE ($1='' OR plugin_id=$1) AND (NOT $2 OR(state IN ('pending','running','retry_wait') AND next_run<=clock_timestamp() AND(payload->'lease' IS NULL OR(payload->'lease'->>'until')::timestamptz<=clock_timestamp()))) ORDER BY CASE WHEN $2 THEN next_run END ASC,CASE WHEN NOT $2 THEN created_at END DESC,id LIMIT $3`, q.PluginID, q.Due, collectionLimit(q.Limit))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DiscoveryRun{}
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		r, e := decode[DiscoveryRun](raw)
		if e != nil {
			return nil, e
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (p *Postgres) ClaimDiscovery(ctx context.Context, id, owner string, ttl time.Duration) (DiscoveryRun, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return DiscoveryRun{}, err
	}
	defer tx.Rollback(ctx)
	r, err := getJSONTx[DiscoveryRun](ctx, tx, nil, `SELECT payload FROM discovery_runs WHERE id=$1 FOR UPDATE`, id)
	if err != nil {
		return DiscoveryRun{}, err
	}
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return DiscoveryRun{}, err
	}
	r, err = claimDiscovery(r, owner, ttl, now.UTC())
	if err != nil {
		return DiscoveryRun{}, err
	}
	if err = writeDiscoveryTx(ctx, tx, r, false); err != nil {
		return DiscoveryRun{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return DiscoveryRun{}, err
	}
	return r, nil
}
func (p *Postgres) CompleteDiscovery(ctx context.Context, candidate DiscoveryRun, c DiscoveryCompletion) (DiscoveryRun, error) {
	// Use the existing registry transaction lock, including absent-row identities.
	// No transaction is opened until the provider has returned.
	tx, err := p.registryTx(ctx)
	if err != nil {
		return DiscoveryRun{}, err
	}
	defer tx.Rollback(ctx)
	r, err := getJSONTx[DiscoveryRun](ctx, tx, nil, `SELECT payload FROM discovery_runs WHERE id=$1 FOR UPDATE`, candidate.ID)
	if err != nil {
		return DiscoveryRun{}, err
	}
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return DiscoveryRun{}, err
	}
	now = now.UTC()
	if !discoveryOwns(r, candidate, now) {
		return DiscoveryRun{}, ErrLeaseLost
	}
	if c.ErrorCode == "" {
		rawRows, e := tx.Query(ctx, `SELECT payload FROM resources FOR UPDATE`)
		if e != nil {
			return DiscoveryRun{}, e
		}
		rows := map[string]resource.Resource{}
		for rawRows.Next() {
			var raw []byte
			if e = rawRows.Scan(&raw); e != nil {
				rawRows.Close()
				return DiscoveryRun{}, e
			}
			row, decodeErr := decodeDiscoveryResource(raw)
			if decodeErr != nil {
				rawRows.Close()
				return DiscoveryRun{}, decodeErr
			}
			rows[row.ID] = row
		}
		e = rawRows.Err()
		rawRows.Close()
		if e != nil {
			return DiscoveryRun{}, e
		}
		next, ids, result, e := planDiscovery(rows, r, c, now)
		if e != nil {
			return DiscoveryRun{}, fmt.Errorf("%w: %v", ErrDiscoveryInvalid, e)
		}
		for id, row := range next {
			if prior, ok := rows[id]; ok && reflect.DeepEqual(prior, row) {
				continue
			}
			payload, e := marshal(row)
			if e != nil {
				return DiscoveryRun{}, e
			}
			if _, e = tx.Exec(ctx, `INSERT INTO resources(id,provider,account_id,region,provider_resource_id,payload,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(id) DO UPDATE SET payload=excluded.payload,updated_at=excluded.updated_at`, row.ID, row.Provider, row.Account, row.Region, row.ProviderResourceID, payload, row.CreatedAt, row.UpdatedAt); e != nil {
				return DiscoveryRun{}, e
			}
		}
		r.Scan = c.Scan
		r.ResourceIDs = ids
		r.Reconciliation = result
	}
	r, err = finishDiscovery(r, c, now)
	if err != nil {
		return DiscoveryRun{}, err
	}
	if err = writeDiscoveryTx(ctx, tx, r, false); err != nil {
		return DiscoveryRun{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return DiscoveryRun{}, err
	}
	return r, nil
}

// Match file-store registry cloning: metadata numbers must survive a scan without
// float64 rounding, including rows not owned by the scan's authority.
func decodeDiscoveryResource(raw []byte) (resource.Resource, error) {
	var r resource.Resource
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	err := decoder.Decode(&r)
	return r, err
}
