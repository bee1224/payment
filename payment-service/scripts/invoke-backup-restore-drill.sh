#!/usr/bin/env bash
set -euo pipefail

source_container="${1:?usage: $0 SOURCE_CONTAINER SOURCE_DATABASE [BACKUP_DIRECTORY]}"
source_database="${2:?usage: $0 SOURCE_CONTAINER SOURCE_DATABASE [BACKUP_DIRECTORY]}"
backup_directory="${3:-$PWD/output/drills}"
drill_container="${DRILL_CONTAINER:-payment-restore-drill-db}"
mariadb_image="${MARIADB_IMAGE:-mariadb:10.11}"
[[ "$source_container" != "$drill_container" && "$drill_container" == *-restore-drill-db ]] || { echo 'invalid drill target' >&2; exit 2; }
[[ "$source_database" =~ ^[A-Za-z0-9_]+$ ]] || { echo 'database name may contain only letters, numbers, and underscore' >&2; exit 2; }
: "${MYSQL_ROOT_PASSWORD:?MYSQL_ROOT_PASSWORD must be supplied through the environment}"
mkdir -p "$backup_directory"
stamp="$(date -u +%Y%m%d-%H%M%S)"; backup="$backup_directory/${source_database}-${stamp}.sql"
docker inspect "$source_container" >/dev/null
docker exec -e "MYSQL_PWD=$MYSQL_ROOT_PASSWORD" "$source_container" mariadb-dump --single-transaction --routines --events --triggers --databases "$source_database" >"$backup"
[[ -s "$backup" ]] || { echo 'backup failed or was empty' >&2; exit 1; }
hash="$(sha256sum "$backup" | awk '{print $1}')"
docker run --rm -d --name "$drill_container" -e "MARIADB_ROOT_PASSWORD=$MYSQL_ROOT_PASSWORD" "$mariadb_image" --character-set-server=utf8mb4 --collation-server=utf8mb4_unicode_ci >/dev/null
cleanup() { docker rm -f "$drill_container" >/dev/null 2>&1 || true; }
trap cleanup EXIT
deadline=$((SECONDS + 90))
until docker exec -e "MYSQL_PWD=$MYSQL_ROOT_PASSWORD" "$drill_container" mariadb-admin ping -h 127.0.0.1 -uroot --silent >/dev/null; do
  (( SECONDS < deadline )) || { echo 'drill database did not become ready' >&2; exit 1; }
  sleep 2
done
docker exec -i -e "MYSQL_PWD=$MYSQL_ROOT_PASSWORD" "$drill_container" mariadb -uroot <"$backup"
source_tables="$(docker exec -e "MYSQL_PWD=$MYSQL_ROOT_PASSWORD" "$source_container" mariadb -N -uroot -e "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='$source_database'")"
drill_tables="$(docker exec -e "MYSQL_PWD=$MYSQL_ROOT_PASSWORD" "$drill_container" mariadb -N -uroot -e "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='$source_database'")"
[[ "$source_tables" == "$drill_tables" && "$drill_tables" -gt 0 ]] || { echo 'restored table count differs from source' >&2; exit 1; }
printf '{"backup":"%s","sha256":"%s","source_tables":%s,"restored_tables":%s,"result":"passed"}\n' "$backup" "$hash" "$source_tables" "$drill_tables"
