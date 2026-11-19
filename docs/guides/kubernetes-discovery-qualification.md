# Kubernetes Discovery Qualification Guide

This guide documents the repeatable local-cluster qualification suite for
Kubernetes discovery and registration. It validates the end-to-end workflow from
real cluster discovery through mapping, durable registration, updates, RBAC
failures, restart recovery, lifecycle transitions (stale and tombstone), UID
replacement guards, and safe cleanup.

## Overview

The qualification suite exercises real Kubernetes API interactions against a
clean, isolated local control plane. It does not use seeded dashboard data, fake
clients, or mutate existing cluster resources.

Key properties:

- **Isolated control plane**: Always runs against a dedicated disposable Kind
  cluster. It refuses non-loopback endpoints and never uses your default
  `kubectl` context.
- **Least-privilege RBAC**: Creates a dedicated `ServiceAccount` with only `list`
  permission on `apps/deployments` in scoped namespaces.
- **Deterministic cleanup**: Verifies namespace deletion by checking UID
  preconditions and deletes the disposable cluster upon completion or failure.

## Prerequisites

Before running qualification, ensure you have installed:

1. **Go** `1.26.x` (matching [`go.mod`](../../go.mod))
2. **Kind** `v0.33.0` (`kind version`)
3. **Docker Engine** running locally with a Unix socket (`unix:///var/run/docker.sock`)

Check versions:

```bash
go version
kind version
docker info
```

## Running Qualification

Execute the single documented qualification command from the repository root:

```bash
scripts/test/discovery.sh
```

### Options

- `--output DIRECTORY`: Specify a custom directory for test logs and reports.
  If omitted, a temporary directory is created under `/tmp`.
- `--reuse-ci`: Reuse the isolated Kind cluster already provisioned by CI.
  Requires GitHub Actions run identity environment variables.

Example with custom output directory:

```bash
scripts/test/discovery.sh --output /tmp/discovery-qualification-results
```

## What the Suite Validates

The suite executes `TestIsolatedDiscoveryRegistryQualification` in
`internal/discovery`, validating each of the following lifecycle stages:

1. **Multi-namespace discovery**: Discovers zero-replica Deployments across
   distinct namespaces (`consize-qualification-a-*` and `consize-qualification-b-*`).
2. **Same-name workload isolation**: Proves Deployments with the same name across
   different namespaces receive distinct universal resource IDs (`ID` and `ProviderResourceID`).
3. **Label selector filtering**: Confirms workloads lacking matching labels are
   excluded from the registry.
4. **Inspectable durable runs**: Verifies manual rescan via `POST /api/discovery/runs`
   returns an asynchronous, inspectable operation.
5. **Metadata and sizing refresh**: Modifies Deployment labels and memory requests;
   verifies the existing registry record updates in-place without duplicating rows.
6. **Authorization failure handling**: Deletes the reader `RoleBinding`. Verifies
   that the failure records an `authorization` error code, health transitions to
   `degraded`, and existing registered resources remain intact (no false absences).
7. **Access restoration and recovery**: Re-creates the `RoleBinding`. Confirms
   health transitions back to `healthy` only after a complete, successful recovery scan.
8. **Process restart durability**: Stops the API server and worker, reopens the
   durable SQLite state, and resumes pending runs without duplicate records.
9. **Scoped absence and stale lifecycle**: Deletes a Deployment in one namespace;
   verifies the resource transitions to `stale` while workloads in other namespaces
   remain `active`.
10. **Tombstone lifecycle**: Verifies confirmed absence converts the resource to
    `deleted` (tombstone) without erasing audit history.
11. **UID replacement guard**: Re-creates a Deployment with the same name but a
    new Kubernetes UID. Confirms the registry rejects the collision (`registry_conflict`)
    without rewriting historical records.
12. **Negative credential checks**: Tests invalid credentials (`unauthorized`) and
    invalid contexts (`configuration`), confirming safe failures without fabricated data.
13. **Cleanup verification**: Deletes all fixture namespaces, waits for Kubernetes
    API confirmation, and destroys the Kind cluster.

## Expected Output and Reports

Upon completion, the suite outputs:

```text
Qualification output: /tmp/consize-discovery-qualification.XXXXXX
```

The output directory contains:

- `summary.json`: Structured machine-readable qualification summary.
- `test.log`: Complete output from `go test -v ./internal/discovery`.
- `cluster.log`: Kind cluster creation and teardown logs.
- `cluster-name.txt`: Name of the created disposable Kind cluster.

### Sample `summary.json`

```json
{
  "version": "discovery-qualification-v1",
  "passed": true,
  "steps": [
    "scheduled discovery registers distinct same-name Deployments",
    "manual rescan returns inspectable durable operation",
    "metadata and sizing refresh preserve registry identity",
    "authorization failure is durable and preserves lifecycle",
    "restored access requires a complete healthy recovery scan",
    "restart resumes captured run without duplicate resources",
    "complete scoped absence records the stale transition",
    "confirmed scoped deletion produces a registry tombstone",
    "same-name UID replacement is rejected without rewriting history",
    "unauthorized credential produces no authoritative observations",
    "invalid context fails without fabricated resources"
  ],
  "cleanup_verified": true
}
```

## Troubleshooting

### Refusing unapproved target / Non-loopback API

- **Cause**: `CONSIZE_DISCOVERY_TEST_CONTEXT` does not start with `kind-consize-discovery-`
  or the API endpoint is not a loopback address.
- **Fix**: Run via `scripts/test/discovery.sh`. Never point qualification at a
  remote or production Kubernetes context.

### Kind version mismatch

- **Cause**: Installed Kind version does not match `v0.33.0`.
- **Fix**: Install Kind 0.33.0 matching the pinned CI version:
  ```bash
  go install sigs.k8s.io/kind@v0.33.0
  ```

### Docker daemon unavailable

- **Cause**: Docker is not running or `DOCKER_HOST` points to a remote socket.
- **Fix**: Start Docker Desktop / local Docker engine. Qualification requires
  a local Unix socket endpoint (`unix://*`).

### RBAC failure or permission propagation delay

- **Cause**: RoleBinding propagation in Kubernetes informers is asynchronous.
- **Diagnostics**: Inspect `test.log`. The test includes polling loops for RBAC
  changes up to 40-50 iterations (100ms intervals). If persistent, check cluster
  API health in `cluster.log`.

### Cluster cleanup failed

- **Cause**: Docker daemon stopped unexpectedly or Kind was unable to delete
  the cluster.
- **Remediation**: Check `cluster-name.txt` in the output directory and manually
  delete the cluster:
  ```bash
  kind delete cluster --name "$(cat /tmp/consize-discovery-qualification.XXXXXX/cluster-name.txt)"
  ```

## Failure Diagnostics Example

When discovery encounters access or configuration failures, Consize classifies
them into safe error codes without raw server text or token leakage:

```json
{
  "id": "run-67890",
  "plugin_id": "kubernetes-action",
  "state": "failed",
  "error_code": "authorization",
  "error_message": "kubernetes discovery: access forbidden for configured scope",
  "reconciliation": {
    "discovered": 0,
    "updated": 0,
    "stale": [],
    "tombstoned": []
  }
}
```

Notice that:
- Error code is classified as `authorization` (safe for logs and metrics).
- Zero observations are returned; no partial or absent resources are reconciled.
- Existing active registry records are preserved without erroneous tombstoning.
