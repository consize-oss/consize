package store

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var postgresMigrations embed.FS

type sqlMigration struct {
	Version  int
	Name     string
	SQL      string
	Checksum string
}

func loadPostgresMigrations() ([]sqlMigration, error) {
	entries, err := postgresMigrations.ReadDir("migrations")
	if err != nil {
		return nil, err
	}
	out := make([]sqlMigration, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		prefix, _, ok := strings.Cut(entry.Name(), "_")
		if !ok {
			return nil, fmt.Errorf("migration %q must begin with a numeric version", entry.Name())
		}
		version, err := strconv.Atoi(prefix)
		if err != nil || version < 1 {
			return nil, fmt.Errorf("migration %q has invalid version", entry.Name())
		}
		contents, err := postgresMigrations.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return nil, err
		}
		digest := sha256.Sum256(contents)
		out = append(out, sqlMigration{Version: version, Name: entry.Name(), SQL: string(contents), Checksum: hex.EncodeToString(digest[:])})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	for i, migration := range out {
		if migration.Version != i+1 {
			return nil, fmt.Errorf("migration sequence is not contiguous at %q", migration.Name)
		}
	}
	return out, nil
}

func migratePostgres(ctx context.Context, pool *pgxpool.Pool) ([]MigrationRecord, error) {
	migrations, err := loadPostgresMigrations()
	if err != nil {
		return nil, err
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(20261007)`); err != nil {
		return nil, fmt.Errorf("lock schema migration: %w", err)
	}
	if _, err := tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
        version integer PRIMARY KEY,
        name text NOT NULL,
        checksum text NOT NULL,
        applied_at timestamptz NOT NULL
    )`); err != nil {
		return nil, fmt.Errorf("create migration ledger: %w", err)
	}
	rows, err := tx.Query(ctx, `SELECT version, name, checksum FROM schema_migrations ORDER BY version`)
	if err != nil {
		return nil, err
	}
	applied := map[int]sqlMigration{}
	for rows.Next() {
		var migration sqlMigration
		if err := rows.Scan(&migration.Version, &migration.Name, &migration.Checksum); err != nil {
			rows.Close()
			return nil, err
		}
		applied[migration.Version] = migration
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for version := range applied {
		if version < 1 || version > len(migrations) {
			return nil, fmt.Errorf("database schema version %d is newer than supported version %d", version, len(migrations))
		}
	}
	for version := 1; version <= len(applied); version++ {
		if _, ok := applied[version]; !ok {
			return nil, fmt.Errorf("database migration ledger has a gap at version %d", version)
		}
	}
	result := make([]MigrationRecord, 0, len(migrations))
	for _, migration := range migrations {
		if previous, ok := applied[migration.Version]; ok {
			if previous.Name != migration.Name || previous.Checksum != migration.Checksum {
				return nil, fmt.Errorf("migration %03d was changed after application; restore the reviewed migration or a compatible backup", migration.Version)
			}
			continue
		}
		if _, err := tx.Exec(ctx, migration.SQL, pgx.QueryExecModeSimpleProtocol); err != nil {
			return nil, fmt.Errorf("apply migration %s: %w", migration.Name, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations(version, name, checksum, applied_at) VALUES ($1,$2,$3,$4)`, migration.Version, migration.Name, migration.Checksum, time.Now().UTC()); err != nil {
			return nil, err
		}
		result = append(result, MigrationRecord{FromVersion: migration.Version - 1, ToVersion: migration.Version, Name: migration.Name})
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return result, nil
}
