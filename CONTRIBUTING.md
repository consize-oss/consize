# Contributing to Consize

New development targets the v0.3 alpha on `main`. Use a short-lived branch and a
pull request targeting `main`. Keep unrelated changes in separate PRs.

The preserved v0.2 release uses `release/0.2` and `hotfix/0.2-*` branches. Follow
[the maintenance policy](docs/maintenance-0.2.md) for that line. Do not move the
`v0.2.0` tag or merge v0.3 architecture changes into the maintenance branch.

## Set up a clean checkout

Install the versions in `.go-version` and `.node-version`, and the npm version in
`ui/package.json` (`packageManager`). Then run:

```sh
go mod download
npm --prefix tools/ci ci
npm --prefix ui ci
```

Read [core model contracts](docs/reference/core-model.md) before changing resource
identity, evidence, recommendations, actions, or storage. Read
[DEVELOPMENT.md](DEVELOPMENT.md) for the real local Kubernetes/Prometheus lab and
[runtime data and demo isolation](docs/engineering/runtime-data.md) for startup
behavior.

## Run the backend and frontend

In separate terminals:

```sh
go run ./cmd/consize serve -addr 127.0.0.1:8080
```

```sh
cd ui
npm run dev -- --hostname 127.0.0.1 --port 3030
```

The default API starts empty, without plugins, and keeps state only in memory.
Use an explicit configuration for persistence or integrations. Some UI panels
still require `/api/v1` endpoints that are not implemented; an unavailable panel
is an integration gap, not evidence that demo fixtures should be enabled.

The old `engine/` directory and `docker-compose.demo.yaml` are not part of the
v0.3 checkout. Use them only on the preserved v0.2 line where they exist.

## Validate a change

Run the checks relevant to the change and the required CI gates:

```sh
scripts/ci/backend.sh
npm --prefix ui run lint
npm --prefix ui run typecheck
npm --prefix ui test
npm --prefix ui run build
scripts/api/check-generated.sh
```

Backend CI also exercises PostgreSQL in a disposable container. Set `CI=1` when
running `go test ./internal/store` locally to require that harness; Docker must
be available. Ordinary local tests may skip optional integration tests, so their
success alone does not qualify a release.

For the existing production-browser smoke, follow `.github/workflows/ci.yml`:
build `bin/consize-smoke`, build the UI with
`NEXT_PUBLIC_CONSIZE_API_BASE_URL=http://127.0.0.1:18080` and
`NEXT_PUBLIC_USE_MOCKS=false`, then run `npm --prefix ui run test:e2e` with the same
variables. The smoke uses ports 18080 and 3031. Its current scope includes visible
API contract failures; it is not proof of a complete optimization workflow.

For consolidation qualification, use
[the qualification procedure](docs/engineering/consolidation-qualification.md).

## Open a pull request

Explain the resulting behavior, link the Linear ticket, and include relevant test
results. Update OpenAPI and generated declarations together when changing the API.
Include screenshots for UI changes and migration/recovery instructions for state
changes. Obtain review for affected backend/integration and frontend surfaces.

Merge through the protected PR process after required checks and reviews pass.
Delete the short-lived branch after merge. Never bypass reviews to qualify a
baseline, rewrite release tags, or treat a skipped/unavailable check as passing.
