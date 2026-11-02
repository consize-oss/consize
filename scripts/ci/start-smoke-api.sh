#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
state="$(mktemp -d)"
pid=""
cleanup() {
  if [[ -n "$pid" ]]; then
    kill "$pid" 2>/dev/null || :
    wait "$pid" 2>/dev/null || :
  fi
  rm -rf "$state"
}
trap cleanup EXIT
trap 'exit 143' TERM
trap 'exit 130' INT
printf '{"state_path":"%s/state.json","allowed_origins":["http://127.0.0.1:3031"]}\n' "$state" > "$state/config.json"
"${CONSIZE_SMOKE_BIN:-./bin/consize-smoke}" serve -config "$state/config.json" -addr 127.0.0.1:18080 -demo=false &
pid=$!
wait "$pid"
