# Durable storage and schema evolution

Consize uses PostgreSQL for production durable state. The API and worker may run as separate processes because they share one transactional database. The JSON `state_path` backend remains available for the single-process local lab and compatibility tests; it is not the production database.

## Configuration

Set `CONSIZE_DATABASE_URL` on both the API and worker. A `database_url` field is also accepted in local configuration, but production deployments should inject the value from a secret rather than commit it.

```sh
export CONSIZE_DATABASE_URL='postgres://consize@127.0.0.1:5432/consize?sslmode=require'
go run ./cmd/consize storage migrate
go run ./cmd/consize storage status
```

PostgreSQL storage commands have a 30-second operation timeout by default so a
stalled connection or migration lock cannot leave the CLI waiting forever. Use
`-timeout` to select a shorter or longer bounded duration when the environment
requires it, for example `storage status -timeout 10s`. Diagnostic JSON redacts
passwords found in connection URLs, keyword assignments, query parameters, and
structured error details; continue to treat the database URL itself as a secret.

Startup runs the same migration path before serving traffic or processing jobs. It fails closed if PostgreSQL cannot be reached, a migration checksum changed, the migration ledger has a gap, a migration fails, or the database is newer than the binary supports.

## Owned data

| Table | Purpose | Important relationships |
| --- | --- | --- |
| `resources` | Normalized infrastructure identity and observed state | Stable primary ID; unique provider identity |
| `recommendations` | Historical, evidence-backed proposals | Foreign key to resource |
| `actions` | Governed attempts to realize recommendations | Foreign keys to recommendation and resource; unique idempotency key |
| `action_events` | Append-only workflow and audit evidence | Foreign keys to action, recommendation, and resource |
| `jobs` | Recoverable apply, verify, and rollback work | Foreign keys to action, recommendation, and resource |
| `schema_migrations` | Applied migration name, version, checksum, and time | One immutable row per schema version |

Plugins do not own or alter core tables. Plugin-specific values are serialized inside the versioned core contracts.

## Migration rules

Migration files live in `internal/store/migrations` and are embedded into the binary. Their numeric versions are contiguous and their SHA-256 checksums are recorded when applied.

1. Consize acquires a PostgreSQL advisory transaction lock so only one process migrates at a time.
2. It validates every previously applied migration name and checksum.
3. It applies each pending migration in order inside one database transaction.
4. It records the migration only after its SQL succeeds.
5. It commits the schema and ledger together.

Never edit an applied migration. Add the next numbered file. A destructive migration must document impact, backup, validation, rollback or forward-repair procedure, and the operator confirmation required before release.

Identifiers and historical references must survive migration. Do not drop and recreate production tables to make a migration pass.

## Transaction boundaries

Consize uses database transactions and row locks for operations that span related entities. In particular:

- action idempotency is enforced by a unique database constraint;
- recommendation and action transitions lock the current record before validation;
- creating or updating a job writes the job, workflow event, recommendation state, action state, and affected resource state in one transaction;
- foreign keys prevent actions, jobs, and audit events from referring to missing owners;
- a partial or failed transaction is rolled back and is never reported as successful.

The active-job index prevents two unresolved actions for the same resource. `manual_intervention` remains unresolved and continues to reserve that resource until an operator resolves it.

## Health and diagnostics

`GET /api/health` includes the backend, durability, current and target schema versions, migration status, migrations applied during startup, required indexes, and entity counts. It never returns the database URL or credentials.

## Upgrade procedure

1. Read the release migration notes and confirm that the current version is supported.
2. Create and verify a PostgreSQL backup.
3. Stop mutation workers or enter maintenance mode when the release requires it.
4. Run `consize storage migrate`, or allow one new instance to perform startup migration.
5. Check `consize storage status` and `/api/health`.
6. Start the remaining API and worker instances.
7. Exercise resource, recommendation, action, verification, and audit reads before reopening writes.

Do not start an older binary after a schema upgrade unless that release explicitly documents backward schema compatibility.

## Backup

Backups are PostgreSQL operations, not schema migrations. Use a credential with read access and store the artifact outside the application volume.

```sh
pg_dump --format=custom --no-owner --no-acl \
  --dbname="$CONSIZE_DATABASE_URL" \
  --file="consize-$(date -u +%Y%m%dT%H%M%SZ).dump"
```

Test restoration regularly. A dump that has never been restored is not sufficient recovery evidence.

## Restore

Restore into a new empty database first; do not overwrite the only copy of a failed database.

```sh
createdb consize_restore
pg_restore --exit-on-error --no-owner --no-acl \
  --dbname=consize_restore \
  consize-YYYYMMDDTHHMMSSZ.dump
```

Point a compatible Consize binary at the restored database, run `storage status`, and validate entity counts and a representative workflow before changing production connectivity.

## Reset

Reset is destructive and is not an upgrade or restore mechanism. For local development, drop and recreate the disposable database or schema with an explicit operator command. Production reset is unsupported; preserve required audit and incident evidence and follow the recovery procedure instead.

The file backend has a separate guarded reset command:

```sh
go run ./cmd/consize storage reset \
  -state ./.consize/local-state.json \
  -confirm 'ERASE LOCAL STATE'
```

It archives the previous file rather than silently deleting it.

## Validation evidence

The backend CI job starts an isolated, digest-pinned PostgreSQL service and tests fresh migration, upgrade from the previous schema fixture, migration-ledger rejection, constraints and indexes, idempotency, restart recovery, and the ordinary file-backend unit and race suites.
