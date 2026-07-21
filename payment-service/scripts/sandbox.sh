#!/usr/bin/env bash
set -euo pipefail

# WSL/Bash control surface for the local Sandbox stack. Never use this script
# for Production: it only accepts .env.sandbox and its output avoids secrets.
action="${1:-config}"
env_file="${2:-.env.sandbox}"

case "$action" in config|up|down|logs|health|rollback) ;; *) echo "usage: $0 {config|up|down|logs|health|rollback} [.env.sandbox]" >&2; exit 2;; esac
[[ "$env_file" == .env.sandbox || "$env_file" == */.env.sandbox ]] || { echo "only a Sandbox env file is allowed" >&2; exit 2; }
[[ -f "$env_file" ]] || { echo "Sandbox env file not found: $env_file" >&2; exit 1; }

env_value() { sed -n "s/^$1=//p" "$env_file" | tail -n 1 | tr -d '\r'; }
case "$action" in
  config) docker compose --env-file "$env_file" config ;;
  up) docker compose --env-file "$env_file" up -d --build ;;
  down) docker compose --env-file "$env_file" down ;;
  logs) docker compose --env-file "$env_file" logs --tail 200 payment-api ;;
  health)
    port="$(env_value APP_HOST_PORT)"; port="${port:-8081}"
    curl --fail --silent --show-error "http://127.0.0.1:${port}/health"; echo ;;
  rollback)
    docker compose --env-file "$env_file" down
    echo 'Sandbox stopped. Restore only an approved Sandbox backup into the Sandbox DB.' ;;
esac
