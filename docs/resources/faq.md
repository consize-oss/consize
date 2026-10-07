---
description: Find answers to common questions about Consize recommendations, safety verification, rollbacks, supported optimization behavior, and current limitations.
---

# FAQ

Conceptual and "should I trust this" questions. If your command actually failed, see [Troubleshooting](troubleshooting.md) instead.

## Does Consize send my data anywhere?

No. Consize runs entirely inside your cluster and only talks to the Prometheus endpoint, Kubernetes API, and (if you enable them) the cloud provider APIs and integrations you configure yourself. There's no external telemetry or phone-home. See [The Safety Net](../concepts/safety-net.md) for the full permission model.

## What does a recommendation actually change?

Only CPU/memory `requests` and `limits` on the workload spec, nothing else. Container images, replica counts, and other spec fields are untouched. See [Kubernetes Rightsizing](../guides/rightsizing.md).

## Why did my recommendation come back with a verdict of `inconclusive` instead of `passed` or `failed`?

`inconclusive` means Consize didn't have enough post-apply data to prove the change was safe, not that anything went wrong. No automatic rollback happens in this case, but it's flagged for a human to look at. This usually means the verification window was too short for the workload's traffic pattern, or a signal (like an app-level SLI) wasn't reporting during that window. See [Observability](../guides/observability.md#after-a-change) for the verdicts and what triggers each one.

## Can Consize make a workload bigger, not just smaller?

Not in the current version, v1 only downsizes. If a workload is under-provisioned, Consize won't recommend increasing it. This is a deliberate scope decision, see [Decisions](../resources/decisions.md) for how significant decisions like this get made and documented.

## What happens if two recommendations apply to the same workload at once?

They don't. The safety-check decision matrix rejects an apply if another one is already in progress in the same namespace, see [The Safety Net](../concepts/safety-net.md#how-a-change-is-evaluated).

## Does Consize need write access to my whole cluster?

No, and by default it doesn't get any. Read access (for observation and analysis) and write access (for direct applies) are configured separately, and write access can be scoped down to specific namespaces, see [Environments](../guides/environments.md#read-scope-and-write-scope).

## What's the difference between the Interactive Sandbox and a real install?

The sandbox runs a single Docker container with synthetic pre-seeded data, no Kubernetes cluster or cloud credentials involved. It's for understanding the workflow, not for evaluating real recommendations. See [Interactive Sandbox](../getting-started/sandbox.md).

## Is there a CLI?

Not currently, Consize is operated through the UI, its REST API (see [API Reference](../reference/api.md)), and Kubernetes-native tooling (`kubectl`, Helm). If a CLI would help your workflow, mention it in [Support](support.md).

## Next steps

* [Troubleshooting](troubleshooting.md)
* [The Safety Net](../concepts/safety-net.md)
* [Support](support.md)