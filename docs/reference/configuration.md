# Configuration

Consize can be configured to control what it observes, what it can change, and how it operates in your environment.

This page is the reference list of configuration options. For task-based walkthroughs, see [Production Installation](../getting-started/installation.md) and [Environments](../guides/environments.md).

## Durable storage

| Variable | Default | Description |
|---|---|---|
| `CONSIZE_DATABASE_URL` | unset | PostgreSQL connection URL shared by the API and worker. Required for production durable storage. Inject it from a secret and require TLS outside trusted local networks. |

The JSON `state_path` configuration is limited to the single-process local lab. PostgreSQL owns production migrations, constraints, restart recovery, and concurrent API/worker access. See [Durable storage and schema evolution](../engineering/durable-storage.md).

## Collection

| Variable | Default | Description |
|---|---|---|
| `CONSIZE_NAMESPACES` | empty (cluster-wide) | Comma-separated namespaces to scope collection to, e.g. `boutique,payments,checkout` |
| `CONSIZE_MIN_DATA_DAYS` | `5` | Minimum distinct days of data required before a recommendation is generated. Confidence scores scale with data volume regardless of this setting. |

## Verification

| Variable | Default | Description |
|---|---|---|
| `CONSIZE_SUSTAINED_MINUTES` | `5` | How long a signal must stay past its regression threshold before it counts as a breach and triggers rollback |
| `CONSIZE_SLI_ERROR_EXPR` | unset (off) | Optional PromQL expression for an app-level error-rate signal. Off by default until you provide app-level labels. |
| `CONSIZE_SLI_P99_EXPR` | unset (off) | Optional PromQL expression for an app-level p99 latency signal |

Without zero-instrumentation SLIs (restarts, OOM kills, evictions, CPU throttling), verification still runs, see [The Safety Net](../concepts/safety-net.md) for the default signal set and verdict logic.

## Cloud database metrics

| Variable | Default | Description |
|---|---|---|
| `CONSIZE_DBMETRICS` | `none` (Kubernetes-only) | Selects the DB metrics source: `none`, `cloudwatch` (AWS RDS), or `cloudmonitoring` (GCP Cloud SQL) |
| `CONSIZE_AWS_REGION` | `us-east-1` | AWS region, used when `CONSIZE_DBMETRICS=cloudwatch` |
| `CONSIZE_GCP_PROJECT` | inferred from service account key | GCP project, used when `CONSIZE_DBMETRICS=cloudmonitoring` |
| `CONSIZE_DB_FILTER` | unset | Optional filter limiting which DB instances are collected |

For the exact IAM permissions these require, see [Production Installation](../getting-started/installation.md#cloud-provider-credentials). For which providers and services are supported at all, see [Supported Platforms](supported-platforms.md).

## Recommendation retention

| Variable | Default | Description |
|---|---|---|
| `CONSIZE_REC_RETENTION` | `168h` (7 days) | How long superseded recommendations are kept before pruning. Applied, verified, rolled-back, and pending recommendations are never pruned. |

## Authentication

| Variable | Default | Description |
|---|---|---|
| `CONSIZE_AUTH_REQUIRED` | `false` | Whether login is enforced on the API/UI |
| `CONSIZE_BOOTSTRAP_ADMIN` | unset | `"email:password"`, creates the first admin account, and only while the users table is empty |

## Namespace and RBAC scoping

Collection scope, write scope, and automatic-application eligibility are configured through Helm values and Kubernetes labels rather than environment variables, see [Environments](../guides/environments.md) for the `collector.namespaces` / `rbac.writer.namespaces` values and the `consize.savings.dev/auto-apply` label.

## Secrets

Metrics connection, Slack notifications, and GitHub integration are configured through Kubernetes secrets (`consize-store`, `consize-alerts`, `consize-github`), see [Production Installation](../getting-started/installation.md#2-configure-your-metrics-connection) for the exact commands.

## Helm values

Production installations use Helm values to configure Consize. See [Production Installation](../getting-started/installation.md#6-install-consize-with-helm) for the install commands.

## Next steps

* [Production Installation](../getting-started/installation.md)
* [Supported Platforms](supported-platforms.md)
* [API Reference](api.md)
* [The Safety Net](../concepts/safety-net.md)
* [Environments](../guides/environments.md)
* [Kubernetes Rightsizing](../guides/rightsizing.md)
