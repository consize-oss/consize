#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
version="$(sed -n 's/^const GovulncheckVersion = "\([^"]*\)"$/\1/p' internal/dependencysecurity/report.go)"
[[ "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo 'Invalid scanner version pin'; exit 1; }
destination="${1:?Provide an absolute installation directory}"
[[ "$destination" = /* ]] || { echo 'Use an absolute installation directory'; exit 1; }
mkdir -p "$destination"
# The Go module proxy and checksum database verify this exact tagged tool and
# its module dependencies. Do not disable GOSUMDB or install @latest.
GOBIN="$destination" GOPRIVATE= GONOSUMDB= GONOPROXY= GOINSECURE= GOPROXY=https://proxy.golang.org GOSUMDB=sum.golang.org GOTOOLCHAIN=local \
  go install "golang.org/x/vuln/cmd/govulncheck@${version}"
go version -m "$destination/govulncheck"
