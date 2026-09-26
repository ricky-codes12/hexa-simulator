# Architecture

## Shape

~~~text
web/                    Svelte 5 + Vite frontend
cmd/api/                Go process entry point
internal/httpapi/       HTTP/API and static-web boundary
internal/postgresstore/ PostgreSQL adapter behind a small interface
db/migrations/          project-owned schema migrations
scripts/                setup, migration and canonical verification
.hexa/project           Hexa.Build Dev + Runtime lifecycle adapter
~~~

## Invariants

- Browser code talks to the application through /api; it never receives database credentials.
- PostgreSQL is an external persistent dependency. Runtime release roots remain immutable.
- Domain/application behavior should move into explicit internal packages as the product grows; do not turn HTTP handlers or database rows into the domain model by default.
- Canonical verification is ./scripts/verify.sh; CI and Hexa.Build call it rather than duplicating test policy.
- /healthz remains cheap and reports database configuration/readiness separately.
- Svelte code uses Svelte 5 conventions. New event handlers use event properties such as onclick/onsubmit, not legacy on: directives.
- Generated code has no runtime dependency on Hexa.Build.

## Device simulator MVP

The product model is now `Device`, persisted in PostgreSQL and exposed through `/api/devices`. The browser owns the demo simulation clock: starting a device emits a telemetry update immediately and every three seconds while that page remains open. The Go API validates and persists the latest sample; it does not run hidden background simulators.

This intentionally keeps the first demo deterministic and observable. A future Teltonika/TCP/UDP/MQTT output adapter should be introduced behind an explicit application boundary rather than embedded in HTTP handlers or browser code. IMEI values are unique persistent identifiers; secrets and downstream credentials remain external configuration.
