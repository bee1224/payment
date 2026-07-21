#!/usr/bin/env bash
set -euo pipefail

env_file="${1:-.env.production}"
[[ -f "$env_file" ]] || { echo "Production env file not found: $env_file" >&2; exit 1; }
echo 'Production deploy is intentionally not executed by this script.'
echo 'Impact: production API and database migration may affect live payments.'
echo "Plan: docker compose --env-file $env_file config; backup; controlled API start; health check; validate callbacks."
echo 'Rollback: restore the approved production image and use a pre-approved database recovery plan; do not run destructive migration down automatically.'
