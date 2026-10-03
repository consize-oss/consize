# Troubleshooting

Organized by what you're seeing, not by component. If your question is conceptual rather than broken, see the [FAQ](faq.md) instead.

## Installation

### Consize stops with `invalid durable resource`

The v0.3 resource migration refuses to guess missing provider, cluster,
location, type, or provider resource identity. Do not immediately delete or
manually edit the state file: it may contain pending verification, rollback,
or audit information.

Follow [Local State and Recovery](../reference/local-state.md) to stop the
process, create and verify a backup, inspect the affected record, and choose
between a disposable-development reset and a maintainer-reviewed migration.

### `curl http://127.0.0.1:18099/readyz` doesn't return `{"status":"ready"}`

1. Check the pods actually came up:

```sh
    kubectl -n consize-system get pods
```

    If a pod is `CrashLoopBackOff` or `Pending`, check its logs and events:

```sh
    kubectl -n consize-system logs <pod-name>
    kubectl -n consize-system describe pod <pod-name>
```

2. If the pods are `Running` but `readyz` still fails, the API usually can't reach its dependencies yet. Check the Prometheus secret is actually set:

```sh
    kubectl -n consize-system get secret consize-store -o jsonpath='{.data.prometheus-url}' | base64 -d
```

    and confirm that URL is reachable from inside the cluster, not just from your machine.

3. If the port-forward itself hangs or refuses the connection, confirm the service name matches your release name (`consize-api` assumes you installed the chart as `consize`, see [Production Installation](../getting-started/installation.md#6-install-consize-with-helm)):

```sh
    kubectl -n consize-system get svc
```

### `helm install` fails with a permissions or namespace error

Confirm the namespace exists and your current `kubectl` context has permission to create resources in it:

```sh
kubectl config current-context
kubectl auth can-i create deployments -n consize-system
```

### Cronjobs exist but never seem to run

```sh
kubectl -n consize-system get cronjobs
kubectl -n consize-system get jobs
```

If jobs aren't being created at all, check the cronjob's schedule and that the Kubernetes cronjob controller isn't suspended cluster-wide. If jobs are created but fail, check their pod logs the same way as above.

## Recommendations

### No recommendations are showing up

Consize needs a minimum amount of historical data before it will generate a recommendation (`CONSIZE_MIN_DATA_DAYS`, 5 days by default, see [Configuration](../reference/configuration.md#collection)). A freshly installed instance won't have recommendations yet, this is expected, not a bug.

### A recommendation looks smaller than I expected

Sizing is based on p95 (requests) and p99 (limits) usage with headroom, not a plain average, so recommendations are intentionally more conservative than "reduce to what's used right now." See [why percentiles, not averages](../guides/rightsizing.md#why-percentiles-not-averages).

## Verification and rollback

### A change was applied but verification never completes

The verification window scales with the apply step (1 hour for step 1, 2 for step 2, and so on), see [Observability](../guides/observability.md#before-a-change). Check how far into that window you are before assuming something's stuck. If the window has clearly passed, check the verifier is running:

```sh
kubectl -n consize-system get cronjobs
```

### A change rolled back and I don't understand why

Check the verification run for that apply, it records which signal breached its threshold (restarts, OOM kills, evictions, CPU throttling, or an app-level SLI if you configured one) and for how long, see [Observability](../guides/observability.md#after-a-change) for the signal list and [The Safety Net](../concepts/safety-net.md#how-a-change-is-evaluated) for the decision matrix. Rollbacks are byte-identical to the pre-apply state on purpose, so this shouldn't have caused a second incident on top of the first.

## Cloud integrations

### AWS/GCP metrics aren't showing up

Confirm `CONSIZE_DBMETRICS` is actually set (it's `none` by default) and that the credentials you configured have the permissions listed in [Production Installation](../getting-started/installation.md#cloud-provider-credentials). A silent empty result is almost always a permissions or region/project mismatch, not a connectivity failure.

### GitHub pull requests aren't being created

Confirm the `consize-github` secret's token has repo write access to the target repository and hasn't expired, see [Production Installation](../getting-started/installation.md#github).

## Still stuck?

See [Support](support.md) for where to ask, include your Consize version and the output of `kubectl -n consize-system get pods` when you do, it's usually the first thing anyone will ask for.

## Next steps

* [FAQ](faq.md)
* [Configuration](../reference/configuration.md)
* [Local State and Recovery](../reference/local-state.md)
* [Support](support.md)
