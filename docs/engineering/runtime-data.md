# Runtime data and demo isolation

Normal startup does not create resources, recommendations, actions, jobs, evidence,
or audit history. Existing records in a configured state store remain visible.

## Start an empty instance

```sh
go run ./cmd/consize serve -addr 127.0.0.1:8080
```

Without a configuration file, this uses memory only. Restarting loses that state.
Startup prints this limitation. `/api/health` reports `degraded` with setup
diagnostics when no plugins are registered; HTTP 200 means the process/store is
reachable, not that evidence or automation is ready.

For persistence, create a configuration file containing a `state_path`, or use
`database_url` (prefer the `CONSIZE_DATABASE_URL` environment variable). Start with
`-config path/to/config.json`. An empty `{}` configuration is valid. Unknown keys,
multiple JSON documents, and `null` configurations are rejected so typos cannot
silently disable capabilities.

Register resources through `/api/resources`, or explicitly configure a discovery
plugin and call `/api/discovery`. With no discovery plugin, discovery returns an
error instead of sample resources. See [the local integration guide](https://github.com/consize-oss/consize/blob/main/DEVELOPMENT.md)
for an explicitly configured Kubernetes/Prometheus example.

## Bind capabilities explicitly

All integrations are disabled unless configured. When enabled:

- `recommender.metrics_plugin_id` must name a registered `metrics.read` capability.
- `recommender.action_plugin_id` must name a registered `action.plan` capability.
- `verification.metrics_plugin_id` must name a registered `metrics.read` capability.

Invalid enabled bindings fail startup. Missing discovery and disabled workflows
are printed as setup diagnostics. Disabled recommendation generation returns an
explicit API error. CLI metrics reads require `-plugin`; recommendation and
end-to-end commands use explicit configuration bindings. No core path substitutes
Prometheus or Kubernetes plugin IDs.

The bundled recommendation algorithm is specifically a Kubernetes memory
headroom algorithm. Its memory keys, patch action type, headroom and reduction
limits are algorithm defaults, not universal provider support. The built-in policy
engine remains installed with conservative approval rules; its identifier and
summary describe that implemented policy, not seeded evaluation outcomes.

## Frontend

The UI calls the configured backend (`NEXT_PUBLIC_CONSIZE_API_BASE_URL`, default
`http://127.0.0.1:8080`). Missing API endpoints, network failures and invalid
responses stay errors. They do not trigger fixture fallback. Empty resource
responses produce an empty inventory; absent telemetry, policy or action
assessments remain unknown. The shell does not claim a workspace, user identity or
admin role until those services provide them. Inventory assessments, when supplied, use
`metadata.inventory`; runtime code does not read `metadata.demo_inventory`.

Some dashboard, recommendation and plugin panels still require `/api/v1`
contracts that are not implemented by the current backend. Those panels report
unavailable until their integration tickets are delivered. Demo mode is not a
substitute for that integration.

## Opt-in design data

For browser design review only, start the development UI explicitly:

```sh
cd ui
NEXT_PUBLIC_USE_MOCKS=true npm run dev -- --port 3030
```

The scenario control is labelled **Demo data**. It and its fixture imports are
loaded only when both development mode and the explicit flag are set. Production
ignores this flag. Restart the dev server without it to use the real API; close
other demo tabs to avoid confusing sessions. Fixtures live under `ui/src/mocks`.

The backend's separate `-demo` flag loads `internal/demo` fixtures into the selected
store. Use a disposable store: disabling the flag does not delete previously
persisted fixtures. `-resource` and `-recommendation` are explicit JSON imports,
not defaults. Never use design fixtures as operational evidence or approve their
actions against real infrastructure.

## Validation

CI runs `TestCleanStart` explicitly, as well as the full backend suite. It verifies
empty in-memory startup, empty durable startup and restart, zero dashboard totals,
missing-capability diagnostics and discovery refusal. Core import tests prevent
runtime packages from importing fixture packages. Frontend tests cover empty API
responses, unavailable backends and static fixture isolation.

The runtime scan covered `cmd`, `internal`, `pkg`, UI entry points, API clients,
configuration and startup. Intentional data/defaults remain in examples, tests,
opt-in demo modules, the implemented policy engine, and the named Kubernetes
algorithm. No normal startup imports records from those examples or fixtures.
