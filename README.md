<h1 align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="/docs/assets/logoreadme-white.png">
    <img width="300" src="/docs/assets/logoreadme-black.png" alt="consize">
  </picture>
</h1>

<p align="center">
  <b>The open source control plane for safe infrastructure optimization.</b>
</p>

<h4 align="center">
  <a href="https://docs.consizehq.com">Docs</a> |
  <a href="#try-the-interactive-sandbox">Sandbox</a> |
  <a href="https://github.com/consize-oss/consize/blob/main/VISION.md">Vision</a> |
  <a href="https://github.com/consize-oss/consize/blob/main/SECURITY.md">Security</a>
</h4>

<h4 align="center">
  <a href="https://github.com/consize-oss/consize/blob/main/LICENSE">
    <img src="https://img.shields.io/badge/License-Apache_2.0-blue.svg" alt="Consize is released under the Apache 2.0 license." />
  </a>
  <a href="https://consizetownhall.slack.com/">
    <img src="https://img.shields.io/badge/Community-Slack-4A154B.svg?logo=slack" alt="Consize community Slack" />
  </a>
</h4>

<img src="/img/demo-dashboard.png" width="100%" alt="Consize dashboard" />

**Consize** turns cost, usage, and health signals into infrastructure changes teams can review and trust. It keeps each recommendation tied to the resource and evidence behind it, checks policy before action, verifies workload health afterwards, and rolls back when a guarded change causes a regression.

Plugins handle provider-specific work such as discovery, metrics, cost estimates, and infrastructure actions. Consize controls when those plugins can run, so a new integration does not need to invent its own approval, recovery, and audit process.

```text
Discover → Recommend → Check policy → Plan → Apply → Verify → Recover
```

---

## Project Status

`v0.2.0` is the current release. The sandbox and Helm installation below use that version.

`v0.3.0-alpha` is in active development on `main`. It introduces the provider-neutral resource model, plugin SDK, durable action orchestration, policy checks, metrics-based verification, restart recovery, and rollback.

The current alpha works end to end with Kubernetes Deployments and Prometheus. Broader cloud discovery, GitOps execution, billing reconciliation, and the public plugin catalog are still being built. Cost values in the alpha are estimates; they are not presented as realized savings.

---

For source development, start with [core model contracts](docs/reference/core-model.md)
and [runtime data, empty startup and demo isolation](docs/engineering/runtime-data.md).

## Getting Started

| 🧪 Try the Sandbox | 🚀 Install v0.2.0 |
| --- | --- |
| Run everything locally in one container, with seeded data and a live regression to watch Consize catch and roll back. | Install the current release on a Kubernetes cluster with the official Helm chart. Read-only by default until you grant least-privilege `RoleBindings`. |

### Try the Interactive Sandbox

```bash
docker run -p 3000:3000 -p 8080:8080 -it ghcr.io/consize-oss/consize-sandbox:latest
```

Open `http://localhost:3000` and watch the Verifier catch an intentional regression on the `checkout-api` workload, then roll it back.

### Install v0.2.0

```bash
# 1. Export the default values to customize your installation
helm show values oci://ghcr.io/consize-oss/charts/consize > values.yaml

# 2. Install the chart using your customized values
helm install consize oci://ghcr.io/consize-oss/charts/consize \
  --version 0.2.0 \
  --namespace consize-system \
  --create-namespace \
  -f values.yaml
```

Consize uses read-only access for analysis and requires explicit, least-privilege, namespace-scoped `RoleBindings` before it can apply supported Kubernetes changes.

For Helm values, Service Accounts, and integration secrets, read the [installation documentation](https://docs.consizehq.com/getting-started/installation/#1-create-the-consize-namespace).

---

## Features

| Feature | Description |
|---------|-------------|
| **Common resource model** | Keeps provider resources, ownership, environment, and live state in one consistent format. |
| **Evidence-backed recommendations** | Shows the metrics, observation window, confidence, proposed change, and expected impact behind a recommendation. |
| **Policy and preflight checks** | Checks the environment, actor, live resource state, and action limits before infrastructure is changed. |
| **Governed execution** | Supports dry runs, idempotent actions, persisted intent, and recovery after interruption. |
| **Verification and rollback** | Uses health metrics after a change and restores the captured previous state when policy permits. |
| **Plugin SDK** | Adds discovery, metrics, cost, and action capabilities without moving control of the safety lifecycle into the plugin. |
| **Audit history** | Records the recommendation, decision, execution, verification, and recovery outcome. |

The first v0.3 plugins are `kubernetes-action` and `prometheus-metrics`. Signed plugin installation is implemented, but the public catalog and publisher-key service have not launched yet.

---

## Roadmap

See [`ROADMAP.md`](ROADMAP.md) for current scope, delivery phases, and ways to contribute.

---

## Contributing

Consize is built with the community. Look for `good first issue` on the tracker and open a Discussion before larger architectural changes. Details are in [`CONTRIBUTING.md`](CONTRIBUTING.md).

---

## ⚖️ License

Distributed under the **Apache 2.0 License**. See [`LICENSE`](LICENSE) for more information.
