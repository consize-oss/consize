# Consize Local Kubernetes Lab

This lab runs the OSS Consize foundation against a local Kubernetes cluster.

For clean-machine prerequisites, dependency installation, backend and UI setup,
plugin configuration, restart recovery and troubleshooting, begin with the
[v0.3 local development guide](../DEVELOPMENT.md). This file is the shorter lab
command reference.

## Start the Cluster

```bash
kubectl --context docker-desktop get nodes
```

## Install the Demo Workload

```bash
kubectl --context docker-desktop apply -f local-lab/k8s/demo-workload.yaml
kubectl --context docker-desktop rollout status deployment/checkout-api -n consize-demo
```

## Install Prometheus

```bash
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo update
helm upgrade --install consize-prometheus prometheus-community/prometheus \
  --kube-context docker-desktop \
  --namespace monitoring \
  --create-namespace \
  --set alertmanager.enabled=false \
  --set prometheus-pushgateway.enabled=false \
  --set server.persistentVolume.enabled=false
```

## Port-Forward Prometheus

```bash
kubectl --context docker-desktop port-forward -n monitoring svc/consize-prometheus-server 9090:80
```

## Run Consize

```bash
go run ./cmd/consize serve \
  -config examples/local-kind.config.json \
  -resource examples/local-kind-resource.json \
  -demo=false \
  -addr 127.0.0.1:8080
```

Then start the UI:

```bash
cd ui
npm ci
npm run dev -- --hostname 127.0.0.1 --port 3030
```

Read [SAFETY-NOTES.md](SAFETY-NOTES.md) before testing apply. The API automatically
resumes durable verification and recovery work on startup. Preserve
`.consize/local-state.json` across restarts. A separate process cannot open the
same state file while the API is running.

The isolated live apply/rollback test is opt-in and targets Docker Desktop only:

See [Health evidence](../docs/prometheus-health-evidence.md) for the idle CPU,
sparse pod-reason queries and opt-in official PromQL/passive baseline tests.

```bash
CONSIZE_LOCAL_INTEGRATION=1 go test ./pkg/plugins/kubernetes -run TestLiveKubernetesApplyRollback -v -count=1
```
