# Consize v0.2 Maintenance Policy

This document defines the preserved source, artifact relationship, maintenance
workflow, and support boundaries for the Consize v0.2 release line.

## Preserved release

The released v0.2.0 source is permanently identified by:

- Git tag: `v0.2.0`
- Git commit: `d323b7faa2e1600e1f0256d08589e81b29a89a64`
- GitHub release: `v0.2.0 - Initial Public Release`
- Helm reference: `oci://ghcr.io/consize-oss/charts/consize:0.2.0`
- Helm manifest digest:
  `sha256:a437b9641391dcb11f444b8b5f4713972478fa877e37f91d6b3a0dd8e50c79d2`
- Sandbox image: `ghcr.io/consize-oss/consize-sandbox:0.2.0`

The `v0.2.0` tag is immutable. It must never be moved, deleted, or recreated to
include later fixes. A correction to released behavior must receive a new patch
version such as `v0.2.1`.

## Artifact relationship

The v0.2.0 GitHub release and Git tag both resolve to the preserved commit
above. The published GHCR Helm chart contains version `0.2.0` and renders
successfully with the documented team-scoped and production values.

The GHCR chart archive is not byte-identical to the `consize-0.2.0.tgz` file
stored in the tagged commit. The stored archive contains the former personal
repository URLs in `Chart.yaml`; the published archive contains the equivalent
`consize-oss` organization URLs. The chart templates and runtime configuration
are otherwise unchanged. This packaging discrepancy is retained as release
history and must not be repaired by moving the tag.

## Maintenance branch

`release/0.2` is the only supported integration branch for v0.2 maintenance.
It begins at the preserved v0.2.0 source commit. It must not receive v0.3
foundation code, architectural migrations, plugin SDK work, or unrelated
features.

Allowed changes are limited to:

- critical security fixes;
- high-severity correctness and data-integrity fixes;
- release-blocking compatibility fixes;
- narrowly scoped documentation corrections;
- build or packaging repairs required to publish a supported patch release.

Every change requires a pull request. Direct pushes, force pushes, branch
deletion, and history rewriting are prohibited. Required CI checks and at least
one maintainer review must pass before merge.

## Hotfix workflow

1. Confirm that the issue affects a supported v0.2 release and qualifies for
   maintenance.
2. Update the local `release/0.2` branch from the protected remote branch.
3. Create a branch named `hotfix/0.2-<short-description>` from `release/0.2`.
4. Make the smallest viable correction and add regression coverage.
5. Open a pull request back to `release/0.2`; never target the v0.2.0 tag.
6. Run the complete v0.2 release checks and obtain maintainer approval.
7. Merge without rewriting release history.
8. Publish a new semantic patch version such as `v0.2.1` and create immutable
   chart and image references with the same version.
9. Open a separate pull request that ports every still-relevant fix to `main`.
   If a port is not applicable, document why in the hotfix pull request.

## Release checks

A v0.2 patch release must run, from a clean checkout:

```bash
cd engine
go test ./...
go vet ./...

cd ../ui
npm ci
npm run lint
npm run build

cd ..
helm lint charts/consize
helm template consize charts/consize \
  -f charts/consize/examples/values-team-scoped.yaml
helm template consize charts/consize \
  -f charts/consize/examples/values-prod.yaml
docker compose -f docker-compose.demo.yaml config --quiet
```

The 4 November 2026 CON-56 check reproduced the original tag's lint error in
`CostOpportunitiesView.tsx`. Its backend tests and frontend production build
passed in that run. An earlier qualification also recorded a Next.js route
context type-check failure; it did not reproduce in the current locked-dependency
build. These results do not authorize editing the immutable tag or weakening lint.

`release/0.2` includes the corrective frontend hotfix from PR #21 and passes
backend tests/vet and frontend lint/build in the current check. Record new patch
release qualification separately. See
[consolidation qualification](engineering/consolidation-qualification.md) for the
exact commits, commands and outstanding live-workflow checks.

## Immutable deployment references

Published v0.2 instructions and deployment manifests must use a semantic
version such as `0.2.0` or a content digest. They must not deploy from `main`, a
floating branch, or the `latest` tag. Production operators should prefer image
and chart digests where their deployment system supports them.

## Support and end of support

The v0.2 line is in maintenance mode. It receives qualifying security and
critical correctness fixes, but no new product features. The team will announce
an end-of-support date at least 90 days in advance and publish the notice in the
repository, release notes, and supported documentation.

After end of support:

- existing immutable releases and source tags remain available;
- no new v0.2 patches are promised;
- unresolved vulnerabilities and compatibility limits are documented;
- users are directed to the supported v0.3 or later migration path;
- exceptional fixes require an explicit repository-owner decision.

The repository owner is accountable for approving patch releases, support
exceptions, and the final end-of-support declaration.
