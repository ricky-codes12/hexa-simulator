#!/usr/bin/env sh
set -eu
command -v go >/dev/null 2>&1 || { echo 'Go 1.23+ is required' >&2; exit 69; }
command -v gofmt >/dev/null 2>&1 || { echo 'gofmt is required' >&2; exit 69; }
command -v bun >/dev/null 2>&1 || { echo 'Bun 1.3.14 is required' >&2; exit 69; }
[ "$(bun --version)" = "1.3.14" ] || { echo "Bun 1.3.14 is required; found $(bun --version)" >&2; exit 69; }
[ -d web/node_modules ] || { echo 'frontend dependencies are missing; run ./scripts/setup.sh first' >&2; exit 69; }
unformatted=$(gofmt -l cmd internal)
[ -z "$unformatted" ] || { printf 'gofmt required:\n%s\n' "$unformatted" >&2; exit 1; }
go mod verify
go vet ./...
go test ./...
(cd web && bun run check)
(cd web && bun run build)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT HUP INT TERM
go build -trimpath -o "$tmp/api" ./cmd/api
"$tmp/api" version >/dev/null
if [ -n "${TEST_DATABASE_URL:-}" ]; then
  command -v psql >/dev/null 2>&1 || { echo 'psql is required when TEST_DATABASE_URL is set' >&2; exit 69; }
  DATABASE_URL="$TEST_DATABASE_URL" ./scripts/migrate.sh
  TEST_DATABASE_URL="$TEST_DATABASE_URL" go test -tags=integration ./internal/postgresstore
else
  printf 'verify: PostgreSQL integration test SKIPPED (set TEST_DATABASE_URL to require it)\n'
fi
printf 'verify: PASS\n'
