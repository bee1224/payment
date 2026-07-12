#!/usr/bin/env bash

set -euo pipefail

PROJECT_ROOT="${PROJECT_ROOT:-/opt/payment-service}"
PROD_ENV_FILE="${PROD_ENV_FILE:-.env.prod}"
TEST_ENV_FILE="${TEST_ENV_FILE:-.env.test}"
NGINX_AVAILABLE_DIR="${NGINX_AVAILABLE_DIR:-/etc/nginx/sites-available}"
NGINX_ENABLED_DIR="${NGINX_ENABLED_DIR:-/etc/nginx/sites-enabled}"
PROD_NGINX_TEMPLATE="${PROD_NGINX_TEMPLATE:-deploy/nginx/payment-prod-namecheap.conf}"
TEST_NGINX_TEMPLATE="${TEST_NGINX_TEMPLATE:-deploy/nginx/payment-test-namecheap.conf}"

require_file() {
    local file_path="$1"
    if [[ ! -f "$file_path" ]]; then
        echo "Missing required file: $file_path" >&2
        exit 1
    fi
}

echo "[1/6] Checking project files"
cd "$PROJECT_ROOT"
require_file "$PROD_ENV_FILE"
require_file "$TEST_ENV_FILE"
require_file "$PROD_NGINX_TEMPLATE"
require_file "$TEST_NGINX_TEMPLATE"

echo "[2/6] Starting production stack"
docker compose --env-file "$PROD_ENV_FILE" up -d --build

echo "[3/6] Starting test stack"
docker compose --env-file "$TEST_ENV_FILE" up -d --build

echo "[4/6] Installing nginx site configs"
install -d "$NGINX_AVAILABLE_DIR" "$NGINX_ENABLED_DIR"
install -m 644 "$PROD_NGINX_TEMPLATE" "$NGINX_AVAILABLE_DIR/payment.conf"
install -m 644 "$TEST_NGINX_TEMPLATE" "$NGINX_AVAILABLE_DIR/payment-test.conf"
ln -sfn "$NGINX_AVAILABLE_DIR/payment.conf" "$NGINX_ENABLED_DIR/payment.conf"
ln -sfn "$NGINX_AVAILABLE_DIR/payment-test.conf" "$NGINX_ENABLED_DIR/payment-test.conf"

echo "[5/6] Reloading nginx"
nginx -t
systemctl reload nginx

echo "[6/6] Verifying local health endpoints"
curl -fsS http://127.0.0.1:8080/health >/dev/null
curl -fsS http://127.0.0.1:8081/health >/dev/null

echo
echo "Dual environment bootstrap completed successfully."
echo "Production health: http://127.0.0.1:8080/health"
echo "Test health:       http://127.0.0.1:8081/health"
