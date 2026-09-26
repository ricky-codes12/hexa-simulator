#!/usr/bin/env sh
set -eu
: "${DATABASE_URL:?DATABASE_URL is required}"
command -v psql >/dev/null 2>&1 || { echo 'psql is required to apply migrations' >&2; exit 69; }
for migration in db/migrations/*.sql; do
  printf 'migrate: %s\n' "$migration"
  psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f "$migration" >/dev/null
done
printf 'migrate: PASS\n'
