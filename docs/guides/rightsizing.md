---
description: Learn how Consize uses Kubernetes resource usage data and percentiles to identify safer rightsizing opportunities.
---

# Kubernetes Rightsizing

Kubernetes workloads are often given more CPU and memory than they actually need.

Rightsizing helps reduce that waste by adjusting resource requests and limits based on observed workload behavior.

Consize helps identify these opportunities and turn them into safer, actionable changes.

## What Consize looks at

Consize analyzes workload resource usage, including:

* CPU usage
* Memory usage
* CPU requests
* Memory requests
* CPU limits
* Memory limits
* Workload configuration
* Historical usage patterns

The goal is to compare **what a workload has been allocated** with **what it actually uses**.

### Why percentiles, not averages

Consize sizes requests from the **p95** of usage and limits from the **p99**, both computed over a 14-day window of 15-minute buckets, with explicit headroom multipliers on top. It never uses a plain average.

Averages hide bursts, a workload that spikes to 2Gi once a day but averages 500Mi would get under-provisioned and start OOM-killing under an average-based policy. Maxes overcorrect the other way and recommend sizes so generous there's no real savings. Percentiles sit in between: robust to spikes and weekly patterns, without chasing a single outlier.

This means Consize's recommendations are sometimes slightly larger than a naive average-based tool would suggest. That's intentional, the goal is *safe* savings, not maximal savings.

## A simple example

Imagine a deployment has:

```yaml
resources:
  requests:
    cpu: "1000m"
    memory: "2Gi"
```

But the workload consistently uses much less than that.

Consize can identify the difference as a potential optimization opportunity.

Instead of immediately changing the workload, the recommendation can go through the configured review and safety process.

## The rightsizing workflow

Consize follows the same core loop described on the [homepage](../index.md#how-it-works):
```
Observe → Analyze → Recommend → Review → Apply → Verify
```


## Recommendations

A recommendation should answer three questions:

### What should change?

For example:

```text
CPU request: 1000m → 500m
Memory request: 2Gi → 1Gi
```

### Why should it change?

The recommendation is based on observed resource usage (p95/p99 over a 14-day window, see above) and the [configured optimization strategy](../reference/configuration.md).

### Is the change safe?

Consize evaluates the proposed change against its configured safety boundaries before it can be applied automatically.

## Applying the change

Consize supports two ways to apply a recommendation. You don't have to pick one globally, either can be used per namespace, see [Environments](environments.md).

=== "Review before applying"

    Keep rightsizing inside your existing Infrastructure-as-Code workflow.

    A typical workflow is:

```text
    Consize
       ↓
    Recommendation
       ↓
    Infrastructure-as-Code change
       ↓
    Pull request
       ↓
    Review
       ↓
    Merge
       ↓
    Deploy
```

    This is useful for teams that want optimization recommendations without giving an automated system direct write access to production.

=== "Direct runtime changes"

    Teams that enable direct application can allow Consize to update supported workloads directly.

    Write access should be limited to the namespaces and resources where runtime optimization is allowed.

    See [The Safety Net](../concepts/safety-net.md) for more information about permissions and guardrails.

## Start with a small scope

You do not need to optimize every workload at once.

Start with:

1. A small number of namespaces
2. A few representative workloads
3. Recommendations before automatic application
4. A review of the resulting changes
5. Gradual expansion once the results are understood

This makes it easier to validate the optimization process before increasing automation.

## Monitoring after a change

Rightsizing should not end when the resource configuration changes.

After a change, monitor the workload for:

* Increased CPU pressure
* Increased memory pressure
* Restarts
* Failed deployments
* Increased latency
* Other application-specific signals

Consize's verification workflow helps close this loop.

## Next steps

* [Try it in the Interactive Sandbox](../getting-started/sandbox.md)
* [The Safety Net](../concepts/safety-net.md)
* [How Consize Works](../concepts/architecture.md)
* [Production Installation](../getting-started/installation.md)
* [Configuration](../reference/configuration.md)
* [FAQ](../resources/faq.md) for questions about how recommendations are computed