# hexa-simulator

Hexa Simulator is a Hexa.Build-native Svelte + Go + PostgreSQL application for demonstrating GPS/IoT device behavior without physical tracker hardware.

## MVP

- Device-only simulator workspace with Hexa-styled dark UI.
- Register and delete virtual GPS devices using a name, IMEI/device ID, and model.
- Start/stop a simulation from the device list or live detail panel.
- Drive each virtual device in auto-route, manual-heading, or click-to-target mode with live speed control, pause/resume, and demo presets for overspeed, drift/route deviation, exit-zone, and return-to-route behavior.
- Refresh device/telemetry state from the Live Map without reloading the browser or interrupting an active simulation.
- Select devices through a paginated, server-side searchable Device Explorer (50 results per page) so the control workspace does not render an unbounded device list.
- With PostgreSQL configured, API startup idempotently fills the database to a 350-device demo target while preserving existing operator-created devices; Runtime activation therefore provisions an already-migrated database without requiring manual bulk entry.
- The workspace clock follows the accessing browser timezone, using WIB/WITA/WIT labels for Indonesia and a localized full date including the year.
- Inspect a dependency-free live route map with a moving, heading-aware vehicle marker plus speed, ignition, coordinates, and last-telemetry time.
- Runtime startup automatically starts the complete persisted demo fleet (350 devices after provisioning) and emits deterministic forestry telemetry for every device approximately every three seconds, independent of browser sessions. Initial ticks are evenly phased across one interval to avoid a startup thundering herd, and startup does not report the fleet ready until every newly started worker has persisted that initial tick.
- A page reload only observes the server-owned runtime; closing the browser does not stop fleet telemetry.
- Telemetry input is range-validated by the API.
- Optional backend-first Teltonika TCP output: when `TELTONIKA_GATEWAY_ADDR` is configured, online telemetry is encoded as Codec 8 Extended (`0x8E`) with an IMEI handshake, CRC-16/IBM, and AVL acknowledgement validation before the API reports success.
- Optional MQTT QoS 1 output publishes the canonical Hexa.Sensor `hexa.sensor/telemetry/v1` envelope to `SIM_MQTT_URL` using the configured `{imei}` topic template.
- Fleet protocol outputs use bounded asynchronous worker queues so a slow broker/listener cannot stop the 350-device simulation clock; Teltonika Direct keeps independent persistent sessions per IMEI.
- HEXA.SENSOR HTTP Push integration: each persisted online telemetry update can be POSTed to the configured Hexa.Sensor ingest endpoint using the `hexa.sensor/telemetry/v1` schema and Bearer authentication.
- The integration secret is never hardcoded; `SIM_SENSOR_PUSH_KEY` remains protected runtime configuration.

## Start here

~~~bash
./scripts/setup.sh
./scripts/migrate.sh   # requires DATABASE_URL
./scripts/verify.sh
.hexa/project dev start
~~~

The web UI is served by Vite at `http://127.0.0.1:5173` during development and proxies `/api` to the Go API on `127.0.0.1:8080`.

Read `AGENTS.md` and `docs/architecture/ARCHITECTURE.md` before extending the project.

## HEXA.SENSOR HTTP Push integration

hexa-simulator is the HTTP client for this integration. For each persisted online telemetry update it POSTs `hexa.sensor/telemetry/v1` JSON to `SIM_SENSOR_PUSH_URL` and authenticates with `Authorization: Bearer <SIM_SENSOR_PUSH_KEY>`. Configure both values only in protected environment configuration.

The HTTP Push integration is optional: when `SIM_SENSOR_PUSH_URL` and `SIM_SENSOR_PUSH_KEY` are both absent, standalone simulator behavior is unchanged. The existing optional Teltonika TCP client remains available independently through `TELTONIKA_GATEWAY_ADDR` for protocol-level testing against an external compatible Gateway.

## Simulator authentication and security

When PostgreSQL is configured, the simulator API is protected by server-side authentication. Configure `SIM_ADMIN_PASSWORD` (minimum 12 characters) and `SIM_SECURITY_KEY` (minimum 32 characters) in protected runtime configuration before startup; `SIM_ADMIN_USERNAME` defaults to `hexa-dev`. The first startup creates the administrator account if it does not already exist.

The application uses HttpOnly SameSite=Strict server-side sessions, CSRF tokens for state-changing API requests, PBKDF2-HMAC-SHA256 password hashing with per-password random salts, optional TOTP MFA with encrypted-at-rest secrets, one-time recovery codes, administrator/operator/viewer roles, security response headers, password changes, and session inventory. Device, telemetry, delete, and Sensor onboarding endpoints require an authenticated session. For production exposure, terminate the application behind HTTPS/TLS and set `SIM_COOKIE_SECURE=true` when served through HTTPS; local Hexa.Build development can use `false` on loopback HTTP.
- Demo observability includes per-device runtime status plus a bounded transmission log for HTTP Push, MQTT and Teltonika Direct attempts, including success/error, latency and telemetry summary without exposing endpoint credentials.
