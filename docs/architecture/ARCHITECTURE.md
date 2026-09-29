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

The product model is `Device`, persisted in PostgreSQL and exposed through `/api/devices`. The Go API owns the demo simulation clock. Starting a device emits a telemetry update immediately and the server continues the three-second runtime loop independently of whether the browser remains open. Browser reloads reconnect to the persisted device/runtime state rather than owning the clock. Stopping it persists an offline sample with speed zero. Devices can also be deleted through the API/UI.

Telemetry follows one of two deterministic synthetic forestry haul routes selected from the device ID. The routes stay inside the same South Sumatra demo bounds used by Hexa.Sensor (`104.688,-3.042` to `104.905,-2.928`), so pushed telemetry remains visible in the Sensor demo world instead of landing outside its constrained map. This makes demonstrations repeatable instead of using random coordinate drift. The Go API continues to validate status, latitude, longitude, speed, and heading before persisting the latest sample.

The device detail view uses MapLibre GL with fully local GeoJSON context: the map starts with a minimal background style, then installs the committed estate, haul-road and facility GeoJSON sources/layers after MapLibre's `load` event. This explicit lifecycle keeps synthetic source loading observable and avoids coupling local context hydration to initial style parsing. Labels remain local HTML markers. No tile provider, map API key, or runtime map service is required. The selected device is rendered as a heading-aware marker. Backend/browser telemetry cadence remains approximately three seconds, while `requestAnimationFrame` interpolation eases the marker between accepted samples and interpolates heading over the shortest turn. The trail is built only from accepted device telemetry points; animation does not manufacture additional persisted telemetry.

The simulator now has an optional backend Teltonika TCP output boundary. When `TELTONIKA_GATEWAY_ADDR` is configured, each online application telemetry update is also encoded as a single-record Codec 8 Extended (`0x8E`) AVL packet. The adapter performs the Teltonika TCP IMEI handshake, validates the one-byte IMEI acceptance response, sends the AVL packet with CRC-16/IBM, and requires a four-byte acknowledgement accepting exactly one record. Gateway connection failures are returned as HTTP 502 so integration failures are visible during a demo rather than silently ignored.

The gateway address and timeout are external configuration (`TELTONIKA_GATEWAY_ADDR`, `TELTONIKA_GATEWAY_TIMEOUT`); no downstream address or credential is committed. With no gateway address configured, standalone simulator behavior is unchanged. The adapter keeps an independent persistent TCP session per IMEI and reconnects that device session after transport failure; different IMEIs are not serialized behind one process-wide network lock. This does **not** claim an APSS Tensor application contract, UDP support, or every Teltonika IO element. The current wire contract is the documented Teltonika TCP + Codec 8 Extended subset needed to expose simulator GPS, speed, heading, timestamp, and ignition telemetry to a compatible Gateway.



## HEXA.SENSOR HTTP Push integration

The application integration is push-based:

~~~text
hexa-simulator -> HTTP POST + Bearer ingest key -> HEXA.SENSOR HTTP Push connector
~~~

For each persisted **online** telemetry update, hexa-simulator can POST one `hexa.sensor/telemetry/v1` record to `SIM_SENSOR_PUSH_URL`. The virtual device IMEI maps to `device.hardware_id`; persisted timestamp maps to `device_time`; latitude, longitude, speed and heading map to `position`; and ignition plus derived movement (`speed > 0`) map to `attributes`. `position.fix_valid` is true for the simulator's validated deterministic route samples.

`SIM_SENSOR_PUSH_URL` and `SIM_SENSOR_PUSH_KEY` must be configured together in protected environment configuration. The key is sent only as `Authorization: Bearer <key>` and is never committed. `SIM_SENSOR_PUSH_TIMEOUT` defaults to five seconds. Non-2xx responses, network errors and timeouts are surfaced through the telemetry API as forwarding failures so a broken demo integration is visible.

When HTTP Push configuration is absent, standalone simulator behavior is unchanged. Optional Teltonika TCP output through `TELTONIKA_GATEWAY_ADDR` remains an independent protocol-testing feature and may be enabled alongside HTTP Push.

## Simulator authentication and MFA enrollment

Simulator access uses server-side sessions and requires TOTP enrollment before an authenticated account can use simulator or administration APIs. Password verification is the first sign-in step. Accounts with MFA already enabled receive an explicit MFA challenge before a session is created. Accounts without MFA receive a restricted session that may access only authentication and MFA-enrollment endpoints until a TOTP secret is verified.

The enrollment UI renders the backend-issued `otpauth://` URI as a local QR code in the browser and also exposes the setup key as a fallback. The QR image is generated client-side; the TOTP secret is not sent to any third-party QR service. After successful verification, recovery codes are shown once and normal simulator access is enabled. Subsequent sign-ins require the authenticator or a recovery code.

## Server-side multi-protocol simulation runtime

Simulation progression is owned by the Go API, not by a browser timer. `POST /api/devices/{id}/simulation` changes start/pause/resume/stop and drive controls; the runtime ticks approximately every three seconds, persists current telemetry, and fans each online record to configured outputs. Reopening the UI only observes and controls this state, so it must not create a second simulation loop.

Output adapters are optional and environment-owned: existing Hexa.Sensor HTTP Push, persistent per-IMEI Teltonika Direct TCP with Codec 8 or 8E and acknowledgement/reconnect, and MQTT 3.1.1 QoS 1 publish using the `hexa.sensor/telemetry/v1` JSON contract. MQTT credentials and Sensor keys remain protected Runtime configuration and must never be committed or returned to the browser. Teltonika/Sitepat cloud mocks are intentionally out of scope until authoritative vendor API contracts exist.


## Concurrent demo fleet startup

After PostgreSQL demo-fleet provisioning completes, the API restores the complete persisted fleet into the server-owned runtime. Each device has exactly one cancellable loop. The loops are started with deterministic phase offsets spread across one telemetry interval, preventing all 350 devices from writing and forwarding on the same instant while preserving an approximately three-second cadence per device. Browser sessions are observers/controllers only; they are not required for any fleet clock. Per-device stop/pause/start semantics remain authoritative after startup. `StartFleet` uses an initial-tick completion barrier: it returns only after every newly created worker has persisted its phased first telemetry sample (or returns the first startup error). This makes 350-device readiness deterministic without collapsing regular telemetry into a thundering herd.

## Fleet-scale output isolation

The server-owned simulation clock is intentionally isolated from downstream protocol latency. Each configured output has a bounded 700-item queue and 16 workers. A telemetry tick persists the device update first and then performs a non-blocking enqueue per output. If a downstream path is unavailable long enough to fill its queue, that output reports an error for the affected device instead of stalling the simulation loop or creating unbounded goroutines. This keeps the 350-device runtime authoritative even during broker/listener outages.

MQTT publishes the canonical `hexa.sensor/telemetry/v1` envelope expected by Hexa.Sensor: `device.hardware_id`, `device_time`, normalized `position` fields (`fix_valid`, `lat`, `lon`, `speed_kmh`, `heading_deg`), and ignition/movement attributes. QoS 1 acknowledgement remains required.

Teltonika Direct keeps one persistent TCP session per IMEI and synchronizes only that device session. Different virtual devices may therefore connect/send concurrently; a slow or reconnecting tracker no longer serializes every other IMEI behind a process-wide network lock. Codec 8/8E handshake, CRC and one-record AVL acknowledgement requirements remain unchanged.
