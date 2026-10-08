# Consize v0.3 Local Development

This guide sets up the current v0.3 backend, Next.js UI, local Kubernetes
workload and Prometheus evidence source. It is the recommended starting point
for Consize team members working from a clean checkout.

The v0.2 release uses a different application structure. Use this guide only
for `main` and branches based on the v0.3 development line.

## 1. Prerequisites

Install and verify:

- Git;
- Go `1.26.x`, matching [`go.mod`](go.mod);
- Node.js `24.x` and npm;
- Docker Desktop with Kubernetes enabled;
- `kubectl`;
- Helm 3.

```bash
git --version
go version
node --version
npm --version
docker version
kubectl version --client
helm version
```

Wait until Docker Desktop reports that Kubernetes is running before continuing.

## 2. Clone and Install Dependencies

```bash
git clone https://github.com/consize-oss/consize.git
cd consize
git switch main

go mod download

cd ui
npm ci
cd ..
```

Use `npm ci`, not `npm install`, so local dependencies match
[`ui/package-lock.json`](ui/package-lock.json).

## 3. Validate the Checkout

Run the backend checks:

```bash
go test ./...
go test -race ./...
go vet ./...
```

Build and type-check the frontend:

```bash
cd ui
npm run build
cd ..
```

These commands must pass without files copied from another checkout.

## 4. Prepare Local Configuration

Create a private local copy of the checked-in lab configuration:

```bash
mkdir -p .consize
cp examples/local-kind.config.json .consize/local-dev.config.json
```

Open `.consize/local-dev.config.json` and set `kubernetes.kubeconfig` to the
absolute path of your kubeconfig. On macOS this is normally:

```text
/Users/YOUR_USERNAME/.kube/config
```

Do not commit the local configuration, state files, audit logs, credentials or
downloaded plugins. The `.consize` directory is ignored by Git.

The lab configuration leaves API authentication disabled. This is intentional:

- the API listens only on `127.0.0.1` when authentication is disabled;
- the current development UI does not attach bearer tokens;
- authenticated role testing should be performed directly against the API with
  environment-backed credentials.

Never bind an unauthenticated API to `0.0.0.0` or another network interface.

## 5. Start Local Kubernetes

Select Docker Desktop and confirm the node is ready:

```bash
kubectl config use-context docker-desktop
kubectl --context docker-desktop get nodes
```

Install the test workload:

```bash
kubectl apply -f local-lab/k8s/demo-workload.yaml
kubectl rollout status deployment/checkout-api -n consize-demo
kubectl get pods -n consize-demo
```

Consize will only modify this test Deployment when an action is explicitly
approved.

## 6. Install and Expose Prometheus

Install Prometheus once:

```bash
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo update
helm upgrade --install consize-prometheus prometheus-community/prometheus \
  --namespace monitoring \
  --create-namespace \
  --set alertmanager.enabled=false \
  --set prometheus-pushgateway.enabled=false \
  --set server.persistentVolume.enabled=false
```

Wait for the server:

```bash
kubectl rollout status deployment/consize-prometheus-server -n monitoring
```

Keep this port-forward running in **Terminal 1**:

```bash
kubectl port-forward -n monitoring svc/consize-prometheus-server 9090:80
```

Verify Prometheus:

```bash
curl -fsS http://127.0.0.1:9090/-/ready
```

## 7. Start the Backend

In **Terminal 2**, from the repository root:

```bash
go run ./cmd/consize serve \
  -config .consize/local-dev.config.json \
  -demo=false \
  -addr 127.0.0.1:8080
```

The API process also runs the durable action and verification worker. Do not
start a second worker against the same state file.

Verify backend health:

```bash
curl -fsS http://127.0.0.1:8080/api/health
```

Discover the local Deployment through the Kubernetes plugin:

```bash
curl -fsS -X POST http://127.0.0.1:8080/api/discovery
curl -fsS http://127.0.0.1:8080/api/resources
```

The durable registry and action state are stored under `.consize`. Keep that
directory when testing restart recovery.

This file backend is for the single-process local lab. Production API and
worker deployments share PostgreSQL through `CONSIZE_DATABASE_URL`; see
[`docs/engineering/durable-storage.md`](docs/engineering/durable-storage.md).

## 8. Start the UI

In **Terminal 3**:

```bash
cd ui
npm run dev -- --hostname 127.0.0.1 --port 3030
```

Open [http://127.0.0.1:3030](http://127.0.0.1:3030).

The UI uses `http://127.0.0.1:8080` by default. To use another API address:

```bash
NEXT_PUBLIC_CONSIZE_API_BASE_URL=http://127.0.0.1:8081 npm run dev -- --hostname 127.0.0.1 --port 3030
```

If the dashboard reports that the API is unavailable, check Terminal 2 and the
health endpoint before changing frontend code.

## 9. Exercise the v0.3 Workflow

Use only the `consize-demo/checkout-api` workload for local action testing.

1. Confirm Kubernetes and Prometheus plugins are healthy.
2. Run discovery and confirm `checkout-api` appears as a registered resource.
3. Allow Prometheus to collect enough samples for the configured evidence
   window.
4. Generate a recommendation.
5. Review the evidence, confidence, proposed request and retained headroom.
6. Create a dry-run plan. Planning must not change Kubernetes.
7. Approve the action only after reading
   [`local-lab/SAFETY-NOTES.md`](local-lab/SAFETY-NOTES.md).
8. Observe rollout readiness and the post-action verification window.
9. Confirm the final action, verification or rollback result in the audit data.

The local configuration uses conservative verification timing. A recommendation
may remain in verification for several minutes; this is expected.

Inspect backend state directly when troubleshooting:

```bash
curl -fsS http://127.0.0.1:8080/api/dashboard
curl -fsS http://127.0.0.1:8080/api/jobs
curl -fsS http://127.0.0.1:8080/api/actions
```

## 10. Test Restart Recovery

To verify durable recovery:

1. Start an approved action or leave a job waiting for verification.
2. Stop the API with `Ctrl+C`.
3. Do not delete `.consize/local-state.json`.
4. Start the API again with the same command and configuration.
5. Confirm `/api/jobs` resumes the existing job rather than creating another
   action.

The state store allows one owning process. An error saying the state is already
locked usually means another API or worker is still running.

Inspect the schema and migration status without starting the API:

```bash
go run ./cmd/consize storage status -state .consize/local-state.json
```

For backup, restore, upgrade, and reset procedures, follow
[`docs/engineering/durable-storage.md`](docs/engineering/durable-storage.md).
Do not delete the state file to resolve an upgrade error.

## 11. Plugin Development and Installation

Built-in plugin contracts are under [`pkg/plugin`](pkg/plugin), and first-party
plugins are under [`pkg/plugins`](pkg/plugins). List configured plugins with:

```bash
go run ./cmd/consize plugins -config .consize/local-dev.config.json
go run ./cmd/consize health -config .consize/local-dev.config.json
```

The `pluginctl` command installs explicitly trusted, signed plugin releases from
an HTTPS catalog. Installation does not automatically enable a plugin:

```bash
go run ./cmd/pluginctl catalog \
  -catalog https://example.invalid/catalog.json \
  -public-key /path/to/trusted-publisher.pub

go run ./cmd/pluginctl install \
  -catalog https://example.invalid/catalog.json \
  -public-key /path/to/trusted-publisher.pub \
  -id PLUGIN_ID \
  -version EXACT_VERSION
```

Replace the example catalog only with a team-approved catalog and publisher
key. Never install an arbitrary community executable into a shared environment.

## 12. Stop or Reset the Lab

Stop the UI, API and Prometheus port-forward with `Ctrl+C` in their terminals.

Remove Kubernetes lab resources:

```bash
kubectl delete -f local-lab/k8s/demo-workload.yaml
helm uninstall consize-prometheus -n monitoring
kubectl delete namespace monitoring
```

To start with empty Consize state, first stop the API, then use the guarded
reset command. The previous file is archived rather than deleted:

```bash
go run ./cmd/consize storage reset \
  -state .consize/local-state.json \
  -confirm "ERASE LOCAL STATE"
```

## Troubleshooting

### Kubernetes remains unavailable

```bash
kubectl config current-context
kubectl get nodes
kubectl get pods -A
```

Confirm the kubeconfig path in `.consize/local-dev.config.json` is absolute and
readable.

### Prometheus metrics are missing

Confirm Terminal 1 is still forwarding port `9090`, then check:

```bash
curl -fsS http://127.0.0.1:9090/-/ready
kubectl get pods -n monitoring
```

New workloads need time to accumulate samples. Missing, stale or incomplete
evidence must not be treated as a successful safety check.

### The UI shows API unavailable

```bash
curl -i http://127.0.0.1:8080/api/health
```

Use the same host form for the UI and configured CORS origin. The default local
combination is UI `127.0.0.1:3030` and API `127.0.0.1:8080`.

### Port 3030 shows the old v0.2 UI

Another worktree may already be serving the old frontend. Identify the process
and its working directory:

```bash
lsof -nP -iTCP:3030 -sTCP:LISTEN
lsof -a -p PROCESS_ID -d cwd -Fn
```

Stop that development server, return to the v0.3 repository, and confirm the UI
package before restarting it:

```bash
cd ui
npm pkg get name version
npm run dev -- --hostname 127.0.0.1 --port 3030
```

The v0.3 package reports `consize-oss-ui` and version `0.3.0-alpha`. Do not run
the v0.3 frontend from a `release/0.2` checkout or the older local `consize/ui`
directory.

### An action button is unavailable

Inspect the recommendation confidence, policy result, plugin health, evidence
coverage and active jobs. Consize blocks actions when required safety evidence
is missing or another unresolved job already owns the resource.

## Team Workflow

Create short-lived branches from `main` and open pull requests back to `main`:

```bash
git switch main
git pull --ff-only
git switch -c feat/short-description
```

Do not develop v0.3 features on `release/0.2`. That branch is reserved for
v0.2.x maintenance and emergency fixes.

Before requesting review, run the backend tests, race detector, vet and frontend
production build listed above. Additional live-integration guidance is in
[`local-lab/README.md`](local-lab/README.md).
