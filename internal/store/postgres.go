package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/consize-oss/consize/pkg/resource"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Postgres struct {
	pool        *pgxpool.Pool
	diagnostics StorageDiagnostics
	now         func() time.Time
}

func OpenPostgres(ctx context.Context, databaseURL string) (*Postgres, error) {
	if databaseURL == "" {
		return nil, errors.New("database_url is required")
	}
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database_url: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect postgres: %w", err)
	}
	applied, err := migratePostgres(ctx, pool)
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("migrate postgres: %w", err)
	}
	migrations, err := loadPostgresMigrations()
	if err != nil {
		pool.Close()
		return nil, err
	}
	status := MigrationCurrent
	if len(applied) > 0 {
		status = MigrationApplied
	}
	return &Postgres{pool: pool, now: time.Now, diagnostics: StorageDiagnostics{
		Backend: "postgres", Durable: true, Healthy: true, CurrentVersion: len(migrations), LoadedVersion: len(migrations) - len(applied),
		TargetVersion: len(migrations), MigrationStatus: status, Migrations: applied, Indexes: RequiredIndexes(),
	}}, nil
}

// InspectPostgres reads migration state without creating tables or applying migrations.
func InspectPostgres(ctx context.Context, databaseURL string) (StorageDiagnostics, error) {
	diagnostics := StorageDiagnostics{Backend: "postgres", Durable: true, TargetVersion: 0, MigrationStatus: MigrationPending, Indexes: RequiredIndexes()}
	migrations, err := loadPostgresMigrations()
	if err != nil {
		return diagnostics, err
	}
	diagnostics.TargetVersion = len(migrations)
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return diagnostics, fmt.Errorf("parse database_url: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return diagnostics, err
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		diagnostics.Error = "database health check failed"
		return diagnostics, err
	}
	diagnostics.Healthy = true
	var ledgerExists bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass(current_schema() || '.schema_migrations') IS NOT NULL`).Scan(&ledgerExists); err != nil {
		return diagnostics, err
	}
	if !ledgerExists {
		return diagnostics, nil
	}
	rows, err := pool.Query(ctx, `SELECT version,name,checksum FROM schema_migrations ORDER BY version`)
	if err != nil {
		return diagnostics, err
	}
	defer rows.Close()
	version := 0
	for rows.Next() {
		var appliedVersion int
		var name, checksum string
		if err := rows.Scan(&appliedVersion, &name, &checksum); err != nil {
			return diagnostics, err
		}
		if appliedVersion != version+1 {
			diagnostics.MigrationStatus = MigrationInvalid
			return diagnostics, fmt.Errorf("database migration ledger has a gap at version %d", version+1)
		}
		if appliedVersion > len(migrations) {
			diagnostics.MigrationStatus = MigrationInvalid
			return diagnostics, fmt.Errorf("database schema version %d is newer than supported version %d", appliedVersion, len(migrations))
		}
		expected := migrations[appliedVersion-1]
		if name != expected.Name || checksum != expected.Checksum {
			diagnostics.MigrationStatus = MigrationInvalid
			return diagnostics, fmt.Errorf("migration %03d was changed after application", appliedVersion)
		}
		version = appliedVersion
	}
	if err := rows.Err(); err != nil {
		return diagnostics, err
	}
	diagnostics.CurrentVersion, diagnostics.LoadedVersion = version, version
	if version == len(migrations) {
		diagnostics.MigrationStatus = MigrationCurrent
	}
	return diagnostics, nil
}

func (p *Postgres) Close() error                     { p.pool.Close(); return nil }
func (p *Postgres) Durable() bool                    { return true }
func (p *Postgres) Health(ctx context.Context) error { return p.pool.Ping(ctx) }

func (p *Postgres) StorageDiagnostics(ctx context.Context) StorageDiagnostics {
	out := p.diagnostics
	out.Healthy = p.pool.Ping(ctx) == nil
	if !out.Healthy {
		out.Error = "database health check failed"
	}
	var resources, recommendations, actions, events, jobs int
	if err := p.pool.QueryRow(ctx, `SELECT
        (SELECT count(*) FROM resources), (SELECT count(*) FROM recommendations),
        (SELECT count(*) FROM actions), (SELECT count(*) FROM action_events),
        (SELECT count(*) FROM jobs)`).Scan(&resources, &recommendations, &actions, &events, &jobs); err == nil {
		out.EntityCounts = map[string]int{"resources": resources, "recommendations": recommendations, "actions": actions, "audit_events": events, "jobs": jobs}
	}
	return out
}

func marshal(value any) ([]byte, error) { return json.Marshal(value) }

func decode[T any](data []byte) (T, error) {
	var value T
	err := json.Unmarshal(data, &value)
	return value, err
}

func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func (p *Postgres) UpsertResource(ctx context.Context, res resource.Resource) (resource.Resource, error) {
	now := p.now().UTC()
	normalized, err := res.Normalize(now)
	if err != nil {
		return resource.Resource{}, fmt.Errorf("normalize resource: %w", err)
	}
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return resource.Resource{}, err
	}
	defer tx.Rollback(ctx)
	var existingJSON []byte
	err = tx.QueryRow(ctx, `SELECT payload FROM resources WHERE id=$1 FOR UPDATE`, normalized.ID).Scan(&existingJSON)
	if err == nil {
		existing, err := decode[resource.Resource](existingJSON)
		if err != nil {
			return resource.Resource{}, err
		}
		if !resource.SameIdentity(existing, normalized) {
			return resource.Resource{}, fmt.Errorf("%w: id %q belongs to a different provider resource", resource.ErrIdentityConflict, normalized.ID)
		}
		if normalized.ObservedAt.Before(existing.LastSeenAt) {
			return resource.Resource{}, errors.New("stale resource observation")
		}
		if !resource.ValidLifecycleTransition(existing.LifecycleState, normalized.LifecycleState) {
			return resource.Resource{}, errors.New("invalid resource lifecycle transition")
		}
		normalized.FirstSeenAt, normalized.CreatedAt = existing.FirstSeenAt, existing.CreatedAt
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return resource.Resource{}, err
	}
	normalized.LastSeenAt, normalized.UpdatedAt = normalized.ObservedAt, now
	if err := normalized.Validate(); err != nil {
		return resource.Resource{}, err
	}
	payload, err := marshal(normalized)
	if err != nil {
		return resource.Resource{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO resources(id,provider,account_id,region,provider_resource_id,payload,created_at,updated_at)
        VALUES($1,$2,$3,$4,$5,$6,$7,$8)
        ON CONFLICT(id) DO UPDATE SET payload=excluded.payload, updated_at=excluded.updated_at`,
		normalized.ID, normalized.Provider, normalized.Account, normalized.Region, normalized.ProviderResourceID, payload, normalized.CreatedAt, normalized.UpdatedAt)
	if err != nil {
		return resource.Resource{}, fmt.Errorf("persist resource: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return resource.Resource{}, err
	}
	return normalized, nil
}

func (p *Postgres) GetResource(ctx context.Context, id string) (resource.Resource, error) {
	var payload []byte
	if err := p.pool.QueryRow(ctx, `SELECT payload FROM resources WHERE id=$1`, id).Scan(&payload); err != nil {
		return resource.Resource{}, notFound(err)
	}
	return decode[resource.Resource](payload)
}

func (p *Postgres) ListResources(ctx context.Context) ([]resource.Resource, error) {
	return listJSON[resource.Resource](ctx, p.pool, `SELECT payload FROM resources ORDER BY id`)
}

func (p *Postgres) CreateRecommendation(ctx context.Context, rec Recommendation) (Recommendation, error) {
	if rec.ResourceID == "" || rec.PluginID == "" || rec.ActionType == "" || rec.AlgorithmID == "" || len(rec.EvidenceRefs) == 0 {
		return Recommendation{}, errors.New("resource_id, plugin_id, action_type, algorithm_id, and evidence_refs are required")
	}
	now := p.now().UTC()
	if rec.Status != "" && rec.Status != RecommendationPending {
		return Recommendation{}, errors.New("new recommendation must start pending")
	}
	if !rec.ExpiresAt.IsZero() && !rec.ExpiresAt.After(now) {
		return Recommendation{}, errors.New("recommendation expiry must be in the future")
	}
	rec.SchemaVersion, rec.Status, rec.CreatedAt, rec.UpdatedAt = ContractVersion, RecommendationPending, now, now
	if rec.AlgorithmVersion == "" {
		rec.AlgorithmVersion = "1"
	}
	if rec.RecommendationType == "" {
		rec.RecommendationType = rec.ActionType
	}
	if rec.Current == nil {
		rec.Current = map[string]any{}
	}
	if rec.Proposed == nil {
		rec.Proposed = map[string]any{}
	}
	if rec.Parameters == nil {
		rec.Parameters = map[string]any{}
	}
	if rec.SavingsEstimate.Classification == "" {
		rec.SavingsEstimate = SavingsEstimate{Classification: SavingsEstimated, AmountMonthly: rec.EstimatedSavingsMonthly, CalculatedAt: now}
	}
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Recommendation{}, err
	}
	defer tx.Rollback(ctx)
	if err := tx.QueryRow(ctx, `INSERT INTO recommendations(resource_id,plugin_id,algorithm_id,status,expires_at,payload,created_at,updated_at)
        VALUES($1,$2,$3,$4,NULLIF($5,'0001-01-01T00:00:00Z')::timestamptz,'{}',$6,$7) RETURNING id`, rec.ResourceID, rec.PluginID, rec.AlgorithmID, rec.Status, rec.ExpiresAt.Format(time.RFC3339Nano), now, now).Scan(&rec.ID); err != nil {
		return Recommendation{}, fmt.Errorf("create recommendation: %w", err)
	}
	payload, _ := marshal(rec)
	if _, err := tx.Exec(ctx, `UPDATE recommendations SET payload=$2 WHERE id=$1`, rec.ID, payload); err != nil {
		return Recommendation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Recommendation{}, err
	}
	return rec, nil
}

func (p *Postgres) GetRecommendation(ctx context.Context, id int64) (Recommendation, error) {
	return getJSON[Recommendation](ctx, p.pool, `SELECT payload FROM recommendations WHERE id=$1`, id)
}
func (p *Postgres) ListRecommendations(ctx context.Context) ([]Recommendation, error) {
	return listJSON[Recommendation](ctx, p.pool, `SELECT payload FROM recommendations ORDER BY id`)
}

func (p *Postgres) TransitionRecommendation(ctx context.Context, id int64, status RecommendationStatus, reason string) error {
	return p.updateRecommendation(ctx, id, func(rec *Recommendation) error {
		if !ValidRecommendationTransition(rec.Status, status) {
			return fmt.Errorf("%w: recommendation %s -> %s", ErrInvalidTransition, rec.Status, status)
		}
		rec.Status, rec.StatusReason, rec.UpdatedAt = status, reason, p.now().UTC()
		return nil
	})
}

func (p *Postgres) SupersedeRecommendation(ctx context.Context, id, replacementID int64, reason string) error {
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	rec, err := getJSONTx[Recommendation](ctx, tx, nil, `SELECT payload FROM recommendations WHERE id=$1 FOR UPDATE`, id)
	if err != nil {
		return err
	}
	replacement, err := getJSONTx[Recommendation](ctx, tx, nil, `SELECT payload FROM recommendations WHERE id=$1`, replacementID)
	if err != nil {
		return errors.New("replacement recommendation does not exist")
	}
	if id == replacementID || rec.ResourceID != replacement.ResourceID {
		return errors.New("replacement must be a different recommendation for the same resource")
	}
	if !ValidRecommendationTransition(rec.Status, RecommendationSuperseded) {
		return fmt.Errorf("%w: recommendation %s -> %s", ErrInvalidTransition, rec.Status, RecommendationSuperseded)
	}
	rec.Status, rec.SupersededBy, rec.StatusReason, rec.UpdatedAt = RecommendationSuperseded, replacementID, reason, p.now().UTC()
	payload, _ := marshal(rec)
	if _, err := tx.Exec(ctx, `UPDATE recommendations SET status=$2,payload=$3,updated_at=$4 WHERE id=$1`, id, rec.Status, payload, rec.UpdatedAt); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (p *Postgres) updateRecommendation(ctx context.Context, id int64, mutate func(*Recommendation) error) error {
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	rec, err := getJSONTx[Recommendation](ctx, tx, nil, `SELECT payload FROM recommendations WHERE id=$1 FOR UPDATE`, id)
	if err != nil {
		return err
	}
	if err := mutate(&rec); err != nil {
		return err
	}
	payload, _ := marshal(rec)
	if _, err := tx.Exec(ctx, `UPDATE recommendations SET status=$2, expires_at=NULLIF($3,'0001-01-01T00:00:00Z')::timestamptz, payload=$4, updated_at=$5 WHERE id=$1`, id, rec.Status, rec.ExpiresAt.Format(time.RFC3339Nano), payload, rec.UpdatedAt); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (p *Postgres) CreateAction(ctx context.Context, action Action) (Action, error) {
	if err := action.Validate(); err != nil {
		return Action{}, err
	}
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Action{}, err
	}
	defer tx.Rollback(ctx)
	var existing []byte
	if err := tx.QueryRow(ctx, `SELECT payload FROM actions WHERE idempotency_key=$1`, action.IdempotencyKey).Scan(&existing); err == nil {
		stored, err := decode[Action](existing)
		if err != nil {
			return Action{}, err
		}
		if stored.RecommendationID != action.RecommendationID || stored.ResourceID != action.ResourceID || stored.PluginID != action.PluginID || stored.ActionType != action.ActionType || stored.Mode != action.Mode {
			return Action{}, errors.New("idempotency key is already bound to a different action request")
		}
		return stored, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return Action{}, err
	}
	rec, err := getJSONTx[Recommendation](ctx, tx, nil, `SELECT payload FROM recommendations WHERE id=$1 FOR UPDATE`, action.RecommendationID)
	if err != nil {
		return Action{}, err
	}
	now := p.now().UTC()
	if rec.Status == RecommendationExpired || rec.Status == RecommendationSuperseded || (!rec.ExpiresAt.IsZero() && !rec.ExpiresAt.After(now)) {
		return Action{}, ErrRecommendationGone
	}
	if rec.ResourceID != action.ResourceID || rec.PluginID != action.PluginID || rec.ActionType != action.ActionType {
		return Action{}, errors.New("action does not match its recommendation")
	}
	action.SchemaVersion, action.Status, action.RequestedAt, action.UpdatedAt = ContractVersion, ActionRequested, now, now
	if action.Parameters == nil {
		action.Parameters = map[string]any{}
	}
	if err := tx.QueryRow(ctx, `INSERT INTO actions(recommendation_id,resource_id,idempotency_key,status,payload,created_at,updated_at) VALUES($1,$2,$3,$4,'{}',$5,$5) RETURNING id`, action.RecommendationID, action.ResourceID, action.IdempotencyKey, action.Status, now).Scan(&action.ID); err != nil {
		return Action{}, err
	}
	payload, _ := marshal(action)
	if _, err := tx.Exec(ctx, `UPDATE actions SET payload=$2 WHERE id=$1`, action.ID, payload); err != nil {
		return Action{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Action{}, err
	}
	return action, nil
}

func (p *Postgres) GetAction(ctx context.Context, id int64) (Action, error) {
	return getJSON[Action](ctx, p.pool, `SELECT payload FROM actions WHERE id=$1`, id)
}
func (p *Postgres) ListActions(ctx context.Context) ([]Action, error) {
	return listJSON[Action](ctx, p.pool, `SELECT payload FROM actions ORDER BY id`)
}

func (p *Postgres) TransitionAction(ctx context.Context, id int64, status ActionStatus, mutate func(*Action) error) (Action, error) {
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Action{}, err
	}
	defer tx.Rollback(ctx)
	action, err := getJSONTx[Action](ctx, tx, nil, `SELECT payload FROM actions WHERE id=$1 FOR UPDATE`, id)
	if err != nil {
		return Action{}, err
	}
	if !ValidActionTransition(action.Status, status) {
		return Action{}, fmt.Errorf("%w: action %s -> %s", ErrInvalidTransition, action.Status, status)
	}
	original := action
	if mutate != nil {
		if err := mutate(&action); err != nil {
			return Action{}, err
		}
	}
	if action.RecommendationID != original.RecommendationID || action.ResourceID != original.ResourceID || action.RemediationPath != original.RemediationPath || action.PluginID != original.PluginID || action.PluginVersion != original.PluginVersion || action.ActionType != original.ActionType || action.Mode != original.Mode || action.IdempotencyKey != original.IdempotencyKey || action.RequestedBy != original.RequestedBy || !reflect.DeepEqual(action.Parameters, original.Parameters) || !reflect.DeepEqual(action.PolicyDecision, original.PolicyDecision) {
		return Action{}, errors.New("immutable action fields cannot be changed")
	}
	now := p.now().UTC()
	action.Status, action.UpdatedAt = status, now
	if status == ActionApproved {
		action.ApprovedAt = now
		if action.ApprovedBy == "" {
			action.ApprovedBy = action.RequestedBy
		}
	}
	if status == ActionExecuting && action.StartedAt.IsZero() {
		action.StartedAt = now
	}
	if ActionTerminal(status) {
		action.FinishedAt = now
	}
	payload, _ := marshal(action)
	if _, err := tx.Exec(ctx, `UPDATE actions SET status=$2,payload=$3,updated_at=$4 WHERE id=$1`, id, status, payload, now); err != nil {
		return Action{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Action{}, err
	}
	return action, nil
}

func (p *Postgres) CreateActionEvent(ctx context.Context, event ActionEvent) (ActionEvent, error) {
	event.CreatedAt = p.now().UTC()
	if event.Parameters == nil {
		event.Parameters = map[string]any{}
	}
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ActionEvent{}, err
	}
	defer tx.Rollback(ctx)
	if event.ActionID > 0 && event.RecommendationID > 0 {
		var recID int64
		if err := tx.QueryRow(ctx, `SELECT recommendation_id FROM actions WHERE id=$1`, event.ActionID).Scan(&recID); err != nil || recID != event.RecommendationID {
			return ActionEvent{}, errors.New("audit action does not exist or does not match its recommendation")
		}
	}
	if err := tx.QueryRow(ctx, `INSERT INTO action_events(action_id,recommendation_id,resource_id,payload,created_at) VALUES(NULLIF($1,0),NULLIF($2,0),$3,'{}',$4) RETURNING id`, event.ActionID, event.RecommendationID, event.ResourceID, event.CreatedAt).Scan(&event.ID); err != nil {
		return ActionEvent{}, err
	}
	payload, _ := marshal(event)
	if _, err := tx.Exec(ctx, `UPDATE action_events SET payload=$2 WHERE id=$1`, event.ID, payload); err != nil {
		return ActionEvent{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ActionEvent{}, err
	}
	return event, nil
}
func (p *Postgres) ListActionEvents(ctx context.Context) ([]ActionEvent, error) {
	return listJSON[ActionEvent](ctx, p.pool, `SELECT payload FROM action_events ORDER BY id`)
}

type rowQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}
type rowsQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func getJSON[T any](ctx context.Context, q rowQuerier, query string, args ...any) (T, error) {
	return getJSONTx[T](ctx, nil, q, query, args...)
}
func getJSONTx[T any](ctx context.Context, tx pgx.Tx, q rowQuerier, query string, args ...any) (T, error) {
	var zero T
	var payload []byte
	if tx != nil {
		q = tx
	}
	if err := q.QueryRow(ctx, query, args...).Scan(&payload); err != nil {
		return zero, notFound(err)
	}
	return decode[T](payload)
}
func listJSON[T any](ctx context.Context, q rowsQuerier, query string) ([]T, error) {
	rows, err := q.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []T{}
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		item, err := decode[T](payload)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
