#!/usr/bin/env bash
# Test-only official binary; never a deployable Consize artifact.
set -euo pipefail
version=3.16.0
case "$(uname -s)-$(uname -m)" in
  Linux-x86_64) platform=linux-amd64; checksum=68727009472a2c02e1ff7f062fd5b3e637285bffc8fc2d340c76fb449d6e0f34 ;;
  Darwin-arm64) platform=darwin-arm64; checksum=f7c71b553316f822bdd860a3ea527ebefb2f8e944e5b7dfd419ae2653cf3ba01 ;;
  *) echo 'Unsupported Prometheus integration-test platform' >&2; exit 1 ;;
esac
[[ $# == 1 ]] || { echo 'Pass a dedicated test-tool directory' >&2; exit 1; }
mkdir -p "$1"
target="$(cd "$1" && pwd)"
archive="$(mktemp)"
trap 'rm -f "$archive"' EXIT
asset="prometheus-${version}.${platform}"
curl --fail --silent --show-error --location --proto '=https' --proto-redir '=https' --tlsv1.2 --connect-timeout 15 --max-time 180 --retry 2 "https://github.com/prometheus/prometheus/releases/download/v${version}/${asset}.tar.gz" -o "$archive"
python3 - "$archive" "$checksum" <<'CHECKSUM'
import hashlib
import sys
hasher = hashlib.sha256()
with open(sys.argv[1], "rb") as archive:
    for chunk in iter(lambda: archive.read(1024 * 1024), b""):
        hasher.update(chunk)
if hasher.hexdigest() != sys.argv[2]:
    raise SystemExit("Prometheus test binary checksum verification failed")
CHECKSUM

tar -xzf "$archive" --strip-components=1 -C "$target" "${asset}/prometheus"
printf '%s/prometheus\n' "$target"
