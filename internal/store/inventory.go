package store

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/consize-oss/consize/pkg/resource"
	"github.com/jackc/pgx/v5"
)

// InventorySnapshot is a coherent read of the existing persisted inputs, not a
// second registry or evidence collector. Never return it directly from an API.
type InventorySnapshot struct {
	Resources       []resource.Resource
	Recommendations []Recommendation
	Jobs            []Job
}

type InventoryReader interface {
	ReadInventory(context.Context) (InventorySnapshot, error)
}

const inventoryEntityLimit = 10000
const inventoryByteLimit = 32 << 20

var ErrInventoryLimit = errors.New("inventory read exceeds its safety budget")

func (m *Memory) ReadInventory(ctx context.Context) (InventorySnapshot, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out InventorySnapshot
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if m.poison != nil {
		return out, m.poison
	}
	if len(m.resources) > inventoryEntityLimit || len(m.recs) > inventoryEntityLimit || len(m.jobs) > inventoryEntityLimit {
		return out, ErrInventoryLimit
	}
	remaining := inventoryByteLimit
	// Serialize each entity separately to bound the allocation and detach all
	// maps/slices before releasing the store lock.
	for _, v := range m.resources {
		if err := appendInventory(ctx, &out.Resources, v, &remaining); err != nil {
			return InventorySnapshot{}, err
		}
	}
	for _, v := range m.recs {
		if err := appendInventory(ctx, &out.Recommendations, v, &remaining); err != nil {
			return InventorySnapshot{}, err
		}
	}
	for _, v := range m.jobs {
		if err := appendInventory(ctx, &out.Jobs, v, &remaining); err != nil {
			return InventorySnapshot{}, err
		}
	}
	sortInventory(&out)
	return out, nil
}

func appendInventory[T any](ctx context.Context, out *[]T, v T, remaining *int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return decodeInventory(out, b, remaining)
}

func decodeInventory[T any](out *[]T, b []byte, remaining *int) error {
	*remaining -= len(b)
	if *remaining < 0 || len(*out) >= inventoryEntityLimit {
		return ErrInventoryLimit
	}
	var v T
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*out = append(*out, v)
	return nil
}

func sortInventory(out *InventorySnapshot) {
	sort.Slice(out.Resources, func(i, j int) bool { return out.Resources[i].ID < out.Resources[j].ID })
	sort.Slice(out.Recommendations, func(i, j int) bool { return out.Recommendations[i].ID < out.Recommendations[j].ID })
	sort.Slice(out.Jobs, func(i, j int) bool { return out.Jobs[i].ID < out.Jobs[j].ID })
}

func (p *Postgres) ReadInventory(ctx context.Context) (InventorySnapshot, error) {
	var out InventorySnapshot
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return out, err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	remaining := inventoryByteLimit
	// Constant statements only: user filters never become SQL text. Limit+1
	// detects oversized inventories rather than silently publishing partial totals.
	if err = readInventoryRows(ctx, tx, `SELECT payload FROM resources ORDER BY id LIMIT 10001`, &out.Resources, &remaining); err != nil {
		return InventorySnapshot{}, err
	}
	if err = readInventoryRows(ctx, tx, `SELECT payload FROM recommendations ORDER BY id LIMIT 10001`, &out.Recommendations, &remaining); err != nil {
		return InventorySnapshot{}, err
	}
	if err = readInventoryRows(ctx, tx, `SELECT payload FROM jobs ORDER BY id LIMIT 10001`, &out.Jobs, &remaining); err != nil {
		return InventorySnapshot{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return InventorySnapshot{}, err
	}
	return out, nil
}

func readInventoryRows[T any](ctx context.Context, tx pgx.Tx, query string, out *[]T, remaining *int) error {
	rows, err := tx.Query(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var b []byte
		if err = rows.Scan(&b); err != nil {
			return err
		}
		if err = decodeInventory(out, b, remaining); err != nil {
			return err
		}
	}
	return rows.Err()
}
