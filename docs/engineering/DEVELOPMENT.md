# Development

Run ./scripts/setup.sh once after creation, then .hexa/project dev start for supervised API + Vite development. Direct go run ./cmd/api serve and (cd web && bun run dev) remain available; Hexa.Build does not hide the normal toolchains.

Persistence is optional until configured. Set DATABASE_URL to a local PostgreSQL instance and run ./scripts/migrate.sh explicitly. Never commit that URL if it contains credentials. Setup and Runtime build never mutate a database implicitly.
