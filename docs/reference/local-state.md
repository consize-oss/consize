# Local State and Recovery

Consize must remember work across process restarts. The v0.3 alpha stores that
information in one local JSON file so a stopped API can resume recommendations,
actions, verification, and recovery instead of silently forgetting them.

This storage option is intended for local development, demonstrations, and
simple single-process alpha testing. It is not the final production persistence
design and must not be used to run multiple Consize API or worker processes.

## What the file contains

The configured `state_path` identifies the state file. The local development
configuration uses:

```text
.consize/local-state.json
```

It contains:

- discovered resource records;
- generated recommendations and their status;
- action and audit events held by the durable store;
- pending action, verification, rollback, and recovery jobs;
- sequence information used to avoid duplicate records.

The configured JSONL audit output is a separate file. Back up that file as
well when it matters to the test or investigation you are preserving.

The store permits one owning process. A message that the state is already
owned usually means another API or worker is using the same file.

## Why an upgrade can stop startup

State schema v3 requires enough immutable provider information to distinguish
resources safely. For Kubernetes, that includes the provider, cluster identity,
location, resource type, and namespace/name provider reference.

An earlier alpha record might identify only `payments/checkout-api`. That name
can exist in several clusters. Consize therefore cannot safely decide whether
old recommendations and pending work belong to production, staging, or another
cluster.

When immutable identity is missing, startup fails with an error similar to:

```text
invalid durable resource "k8s:payments:checkout-api": invalid resource: account is required
```

This is deliberate. Consize does not invent a cluster, account, provider, or
location because doing so could attach an action to the wrong infrastructure.

## Before making any change

1. Stop the Consize API and worker processes.
2. Confirm no process still owns the state file.
3. Create a timestamped backup.
4. Inspect the reported record without printing credentials or unrelated
   provider metadata into a shared log.
5. Decide whether the state is disposable or must be preserved.

From the repository root:

```bash
STATE=.consize/local-state.json
STAMP=$(date -u +%Y%m%dT%H%M%SZ)

test -f "$STATE"
cp "$STATE" "$STATE.$STAMP.bak"
shasum -a 256 "$STATE" "$STATE.$STAMP.bak"
```

The two checksums must match. Keep the backup outside short-lived containers
or ephemeral volumes if you need it after the environment is removed.

To view only the state version and record counts:

```bash
jq '{
  version,
  resources: (.resources | length),
  recommendations: (.recommendations | length),
  actions: (.actions | length),
  jobs: (.jobs | length)
}' "$STATE"
```

To list records missing immutable identity fields:

```bash
jq -r '
  .resources
  | to_entries[]
  | select(
      (.value.provider // "") == "" or
      (.value.account // "") == "" or
      (.value.region // "") == "" or
      (.value.type // "") == "" or
      (.value.provider_resource_id // "") == ""
    )
  | .key
' "$STATE"
```

Do not modify the file with `jq`, a text editor, or a script while Consize is
running. Do not guess missing identity values.

## Choose the correct recovery path

### Disposable local development state

Reset is appropriate only when all of the following are true:

- the file belongs to a local lab, demo, or throwaway alpha environment;
- no action or rollback must resume;
- recommendation and audit history is not required;
- the resources can be rediscovered from the provider;
- you have created and verified a backup.

Archive the file instead of deleting it:

```bash
STATE=.consize/local-state.json
STAMP=$(date -u +%Y%m%dT%H%M%SZ)

mkdir -p .consize/archive
mv "$STATE" ".consize/archive/local-state.$STAMP.json"
```

Before restarting, verify the Kubernetes identity in
`.consize/local-dev.config.json`:

```json
{
  "kubernetes": {
    "enabled": true,
    "kubeconfig": "/absolute/path/to/.kube/config",
    "cluster_id": "docker-desktop",
    "location": "local"
  }
}
```

`cluster_id` must identify the actual cluster and remain stable across
restarts. `location` must be an explicit region or scope such as `us-east-1`
or `local`; it must not be inferred from a resource name.

Restart the API with the same configuration, then run discovery again:

```bash
go run ./cmd/consize serve \
  -config .consize/local-dev.config.json \
  -demo=false \
  -addr 127.0.0.1:8080
```

In another terminal:

```bash
curl -fsS -X POST http://127.0.0.1:8080/api/discovery
curl -fsS http://127.0.0.1:8080/api/resources
```

Confirm that the rediscovered resource uses the intended cluster and location
before generating or approving another recommendation.

### State that must be preserved

Do not reset the file when it contains pending actions, verification,
rollback, unresolved recovery, or audit history you need to retain.

The current alpha does not provide a supported command for repairing missing
immutable identity in place. Keep the original file and verified backup, record
the exact startup error and Consize commit or version, and ask a maintainer to
review the migration. A repair must preserve references between the resource,
recommendations, jobs, and audit events; changing only one JSON field is not a
safe migration.

When asking for support, do not attach the complete state file publicly. It can
contain infrastructure names, ownership, configuration, and provider metadata.
Share only the redacted error and affected resource ID unless a private review
explicitly requires more.

## Restoring the backup

Stop Consize before restoration. Preserve the failed or newly generated file
for investigation, then copy the verified backup into the configured path:

```bash
STATE=.consize/local-state.json
BACKUP=.consize/local-state.json.YYYYMMDDTHHMMSSZ.bak

test -f "$BACKUP"
cp "$BACKUP" "$STATE"
```

Restoring an incompatible backup does not make it compatible; it only returns
the environment to the preserved state for review or a supported migration.

## Current limitations

- the local file store supports one owner process;
- it needs persistent host or volume storage to survive container replacement;
- backup and restore are operator-managed;
- incomplete immutable identity fails closed;
- there is no supported in-place repair command in the current alpha;
- PostgreSQL and production storage operations are future work.

These limitations are acceptable for the documented alpha development path.
They are not production guarantees.
