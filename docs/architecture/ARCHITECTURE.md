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

Telemetry follows one of two deterministic Jakarta demo routes selected from the device ID. This makes demonstrations repeatable instead of using random coordinate drift. The Go API validates status, latitude, longitude, speed, and heading before persisting the latest sample. The device detail view projects that same deterministic route into an inline SVG map, so the live marker moves without introducing a map SDK, network tile dependency, API key, or new runtime service.

The simulator now has an optional backend Teltonika TCP output boundary. When `TELTONIKA_GATEWAY_ADDR` is configured, each online application telemetry update is also encoded as a single-record Codec 8 Extended (`0x8E`) AVL packet. The adapter performs the Teltonika TCP IMEI handshake, validates the one-byte IMEI acceptance response, sends the AVL packet with CRC-16/IBM, and requires a four-byte acknowledgement accepting exactly one record. Gateway connection failures are returned as HTTP 502 so integration failures are visible during a demo rather than silently ignored.

The gateway address and timeout are external configuration (`TELTONIKA_GATEWAY_ADDR`, `TELTONIKA_GATEWAY_TIMEOUT`); no downstream address or credential is committed. With no gateway address configured, standalone simulator behavior is unchanged. The current adapter opens a TCP connection for each telemetry sample, which is intentionally simple and deterministic for integration testing; connection pooling/session persistence can be introduced after the target Gateway behavior is known. This does **not** claim an APSS Tensor application contract, UDP support, or every Teltonika IO element. The current wire contract is the documented Teltonika TCP + Codec 8 Extended subset needed to expose simulator GPS, speed, heading, timestamp, and ignition telemetry to a compatible Gateway.



## HEXA.SENSOR HTTP Push integration

The application integration is push-based:

~~~text
hexa-simulator -> HTTP POST + Bearer ingest key -> HEXA.SENSOR HTTP Push connector
~~~

For each persisted **online** telemetry update, hexa-simulator can POST one `hexa.sensor/telemetry/v1` record to `SIM_SENSOR_PUSH_URL`. The virtual device IMEI maps to `device.hardware_id`; persisted timestamp maps to `device_time`; latitude, longitude, speed and heading map to `position`; and ignition plus derived movement (`speed > 0`) map to `attributes`. `position.fix_valid` is true for the simulator's validated deterministic route samples.

`SIM_SENSOR_PUSH_URL` and `SIM_SENSOR_PUSH_KEY` must be configured together in protected environment configuration. The key is sent only as `Authorization: Bearer <key>` and is never committed. `SIM_SENSOR_PUSH_TIMEOUT` defaults to five seconds. Non-2xx responses, network errors and timeouts are surfaced through the telemetry API as forwarding failures so a broken demo integration is visible.

When HTTP Push configuration is absent, standalone simulator behavior is unchanged. Optional Teltonika TCP output through `TELTONIKA_GATEWAY_ADDR` remains an independent protocol-testing feature and may be enabled alongside HTTP Push.
