# hexa-simulator

A small full-stack application created from Hexa.Build's **Svelte + Go + PostgreSQL** starter.
The generated source is owned by this project immediately. There is no generator reconciliation loop or runtime framework dependency on Hexa.Build.

## Start here

~~~bash
./scripts/setup.sh
./scripts/verify.sh
.hexa/project dev start
~~~

The web UI is served by Vite at `http://127.0.0.1:5173` during development and proxies /api to the Go API on `127.0.0.1:8080`.

PostgreSQL is intentionally progressive: without `DATABASE_URL`, the frontend/API still start and clearly report that persistence is not configured. Configure a local PostgreSQL URL, run `./scripts/migrate.sh`, and the included todo flow becomes persistent.

Read `AGENTS.md` and `docs/architecture/ARCHITECTURE.md` before extending the project.
