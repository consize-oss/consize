---
description: Learn how Consize evaluates optimization changes with safety checks, guardrails, and post-change verification to reduce production risk.
---

# The Safety Net

Infrastructure optimization is only useful if the changes are safe.

Consize is designed around a simple principle:

> Optimize infrastructure without turning cost savings into production risk.

The safety layer evaluates proposed changes before they are applied and verifies workloads after changes are made.

## How the safety net works

```text
Proposed change
      ↓
Safety checks
      ↓
Approved?
   ↙       ↘
 No         Yes
 ↓           ↓
Stop       Apply
             ↓
         Verify
             ↓
Healthy → continue | Breach → roll back
```

## Guardrails

Consize can enforce boundaries around what it is allowed to change.

These boundaries can include:

* Which namespaces can be analyzed
* Which resources can be modified
* Which workloads can receive automatic changes
* Which Kubernetes permissions Consize has
* Whether changes require review before application

This means teams can start with a narrow scope and expand automation gradually.

### How a change is evaluated

Every proposed change is checked against a fixed decision matrix before it's allowed to proceed:

| Condition | Outcome |
|---|---|
| Workload is excluded, but change requires auto-apply | **BLOCK** |
| Step size exceeds 30% of current allocation | **SPLIT** into smaller sub-steps |
| Target namespace is protected | **BLOCK** |
| Another apply is already in progress in the namespace | **REJECT** |
| No approval given, and auto-apply isn't enabled | **WAIT_APPROVAL** |
| Dry-run mode | No write call is issued |

This is deterministic, not a judgment call the system makes at apply time, the same input always produces the same outcome, which is what makes the safety guarantees testable.

## Read access and write access

Consize separates observation from modification.

### Read access

The collector needs permission to observe resources and gather the information required for analysis.

Read access does not allow Consize to modify workloads.

### Write access

Direct runtime changes require additional Kubernetes permissions.

Teams can restrict these permissions to specific namespaces and resources.

For example, Consize can have read access across a cluster while only having write access to a specific namespace.

This follows the principle of least privilege.

## Controlled automation

Automation does not have to mean unlimited access.

Teams can decide:

* What Consize can observe
* What Consize can change
* Where changes can happen
* Whether changes require review
* Which namespaces can use automatic application

This allows teams to increase automation without giving the system unrestricted control over the cluster.

## Apply stepping

Consize never applies a large change in one go. If a step would exceed 30% of the current allocation, it is split into smaller sub-steps (see the decision matrix above), and each increment is verified before the next one proceeds.

The goal is to reduce the chance that an aggressive optimization causes an unexpected production impact.

## Verification

After every change, Consize monitors SLIs such as OOM kills and CPU throttling. If a threshold is breached, it triggers an automatic rollback that restores the previous state byte-for-byte.

The workflow becomes:

```text
Observe
   ↓
Recommend
   ↓
Check safety
   ↓
Apply
   ↓
Verify
   ↓
Healthy → continue | Breach → roll back
```

Verification creates a feedback loop instead of treating optimization as a one-time action.

## Reviewable changes

Teams that prefer Infrastructure-as-Code workflows can keep humans in the approval loop.

Instead of applying a change directly, Consize can produce a change that goes through your existing pull request and review process.

This gives teams visibility into:

* What is changing
* Where it is changing
* Why the change was recommended
* When the change should be applied

## Start small

You do not need to automate everything immediately.

A typical adoption path can look like:

```text
Observe only
    ↓
Recommendations
    ↓
Reviewable changes
    ↓
Limited runtime changes
    ↓
Broader automation
```

Start with a small scope, understand the recommendations, and increase automation as your confidence grows.

## Next steps

* [How Consize Works](architecture.md)
* [Production Installation](../getting-started/installation.md)
* [Configuration](../reference/configuration.md)