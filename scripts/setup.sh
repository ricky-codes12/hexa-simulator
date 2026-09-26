#!/usr/bin/env sh
set -eu
command -v go >/dev/null 2>&1 || { echo 'Go 1.23+ is required' >&2; exit 69; }
command -v bun >/dev/null 2>&1 || { echo 'Bun 1.3.14 is required' >&2; exit 69; }
[ "$(bun --version)" = "1.3.14" ] || { echo "Bun 1.3.14 is required; found $(bun --version)" >&2; exit 69; }
go mod verify
(cd web && bun install --no-save)
printf 'setup: Go and Svelte toolchains are ready\n'
if [ -n "${DATABASE_URL:-}" ]; then
  printf 'setup: PostgreSQL configured; run ./scripts/migrate.sh explicitly before using persistence\n'
else
  printf 'setup: PostgreSQL not configured yet; set DATABASE_URL when persistence is needed\n'
fi
