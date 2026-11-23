# Testing

Consize uses automated tests to help catch regressions before changes are merged.

When making a change, run the tests that cover the part of the project you modified.

## Run the engine test suite

The engine is written in Go. From the repository root:

```sh
cd engine
go test ./...
```

## Run tests for a specific package

You can scope tests to one package when working on a focused change:

```sh
go test ./internal/analysis/...
```

Run a single test by name:

```sh
go test ./internal/analysis/... -run TestPercentileSizing
```

This is useful when iterating on a change and you do not want to run the entire suite every time.

## Lint

```sh
golangci-lint run
```

## UI

The UI (`ui/`) currently ships a linter but no automated test suite yet:

```sh
cd ui
npm run lint
```

If you're adding UI logic that would benefit from tests, that's a good candidate for a first contribution, see [Decisions](decisions.md) for how to think about introducing new tooling.

## Before opening a pull request

Before submitting a pull request:

1. Run `go test ./...` for the areas you touched, then the full suite.
2. Run `golangci-lint run`.
3. Run `npm run lint` in `ui/` if you touched the UI.
4. Check that documentation and configuration examples still work.
5. Review the changes with `git diff`.
6. Make sure unrelated files were not changed.

A typical workflow is:

```
Make change
    ↓
Run focused tests
    ↓
Fix failures
    ↓
Run full test suite + lint
    ↓
Review git diff
    ↓
Open pull request
```

## End-to-end and integration tests

Some behavior cannot be fully tested with unit tests alone.

### Kubernetes discovery qualification

To run the complete isolated-cluster discovery-to-registry integration suite:

```sh
scripts/test/discovery.sh
```

This provisions an isolated Kind cluster, executes the end-to-end qualification
suite, validates clean teardown, and generates a structured summary.
See the [Kubernetes Discovery Qualification Guide](../guides/kubernetes-discovery-qualification.md)
for details and troubleshooting.

For broader tests that interact with an existing Kubernetes environment, see [E2E Tests](e2e.md).

## When tests fail

Start by identifying whether the failure is related to your change.

Check:

* The failing test
* The error message
* Recent changes in the affected code
* Test configuration
* Kubernetes or external service dependencies

Avoid changing tests simply to make a failure disappear. Understand why the test is failing first.

## Next steps

* [E2E Tests](e2e.md)
* [Decisions](decisions.md)
* [How Consize Works](../concepts/architecture.md)