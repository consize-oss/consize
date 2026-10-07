---
description: Get started with Consize to safely identify and optimize cloud, AI and SaaS infrastructure waste through recommendations, reviewable changes, and automatic rollback.
---

# Get started

Consize helps engineering teams reduce infrastructure waste without turning cost optimization into a production risk.

It analyzes Kubernetes workloads and cloud resources, recommends safer changes, and lets teams either open a reviewable Infrastructure-as-Code pull request or apply a guarded runtime change.

## Choose your path

=== "I want to see it work first"

    No cluster, no cloud account, just Docker. This is the fastest way to understand the `Observe` → `Analyze` → `Recommend` → `Review` → `Apply` → `Verify loop`, including watching an automatic rollback happen.

    [Try the Interactive Sandbox](sandbox.md){ .md-button .md-button--primary }

=== "I'm ready to install it"

    The steps below get a minimal instance running against a real cluster. For RBAC, namespace scoping, GitHub/Slack integrations, and cloud provider credentials.
    
    [Production Installation](installation.md){ .md-button .md-button--primary }

## Before you start

You need:

* A Kubernetes cluster
* `kubectl` installed and configured
* Helm installed
* Prometheus or a compatible metrics endpoint

Optional integrations such as GitHub, Slack, and cloud provider credentials can be configured later, see [Production Installation](installation.md).

## 1. Create the Consize namespace

```sh
--8<-- "create-namespace.sh"
```

## 2. Configure your metrics connection

Create the required secret with your Prometheus endpoint:

```sh
--8<-- "create-metrics-secret.sh"
```

> If you use an external Postgres database, configure it separately. Consize can provision a lightweight Postgres database automatically.

## 3. Deploy Consize

Helm is the recommended production installation method. Install directly from the published chart on GitHub Container Registry (GHCR):

```sh
--8<-- "helm-install-oci.sh"
```

For more installation options, including cloud credentials, namespace scoping, RBAC, GitHub, and Slack, see [Production Installation](installation.md).

## 4. Verify the installation

--8<-- "verify-install.md"

## 5. Understand the workflow

Consize follows one basic loop, shown in full (including the rollback path) on the [homepage](../index.md#how-it-works):

```
Observe → Analyze → Recommend → Review → Apply → Verify
```


Consize observes your workloads, analyzes resource usage, and identifies optimization opportunities. Depending on your configuration, the resulting change either goes through a review workflow or is applied directly within configured safety boundaries.

Before enabling automatic changes, review the recommendations Consize produces. For each one, look at:

* Which workload is being optimized
* Which resource is being changed
* The current allocation
* The recommended allocation
* Why the change was recommended

This gives you a chance to understand how Consize behaves before increasing automation.

## 6. Try Consize

Once Consize is running, you can explore how it works without making changes to a production environment.

Try the [Interactive Sandbox](sandbox.md).

## Something not working?

Check [Troubleshooting](../resources/troubleshooting.md) for the most common install issues, or the [FAQ](../resources/faq.md). If neither covers it, see [Support](../resources/support.md) for where to ask.

## Next steps

* [Interactive Sandbox](sandbox.md)
* [Production Installation](installation.md)
* [How Consize Works](../concepts/architecture.md)
* [Kubernetes Rightsizing](../guides/rightsizing.md)
* [Configuration](../reference/configuration.md)