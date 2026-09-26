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

The product model is `Device`, persisted in PostgreSQL and exposed through `/api/devices`. The browser owns the demo simulation clock. Starting a device emits a telemetry update immediately and every three seconds while that page remains open. If the page reloads while a device is persisted as online, the browser resumes that device's simulation clock. Stopping it persists an offline sample with speed zero. Devices can also be deleted through the API/UI.

Telemetry follows one of two deterministic Jakarta demo routes selected from the device ID. This makes demonstrations repeatable instead of using random coordinate drift. The Go API validates status, latitude, longitude, speed, and heading before persisting the latest sample.

This intentionally remains an application-level GPS telemetry simulator. It does **not** claim Teltonika Codec 8/8E, TCP/UDP, MQTT, or an APSS Tensor contract. A downstream output adapter must be introduced behind an explicit application boundary after the target endpoint/protocol, authentication, acknowledgement/retry behavior, and payload contract are known. Downstream credentials must remain external configuration.
