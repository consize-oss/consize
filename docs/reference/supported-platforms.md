---
description: Check which Cloud, AI, and SaaS environments, providers, metrics systems, and workload types Consize currently supports before installing.
---

# Supported Platforms

A quick reference for what Consize can talk to today, so you can check fit before installing.

## Kubernetes

| Requirement | Details |
|---|---|
| Cluster | Any standard Kubernetes cluster reachable via `kubectl`/Helm. No distribution-specific dependencies. |
| Metrics | Prometheus, or anything that exposes a Prometheus-compatible query endpoint. |
| Workload types | Deployments, StatefulSets, DaemonSets, ReplicaSets (read), Deployments (direct apply). |

## Cloud providers

| Provider | Cloud database rightsizing | Cloud waste scanning |
|---|---|---|
| AWS | ✅ Amazon RDS (via CloudWatch) | ✅ Unattached EBS volumes, stopped EC2 instances |
| GCP | ✅ Cloud SQL (via Cloud Monitoring) | ✅ Unattached Persistent Disks, stopped Compute instances |
| Azure | ❌ Not yet | ❌ Not yet |

Both are opt-in and off by default, see [Configuration](configuration.md#cloud-database-metrics). For the exact permissions each one needs, see [Production Installation](../getting-started/installation.md#cloud-provider-credentials).

Additional providers are tracked on the [Roadmap](../resources/roadmap.md#future-areas), not committed to a date.

## Infrastructure-as-Code

Consize can generate rightsizing pull requests against Terraform, for both Kubernetes workloads and cloud waste cleanups (the generated diffs target `.tf` files directly). YAML-based GitOps workflows are supported for direct Kubernetes manifest changes.

## Integrations

| Integration | What it does |
|---|---|
| GitHub | Opens pull requests for reviewable changes, see [Production Installation](../getting-started/installation.md#github). |
| Slack | Sends notifications for recommendations, applies, and rollbacks, see [Production Installation](../getting-started/installation.md#slack). |

Nothing else is wired up yet, if you need a different notification or IaC target, say so in [Support](../resources/support.md), it helps prioritize the roadmap.

## Next steps

* [Production Installation](../getting-started/installation.md)
* [Configuration](configuration.md)
* [Roadmap](../resources/roadmap.md)