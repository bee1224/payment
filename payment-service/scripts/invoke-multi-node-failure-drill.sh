#!/usr/bin/env bash
set -euo pipefail

project_name="${1:-payment-drill}"
[[ "$project_name" == *drill* ]] || { echo 'project name must contain drill' >&2; exit 2; }
[[ -f .env.drill ]] || { echo 'Create an isolated .env.drill first' >&2; exit 1; }
compose=(docker compose -p "$project_name" --env-file .env.drill -f deploy/drill/compose.multi-node.yaml)
"${compose[@]}" up -d --build
cleanup() { "${compose[@]}" down -v --remove-orphans; }
trap cleanup EXIT
for node in api-1 api-2; do
  deadline=$((SECONDS + 180))
  until "${compose[@]}" exec -T "$node" wget -q -O - http://127.0.0.1:8080/health >/dev/null; do
    (( SECONDS < deadline )) || { echo "$node did not become healthy" >&2; exit 1; }
    sleep 2
  done
done
deadline=$((SECONDS + 180))
until curl --fail --silent --show-error --max-time 3 http://127.0.0.1:18080/health >/dev/null; do
  (( SECONDS < deadline )) || { echo 'load-balanced health check did not pass' >&2; exit 1; }
  sleep 2
done
victim="$("${compose[@]}" ps -q api-1)"
[[ -n "$victim" ]] || { echo 'no API node available for fault injection' >&2; exit 1; }
docker stop "$victim" >/dev/null
deadline=$((SECONDS + 30))
until curl --fail --silent --show-error --max-time 3 http://127.0.0.1:18080/health >/dev/null; do
  (( SECONDS < deadline )) || { echo 'service did not remain healthy after one node stopped' >&2; exit 1; }
  sleep 2
done
printf '{"project":"%s","stopped_node":"%s","health_after_failure":200,"result":"passed"}\n' "$project_name" "$victim"
