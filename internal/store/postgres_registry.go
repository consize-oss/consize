package store

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/consize-oss/consize/pkg/resource"
	"github.com/jackc/pgx/v5"
)

// A transaction-scoped, schema-specific registry lock also covers the absent-row
// case. All registry writers take it before inspecting identity or lifecycle.
func (p *Postgres) registryTx(ctx context.Context) (pgx.Tx, error) {
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended(current_schema() || ':consize-resource-registry',0))`); err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	return tx, nil
}
func (p *Postgres) CreateResource(ctx context.Context, r resource.Resource) (resource.Resource, error) {
	return p.putResource(ctx, r, true)
}
func (p *Postgres) UpsertResource(ctx context.Context, r resource.Resource) (resource.Resource, error) {
	return p.putResource(ctx, r, false)
}
func (p *Postgres) putResource(ctx context.Context, r resource.Resource, createOnly bool) (resource.Resource, error) {
	now := p.now().UTC()
	normalized, err := normalizeObservation(r, now)
	if err != nil {
		return resource.Resource{}, err
	}
	tx, err := p.registryTx(ctx)
	if err != nil {
		return resource.Resource{}, err
	}
	defer tx.Rollback(ctx)
	var data []byte
	var existing *resource.Resource
	// ID lookup first rejects an attempt to reuse a different resource's ID.
	err = tx.QueryRow(ctx, `SELECT payload FROM resources WHERE id=$1 FOR UPDATE`, normalized.ID).Scan(&data)
	if err == nil {
		value, decodeErr := decode[resource.Resource](data)
		if decodeErr != nil {
			return resource.Resource{}, decodeErr
		}
		existing = &value
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return resource.Resource{}, err
	}
	if existing == nil {
		err = tx.QueryRow(ctx, `SELECT payload FROM resources WHERE provider=$1 AND account_id=$2 AND region=$3 AND payload->>'type'=$4 AND provider_resource_id=$5 FOR UPDATE`, normalized.Provider, normalized.Account, normalized.Region, normalized.Type, normalized.ProviderResourceID).Scan(&data)
		if err == nil {
			value, decodeErr := decode[resource.Resource](data)
			if decodeErr != nil {
				return resource.Resource{}, decodeErr
			}
			existing = &value
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return resource.Resource{}, err
		}
	}
	normalized, err = prepareResource(normalized, existing, now, createOnly)
	if err != nil {
		return resource.Resource{}, err
	}
	payload, err := marshal(normalized)
	if err != nil {
		return resource.Resource{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO resources(id,provider,account_id,region,provider_resource_id,payload,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(id) DO UPDATE SET payload=excluded.payload,updated_at=excluded.updated_at`, normalized.ID, normalized.Provider, normalized.Account, normalized.Region, normalized.ProviderResourceID, payload, normalized.CreatedAt, normalized.UpdatedAt)
	if err != nil {
		return resource.Resource{}, fmt.Errorf("persist resource: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return resource.Resource{}, err
	}
	return normalized, nil
}
func (p *Postgres) UpdateResourceLifecycle(ctx context.Context, req resource.LifecycleUpdate) (resource.Resource, error) {
	if err := validateScope(req.Scope); err != nil {
		return resource.Resource{}, err
	}
	tx, err := p.registryTx(ctx)
	if err != nil {
		return resource.Resource{}, err
	}
	defer tx.Rollback(ctx)
	var b []byte
	if err = tx.QueryRow(ctx, `SELECT payload FROM resources WHERE id=$1 FOR UPDATE`, req.ID).Scan(&b); err != nil {
		return resource.Resource{}, notFound(err)
	}
	previous, err := decode[resource.Resource](b)
	if err != nil {
		return resource.Resource{}, err
	}
	if !scopeMatches(previous, req.Scope) {
		return resource.Resource{}, resource.ErrScopeMismatch
	}
	updated, err := lifecycleChange(previous, req.State, req.ObservedAt, p.now())
	if err != nil {
		return resource.Resource{}, err
	}
	if err = writeLifecycle(ctx, tx, updated); err != nil {
		return resource.Resource{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return resource.Resource{}, err
	}
	return updated, nil
}
func writeLifecycle(ctx context.Context, tx pgx.Tx, r resource.Resource) error {
	b, err := marshal(r)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE resources SET payload=$2,updated_at=$3 WHERE id=$1`, r.ID, b, r.UpdatedAt)
	return err
}
func (p *Postgres) ReconcileResources(ctx context.Context, req resource.Reconciliation) (resource.ReconciliationResult, error) {
	if err := validateScope(req.Scope); err != nil {
		return resource.ReconciliationResult{}, err
	}
	tx, err := p.registryTx(ctx)
	if err != nil {
		return resource.ReconciliationResult{}, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT payload FROM resources WHERE provider=$1 AND account_id=$2 AND region=$3 AND payload->>'type'=$4 AND payload->>'source_plugin_id'=$5 ORDER BY id FOR UPDATE`, req.Scope.Provider, req.Scope.Account, req.Scope.Region, req.Scope.Type, req.Scope.SourcePluginID)
	if err != nil {
		return resource.ReconciliationResult{}, err
	}
	resources := []resource.Resource{}
	for rows.Next() {
		var b []byte
		if err = rows.Scan(&b); err != nil {
			rows.Close()
			return resource.ReconciliationResult{}, err
		}
		r, decodeErr := decode[resource.Resource](b)
		if decodeErr != nil {
			rows.Close()
			return resource.ReconciliationResult{}, decodeErr
		}
		resources = append(resources, r)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return resource.ReconciliationResult{}, err
	}
	changes, result, err := planReconciliation(resources, req, p.now())
	if err != nil {
		return result, err
	}
	for _, r := range changes {
		if err = writeLifecycle(ctx, tx, r); err != nil {
			return resource.ReconciliationResult{}, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return resource.ReconciliationResult{}, err
	}
	return result, nil
}
func (p *Postgres) ListResourcePage(ctx context.Context, q resource.Query) (resource.Page, error) {
	q, after, err := parseResourceQuery(q)
	if err != nil {
		return resource.Page{}, err
	}
	// Fixed field names and bound values keep filters out of executable SQL.
	conditions := []string{`id COLLATE "C" > $1`}
	args := []any{after}
	for _, filter := range [][2]string{{"provider", q.Provider}, {"account_id", q.Account}, {"region", q.Region}, {"payload->>'type'", q.Type}, {"payload->>'source_plugin_id'", q.SourcePluginID}, {"payload->>'environment'", q.Environment}, {"payload->>'lifecycle_state'", q.LifecycleState}, {"payload->>'support_status'", q.SupportStatus}, {"payload->>'owner'", q.Owner}} {
		if filter[1] != "" {
			args = append(args, filter[1])
			conditions = append(conditions, filter[0]+"=$"+strconv.Itoa(len(args)))
		}
	}
	args = append(args, q.Limit+1)
	rows, err := p.pool.Query(ctx, `SELECT payload FROM resources WHERE `+strings.Join(conditions, " AND ")+` ORDER BY id COLLATE "C" LIMIT $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		return resource.Page{}, err
	}
	defer rows.Close()
	out := []resource.Resource{}
	for rows.Next() {
		var b []byte
		if err = rows.Scan(&b); err != nil {
			return resource.Page{}, err
		}
		r, decodeErr := decode[resource.Resource](b)
		if decodeErr != nil {
			return resource.Page{}, decodeErr
		}
		out = append(out, r)
	}
	if err = rows.Err(); err != nil {
		return resource.Page{}, err
	}
	return resourcePage(out, q), nil
}
