#!/usr/bin/env bash

set -euo pipefail

PROJECT_ROOT="${PROJECT_ROOT:-/opt/payment-service}"
MODE="${1:-prepare}"
PROD_ENV_FILE="${PROD_ENV_FILE:-.env.production}"
SANDBOX_ENV_FILE="${SANDBOX_ENV_FILE:-.env.sandbox}"
PROD_NGINX_TEMPLATE="${PROD_NGINX_TEMPLATE:-deploy/nginx/nnviopp-production.conf.example}"
SANDBOX_NGINX_TEMPLATE="${SANDBOX_NGINX_TEMPLATE:-deploy/nginx/nnviopp-sandbox.conf.example}"

require_file() {
    local file_path="$1"
    if [[ ! -f "$file_path" ]]; then
        echo "Missing required file: $file_path" >&2
        exit 1
    fi
}

cd "$PROJECT_ROOT"
case "$MODE" in
    prepare)
        require_file "$SANDBOX_ENV_FILE"
        require_file "$SANDBOX_NGINX_TEMPLATE"
        docker compose --env-file "$SANDBOX_ENV_FILE" config --quiet
        echo "Sandbox preparation succeeded. No containers, Nginx, or Production resources were changed."
        ;;
    sandbox)
        require_file "$SANDBOX_ENV_FILE"
        docker compose --env-file "$SANDBOX_ENV_FILE" up -d --build
        echo "Sandbox stack started."
        ;;
    production)
        if [[ "${2:-}" != "--confirm-production" ]]; then
            echo "Production start requires: $0 production --confirm-production" >&2
            exit 2
        fi
        read -r -p "Type START_PRODUCTION to confirm Production start: " confirmation
        if [[ "$confirmation" != "START_PRODUCTION" ]]; then
            echo "Production start cancelled." >&2
            exit 2
        fi
        require_file "$PROD_ENV_FILE"
        require_file "$PROD_NGINX_TEMPLATE"
        docker compose --env-file "$PROD_ENV_FILE" up -d --build
        echo "Production stack started after explicit double confirmation."
        ;;
    *)
        echo "Usage: $0 [prepare|sandbox|production --confirm-production]" >&2
        exit 2
        ;;
esac
