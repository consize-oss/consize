#!/usr/bin/env bash
set -euo pipefail
cd "${1:-$(dirname "$0")/../..}"
unformatted="$(git ls-files -z '*.go' | xargs -0 gofmt -l)"
if [[ -n "$unformatted" ]]; then
  printf 'Formatting failure (run gofmt locally):\n%s\n' "$unformatted"
  exit 1
fi
