#!/usr/bin/env bash
# An isolated test control plane, not a Consize deployment/release image.
set -euo pipefail
[[ $# == 2 ]] || { echo 'Usage: discovery-cluster.sh create|delete TOOL_DIRECTORY' >&2; exit 1; }
operation="$1"
target="$2"
version=0.33.0
checksum=aee6151561422756b764a4ae28e7f44cda5af5a9eead3cc9985112b1de8d8e0d
image='kindest/node:v1.37.0@sha256:a1ed56cfb0e7b93589bdf97c8cd566405a265939e3620fc4f5de89adff580ae5'
[[ "$(uname -s)-$(uname -m)" == Linux-x86_64 ]] || { echo 'CI discovery cluster requires Linux amd64' >&2; exit 1; }
[[ -n "${GITHUB_RUN_ID:-}" && -n "${GITHUB_RUN_ATTEMPT:-}" ]] || { echo 'Refusing to operate without an isolated CI run identity' >&2; exit 1; }
cluster="consize-discovery-${GITHUB_RUN_ID}-${GITHUB_RUN_ATTEMPT}"
[[ "$cluster" =~ ^consize-discovery-[0-9]+-[0-9]+$ ]] || exit 1
export KIND_EXPERIMENTAL_PROVIDER=docker
case "$operation" in
  create)
    umask 077
    mkdir -p "$target"
    archive="$(mktemp)"
    trap 'rm -f "$archive"' EXIT
    curl --fail --silent --show-error --location --proto '=https' --proto-redir '=https' --tlsv1.2 --connect-timeout 15 --max-time 120 --retry 2 "https://github.com/kubernetes-sigs/kind/releases/download/v${version}/kind-linux-amd64" -o "$archive"
    python3 - "$archive" "$checksum" <<'CHECKSUM'
import hashlib
import sys
hasher = hashlib.sha256()
with open(sys.argv[1], 'rb') as artifact:
    for chunk in iter(lambda: artifact.read(1024 * 1024), b''):
        hasher.update(chunk)
if hasher.hexdigest() != sys.argv[2]:
    raise SystemExit('kind test tool checksum verification failed')
CHECKSUM
    mv "$archive" "$target/kind"
    chmod 0700 "$target/kind"
    "$target/kind" create cluster --config "$(dirname "$0")/../test/evidence-kind.yaml" --name "$cluster" --image "$image" --kubeconfig "$target/kubeconfig" --wait 120s
    chmod 0600 "$target/kubeconfig"
    printf 'CONSIZE_DISCOVERY_TEST_KUBECONFIG=%s\n' "$target/kubeconfig" >> "$GITHUB_ENV"
    printf 'CONSIZE_DISCOVERY_TEST_CONTEXT=kind-%s\n' "$cluster" >> "$GITHUB_ENV"
    printf 'CONSIZE_DISCOVERY_TEST_FIXTURES=1\n' >> "$GITHUB_ENV"
    ;;
  delete)
    if [[ -x "$target/kind" ]]; then
      "$target/kind" delete cluster --name "$cluster" --kubeconfig "$target/kubeconfig"
    fi
    ;;
  *) echo 'Unknown test cluster operation' >&2; exit 1 ;;
esac
