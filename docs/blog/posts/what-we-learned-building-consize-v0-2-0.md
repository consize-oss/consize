---
date: 2026-10-06
image: assets/blog/what-we-learned-v0-2-0.png
categories:
  - Announcements
  - Product
authors:
  - Ujunwa Njoku
slug: what-we-learned-building-consize-v0-2-0
description: >
  A candid retrospective on Consize v0.2.0: what we got right, what is still
  rough, and what v0.3.0 is building next.
---

# What we learned building the first version of Consize

![Consize v0.2.0 retrospective: stepwise rightsizing](../../assets/blog/what-we-learned-v0-2-0.png)

Consize v0.2.0 is out while v0.3.0 is still in progress...

<!-- more -->

It's a good moment
to be honest about how we got here. This isn't a launch post, but rather the list of
things we believed at the start, the ones that survived contact with reality,
and the ones we're still working on.

## Where we started

We began with a simple observation: teams waste 30 to 50% of their compute and
database spend, and it isn't because they can't see it. Dashboards will 
show a workload using a fraction of what it requested. The change just doesn't
get made, because the person who could make it is also the person who gets
paged if it goes wrong.

So the question we set out to answer wasn't "how do we find waste?" It was
"how do we make the fix safe enough that someone actually applies it?"

## Lesson 1: Safety is the product, not a feature

It's tempting to think the analysis engine is the heart of a rightsizing tool.
We don't think it is. The hard
engineering lives in what happens after someone clicks apply: the verifier, the
step-wise rollout, and the rollback.

That's where the design puts its weight. The question we care most about is "how
do we know this change is safe, and what do we do when it isn't?" rather than
"what number should we recommend?"

## Lesson 2: Deterministic beats clever

It would have been tempting to reach for machine learning. Instead, Consize
uses plain statistics over 14-day windows: CPU requests are set to p95 × 1.2,
and limits to the higher of 2× the request or p99.

We chose that on purpose. Infrastructure changes are consequential, and an
engineer who's about to approve one should be able to audit exactly why the
number is what it is. The analysis engine is also built as pure functions over stored
telemetry, with no I/O inside, so it's straightforward to unit-test.

A small rule matters more than it first appears: **Consize withholds
recommendations when it has less than 5 days of data.** It's unusual for a
tool's first behavior to sometimes be "I don't know yet," but a confident wrong
answer is the most expensive output a rightsizing tool can produce.

## Lesson 3: Never apply a big change all at once

If a workload is over-provisioned by 4x, the obvious fix is to cut it to the
right size in one step. It's also the fix most likely to hurt you. Consize
never changes a resource by more than 30% in a single apply, so a big
reduction happens over repeated, smaller steps. The verification window scales
with the step number too. With the default one-hour base, step 1 compares an hour
before and after the change, step 2 compares two hours, and so on, so later steps
get watched more closely.

Each step also uses `resourceVersion`-guarded patches. That keeps retries
idempotent, so a transient API failure can't cause a change to be applied twice,
and it stops Consize from racing with other controllers that touch the same
object. It's a boring detail, and the kind of thing you only add after you've
thought carefully about how production systems actually behave.

## Lesson 4: A rollback has to be byte-identical

"Roll back" sounds simple until you ask what you're rolling back to. If the
rollback restores something merely close to the original, you've traded one
change for another. Consize reverts to the exact pre-apply values, so the system
returns to the state it was in before it touched anything.

The verifier is also honest about what it doesn't know. If verification data is
missing, it waits up to two hours and then returns an inconclusive verdict
instead of marking the change verified.

This is also why the sandbox exists. We wanted anyone to be able to watch a
regression get caught and reversed without needing a cluster. When you apply
the recommendation for the seeded `checkout-api` workload, a background
injector spikes OOMKill metrics, and you watch the verifier catch it. 

```
docker run -p 3000:3000 -p 8080:8080 -it ghcr.io/consize-oss/consize-sandbox:latest
```

## Lesson 5: Trust is earned in stages

Asking a team to hand a tool write access to production on day one is a big ask.
So the defaults are conservative. Changes are dry-run by default, they need
explicit approval, and automatic apply only happens in namespaces that have
opted in with a label. If you'd rather not let Consize touch the cluster at all,
it can open reviewable pull requests against your IaC repo instead.

The same thinking drove our permission model. Consize uses separate reader and
writer service accounts, and the writer is only granted, with least-privilege
RBAC, in namespaces that explicitly opt in. Automatic apply additionally
requires an auto-apply label on the namespace. It makes onboarding a little
slower. We'd take that trade every time, because a tool that acts on
infrastructure should have to ask for that power, not assume it.

## Lesson 6: Databases need a shorter leash

In v0.2.0, Consize covers databases as well as workloads, and it treats them more
strictly. A database change moves one instance-class step at a time and never
skips two or more classes. It only happens inside the instance's maintenance
window, it needs approval unless the `auto-db` policy has been explicitly
enabled, and it's never applied to a primary that has no replicas, because there
would be no failover safety net.

Before picking a candidate class, Consize checks headroom on CPU, IOPS, free
memory, and connections. If nothing qualifies, it says "keep" and names the
resource that's the bottleneck.

## What's still rough

A retrospective that only lists wins isn't worth reading, so here's the honest
part.

- **We're early.** v0.2.0 supports AWS and GCP today. Additional cloud providers
  follow once the v0.3.0 foundation is qualified.
- **It's a single-cluster tool for now.** One cluster per deployment, and
  multi-cluster fleet management isn't built yet.
- **It only shrinks things.** Recommendations are downsize-only in v1, and
  upsizing is planned behind an explicit toggle. It also doesn't manage
  autoscaling policy such as HPA tuning, and it doesn't resize storage, queues,
  or serverless memory yet.
- **Policy is still simple.** Today's guardrails are namespace labels,
  exclusions, and step limits. v0.3.0 is meant to add policy decisions based on
  environment, risk, and available evidence.

## What's next

Consize ships narrow, then widens. v0.2.0 is the stable release and is maintained
on the `release/0.2` branch, while v0.3.0 is in active development on `main`. Its
goal is a safer, more extensible foundation for turning infrastructure evidence
into governed optimization actions.

The first v0.3.0 workflow targets Kubernetes Deployments and Prometheus. Broader
resource types (databases, data warehouses, storage, CI/CD, and GPU/LLM spend)
and additional cloud providers follow once that foundation is qualified.

Here's what's planned for v0.3.0:

- **Evidence-backed recommendations** that use real workload metrics to explain
  what should change and why.
- **Policy guardrails** that require approval, allow safe automation, or block a
  change based on environment, risk, and available evidence.
- **Safety headroom**, with configurable capacity above observed demand and a
  limit on the size of each optimization step.
- **Dry runs and reviewable plans**, so you can inspect proposed changes before
  they affect infrastructure.
- **Controlled Kubernetes remediation** through a single governed action path.
- **Post-action verification** that monitors workload health after every applied
  change.
- **Automatic rollback** when verification fails or an action can't complete
  safely.
- **Restart recovery**, so pending actions and verification continue after
  Consize restarts.
- **Durable audit history** covering recommendations, policy decisions, actions,
  verification results, and rollback outcomes.
- **Extensible plugins** for connecting more infrastructure, metrics, cost, and
  action providers through a common model.
- **Clear savings reporting** that keeps projected savings separate from savings
  confirmed by provider billing data.

You can follow progress on `main` against the
[roadmap](https://github.com/consize-oss/consize/blob/main/ROADMAP.md), and the
longer-term direction lives in the
[vision doc](https://github.com/consize-oss/consize/blob/main/VISION.md). For
larger proposals, open a GitHub Discussion and if you want to contribute, look for a `good first issue`.

And if you try the sandbox, we'd love to hear what broke, what confused you, and
what you wish it did. Tell us what you'd want guarding a change like this.

[Try the Interactive Sandbox](../../getting-started/sandbox.md){ .md-button .md-button--primary }
