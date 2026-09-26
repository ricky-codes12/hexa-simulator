# Hexa.Build Project

Project: hexa-simulator
Starter: Svelte + Go + PostgreSQL 1.0.0

This repository was generated from a versioned Project Genesis starter. The starter is provenance only; the repository owns its code, dependencies, tests, migrations, CI and Runtime behavior.

## Canonical lifecycle

- `.hexa/project doctor` checks Git, Go, Bun 1.3.14 and user-systemd prerequisites.
- `.hexa/project setup` installs frontend dependencies in the ignored `web/node_modules` tree and verifies Go modules.
- `.hexa/project verify` runs Go tests/static analysis plus Svelte checks/build. PostgreSQL integration is required when `TEST_DATABASE_URL` is set.
- `.hexa/project dev ...` supervises the Go API and Vite dev server through transient user-systemd units.
- Runtime builds immutable API + static Svelte assets from accepted source and requires a protected external `DATABASE_URL` before activation.

## Dependency policy

Direct frontend dependency versions and Bun 1.3.14 are pinned by this starter version. The project uses Bun exclusively for frontend dependency installation and scripts; there is no alternate package-manager fallback path. M8.2 does not introduce automatic starter upgrades. Transitive frontend resolution is refreshed deliberately when the starter dependency baseline changes and must be requalified before release.

Go database access uses the small `database/sql` boundary with `github.com/lib/pq` pinned in `go.mod`/`go.sum`. Replace it normally if the project later chooses another PostgreSQL driver.

## Database configuration

No real password or database URL is generated. `config/runtime.env.example` is documentation only. Keep real development/Runtime configuration outside Git.

## Runtime

The immutable Runtime release contains an API binary plus compiled web assets. PostgreSQL remains external persistent infrastructure and is never stored inside an immutable Runtime release directory. Runtime build/activation never applies database migrations implicitly; migrations remain an explicit project-owned operation under the deployment/database authority you choose.
