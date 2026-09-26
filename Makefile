SHELL := /bin/sh

.PHONY: setup verify test test-integration lint build web-build run migrate
setup:
	./scripts/setup.sh
verify:
	./scripts/verify.sh
test:
	go test ./...
test-integration:
	@test -n "$(TEST_DATABASE_URL)" || (echo 'TEST_DATABASE_URL is required' >&2; exit 69)
	DATABASE_URL="$(TEST_DATABASE_URL)" ./scripts/migrate.sh
	TEST_DATABASE_URL="$(TEST_DATABASE_URL)" go test -tags=integration ./internal/postgresstore
lint:
	@test -z "$$(gofmt -l cmd internal)"
	go vet ./...
	(cd web && bun run check)
build: web-build
	go build -trimpath -o bin/api ./cmd/api
web-build:
	(cd web && bun run build)
migrate:
	./scripts/migrate.sh
run:
	go run ./cmd/api serve
