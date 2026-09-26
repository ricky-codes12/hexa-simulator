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



## HEXA.SENSOR pull integration

The agreed application integration is pull-based:

~~~text
HEXA.SENSOR -> HTTP GET /api/integration/devices + X-SECRET-KEY -> hexa-simulator
            <- JSON device + latest telemetry state                 <-
~~~

HEXA.SENSOR initiates the request and hexa-simulator is the data provider. The endpoint returns the same persisted `Device` representation used by the simulator, including IMEI, status, latitude, longitude, speed, heading, ignition, and timestamps. This keeps the integration generic so HEXA.SENSOR can apply its own mapping/rules.

The inbound `X-SECRET-KEY` value is compared with `HEXA_SENSOR_SECRET_KEY` from protected environment configuration. If the secret is absent, the integration endpoint fails closed with HTTP 503; missing or incorrect request credentials receive HTTP 401. The browser-facing `/api/devices` endpoint remains unchanged for the simulator UI.

The previous project-owned TCP-to-HTTP forwarding Gateway is intentionally removed: the simulator no longer needs a HEXA.SENSOR URL and does not push telemetry to HEXA.SENSOR. Optional Teltonika TCP output through `TELTONIKA_GATEWAY_ADDR` remains an independent protocol-testing feature.
