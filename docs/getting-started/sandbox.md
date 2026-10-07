---
description: Explore Consize's optimization and rollback workflow locally with Docker, without a Kubernetes cluster or production credentials.
---

# Interactive Sandbox

The Interactive Sandbox lets you explore Consize's optimization workflow without setting up a production Kubernetes environment.

It is the fastest way to understand what Consize does before installing it in your own cluster. No Kubernetes cluster, no cloud account, and no production credentials required, just Docker.

## Run the sandbox

**Prerequisite:** Docker installed and running.

```sh
docker run -p 3000:3000 -p 8080:8080 -it ghcr.io/consize-oss/consize-sandbox:latest
```

Then open [http://localhost:3000](http://localhost:3000). The sandbox is pre-seeded with historical data, cloud waste opportunities, and a live metrics simulation, so there's real data to explore immediately.

## What you can explore

The sandbox demonstrates the core Consize workflow, shown in full (including the rollback path) on the [homepage](../index.md#how-it-works):

```
Observe → Analyze → Recommend → Review → Apply → Verify
```

Watch the Verifier catch an intentional regression on the `checkout-api` workload and trigger an automatic rollback, the same safety loop that runs in production, compressed into something you can see end to end in a few minutes.

## Why use the sandbox?

Use the sandbox if you want to:

* Understand how Consize works
* See a rightsizing recommendation
* Explore the safety checks
* Understand the approval and application flow
* Try Consize before installing it

## No production access required

The sandbox is designed for exploration. You do not need to connect it to your production Kubernetes cluster or provide production credentials. This makes it useful for evaluating the workflow before introducing Consize into a real environment.

## From the sandbox to production

Once you understand the workflow, you can install Consize in your own Kubernetes environment. The recommended path:

```
Interactive Sandbox → Production Installation → Recommendations → Controlled automation
```

## Next steps

* [Production Installation](installation.md)
* [Kubernetes Rightsizing](../guides/rightsizing.md)
* [The Safety Net](../concepts/safety-net.md)