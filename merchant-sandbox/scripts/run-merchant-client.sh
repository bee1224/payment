#!/usr/bin/env bash
set -euo pipefail

: "${GOCACHE:=$HOME/.cache/go-build}"
: "${GOPATH:=$HOME/go}"
export GOCACHE GOPATH

if [[ -f .env ]]; then
  set -a
  # shellcheck disable=SC1091
  source .env
  set +a
fi

exec go run ./cmd/merchant-client "$@"
