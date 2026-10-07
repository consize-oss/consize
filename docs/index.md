---
title: "Consize Documentation"
hide:
  - toc
---

<div class="consize-hero consize-hero--split" markdown>

<div class="consize-hero__text" markdown>

# <span class="consize-gradient">Optimize safely</span> with Consize

Learn how to use consize to analyze, identify and apply safer changes with built-in guardrails without turning cost optimization into a production risk.

<div class="consize-hero__actions" markdown>
[Get started](getting-started/get-started.md){ .md-button .md-button--primary }
[Try the Interactive Sandbox](getting-started/sandbox.md){ .md-button }
</div>

</div>

<div class="consize-hero__cmd" markdown>
```bash
docker run -p 3000:3000 -p 8080:8080 -it ghcr.io/consize-oss/consize-sandbox:latest
```
</div>

<div class="consize-shot" markdown>
![The Consize dashboard showing a rightsizing recommendation and its safety verification status](assets/demo-dashboard.png){ .consize-hero-shot }
</div>

</div>

<p class="consize-note">Stuck at any point? <a href="resources/troubleshooting/">Troubleshooting</a>, the <a href="resources/faq/">FAQ</a>, and <a href="resources/support/">Support</a> are one click away from every page in the footer.</p>

Consize continuously evaluates your infrastructure and turns resource usage data into actionable recommendations. You decide how changes are applied through reviewable pull requests against your existing Infrastructure-as-Code workflow, or as guarded runtime changes within configured boundaries.

## How it works

Go from zero to a verified rightsizing change against a real workload.

<div class="consize-steps" markdown>

<div class="consize-step" markdown>

<div class="consize-step__icon" markdown>:material-magnify:</div>

<div class="consize-step__title" markdown>1\. Analyze</div>

Point Consize at a cluster. It profiles real CPU and memory usage over a rolling window: no guesswork, no assumed sizing.

<div class="consize-step__links" markdown>

- [Get started](getting-started/get-started.md)
- [Interactive Sandbox](getting-started/sandbox.md)
- [Production Installation](getting-started/installation.md)

</div>

</div>

<div class="consize-step" markdown>

<div class="consize-step__icon" markdown>:material-source-pull:</div>

<div class="consize-step__title" markdown>2\. Review</div>

Get a rightsizing pull request against your IaC repo, or apply approved changes directly through the UI within configured boundaries.

<div class="consize-step__links" markdown>

- [Kubernetes Rightsizing](guides/rightsizing.md)
- [Environments](guides/environments.md)
- [Configuration](reference/configuration.md)

</div>

</div>

<div class="consize-step" markdown>

<div class="consize-step__icon" markdown>:material-shield-check:</div>

<div class="consize-step__title" markdown>3\. Verify</div>

SLIs are watched after every change. If something regresses, Consize triggers an automatic, byte-identical rollback.

<div class="consize-step__links" markdown>

- [The Safety Net](concepts/safety-net.md)
- [Observability](guides/observability.md)
- [How Consize Works](concepts/architecture.md)

</div>

</div>

</div>