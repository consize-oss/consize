#!/usr/bin/env bash
# Creates only a new, explicitly named disposable Kind cluster. Never uses the
# caller's kubectl context; the CI reuse path accepts only its exact run identity.
set -euo pipefail
repo="$(cd "$(dirname "$0")/../.." && pwd)"
reuse_ci=false
output=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --reuse-ci) reuse_ci=true; shift ;;
    --output) [[ $# -ge 2 ]] || { echo 'Missing --output directory' >&2; exit 2; }; output="$2"; shift 2 ;;
    --help) echo 'Usage: scripts/test/evidence.sh [--output NEW_DIRECTORY] [--reuse-ci]'; exit 0 ;;
    *) echo 'Unknown qualification option' >&2; exit 2 ;;
  esac
done
command -v go >/dev/null || { echo 'Go is required' >&2; exit 1; }
umask 077
if [[ -z "$output" ]]; then
  output="$(mktemp -d "${TMPDIR:-/tmp}/consize-evidence-qualification.XXXXXX")"
else
  [[ ! -e "$output" ]] || { echo 'Use a new output directory' >&2; exit 2; }
  mkdir -p "$output"
fi
output="$(cd "$output" && pwd)"
cluster_name=""
owned_cluster=false
cleanup() {
  result=$?
  trap - EXIT INT TERM
  if [[ "$owned_cluster" == true ]]; then
    if ! kind delete cluster --name "$cluster_name" --kubeconfig "$output/kubeconfig" >> "$output/cluster.log" 2>&1; then
      echo 'Disposable cluster cleanup failed; inspect cluster.log and cluster-name.txt' >&2
      [[ "$result" != 0 ]] || result=1
    else
      remaining="$(kind get clusters 2>> "$output/cluster.log")" || { [[ "$result" != 0 ]] || result=1; remaining="$cluster_name"; }
      while IFS= read -r name; do
        if [[ "$name" == "$cluster_name" ]]; then echo 'Disposable cluster still exists after cleanup' >&2; [[ "$result" != 0 ]] || result=1; fi
      done <<< "$remaining"
    fi
    rm -f "$output/kubeconfig"
  fi
  echo "Qualification output: $output"
  exit "$result"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
if [[ "$reuse_ci" == true ]]; then
  [[ "${GITHUB_ACTIONS:-}" == true && "${CI:-}" == true && "${GITHUB_RUN_ID:-}" =~ ^[0-9]+$ && "${GITHUB_RUN_ATTEMPT:-}" =~ ^[0-9]+$ ]] || { echo 'CI reuse requires a GitHub run identity' >&2; exit 2; }
  expected="kind-consize-discovery-${GITHUB_RUN_ID}-${GITHUB_RUN_ATTEMPT}"
  [[ "${CONSIZE_DISCOVERY_TEST_CONTEXT:-}" == "$expected" && "${CONSIZE_DISCOVERY_TEST_KUBECONFIG:-}" == /* && "${CONSIZE_DISCOVERY_TEST_FIXTURES:-}" == 1 ]] || { echo 'CI cluster scope does not match this run' >&2; exit 2; }
else
  command -v kind >/dev/null || { echo 'Install the pinned Kind version documented in scripts/ci/discovery-cluster.sh' >&2; exit 1; }
  command -v docker >/dev/null || { echo 'A local Docker engine is required' >&2; exit 1; }
  docker_host="${DOCKER_HOST:-}"
  if [[ -z "$docker_host" ]]; then docker_host="$(docker context inspect --format '{{.Endpoints.docker.Host}}')"; fi
  [[ "$docker_host" == unix://* ]] || { echo 'Refusing a non-local Docker endpoint' >&2; exit 2; }
  docker info >/dev/null 2>&1 || { echo 'Start the local Docker engine before qualification' >&2; exit 1; }
  expected_version="$(sed -n 's/^version=//p' "$repo/scripts/ci/discovery-cluster.sh")"
  image="$(sed -n "s/^image='\(.*\)'/\1/p" "$repo/scripts/ci/discovery-cluster.sh")"
  actual_version="$(kind version)"
  [[ "$actual_version" == "kind v${expected_version}" || "$actual_version" == "kind v${expected_version} "* ]] || { echo "Kind v${expected_version} is required" >&2; exit 1; }
  [[ "$image" == kindest/node:*@sha256:* ]] || { echo 'Missing pinned test node image' >&2; exit 1; }
  suffix="$(od -An -N6 -tx1 /dev/urandom | tr -d ' \n')"
  cluster_name="consize-discovery-local-$(date +%s)-${suffix}"
  printf '%s\n' "$cluster_name" > "$output/cluster-name.txt"
  export KIND_EXPERIMENTAL_PROVIDER=docker
  owned_cluster=true
  kind create cluster --config "$repo/scripts/test/evidence-kind.yaml" --name "$cluster_name" --image "$image" --kubeconfig "$output/kubeconfig" --wait 120s > "$output/cluster.log" 2>&1
  chmod 0600 "$output/kubeconfig"
  export CONSIZE_DISCOVERY_TEST_KUBECONFIG="$output/kubeconfig"
  export CONSIZE_DISCOVERY_TEST_CONTEXT="kind-$cluster_name"
  export CONSIZE_DISCOVERY_TEST_FIXTURES=1
fi
if [[ -z "${CONSIZE_PROMETHEUS_BIN:-}" ]]; then
  CONSIZE_PROMETHEUS_BIN="$("$repo/scripts/ci/install-prometheus-test.sh" "$output/prometheus-tools")"
  export CONSIZE_PROMETHEUS_BIN
fi
[[ "$CONSIZE_PROMETHEUS_BIN" == /* && -x "$CONSIZE_PROMETHEUS_BIN" ]] || { echo 'Use an absolute pinned Prometheus executable path' >&2; exit 2; }
export CONSIZE_EVIDENCE_TEST_FIXTURES=1
export CONSIZE_EVIDENCE_QUALIFICATION_REPORT="$output/summary.json"
cd "$repo"
go test -mod=readonly -count=1 -v ./internal/collection -run '^TestIsolatedEvidencePipelineQualification$' 2>&1 | tee "$output/test.log"
# The Go test writes the report only after checking fixture namespace cleanup.
[[ -s "$output/summary.json" ]] || { echo 'Qualification produced no completion summary' >&2; exit 1; }
