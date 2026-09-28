# hexa-simulator

Hexa Simulator is a Hexa.Build-native Svelte + Go + PostgreSQL application for demonstrating GPS/IoT device behavior without physical tracker hardware.

## MVP

- Device-only simulator workspace with Hexa-styled dark UI.
- Register and delete virtual GPS devices using a name, IMEI/device ID, and model.
- Start/stop a simulation from the device list or live detail panel.
- Drive each virtual device in auto-route, manual-heading, or click-to-target mode with live speed control, pause/resume, and demo presets for overspeed, drift/route deviation, exit-zone, and return-to-route behavior.
- Refresh device/telemetry state from the Live Map without reloading the browser or interrupting an active simulation.
- Inspect a dependency-free live route map with a moving, heading-aware vehicle marker plus speed, ignition, coordinates, and last-telemetry time.
- Online devices emit deterministic Jakarta route telemetry every three seconds (position, speed, heading, ignition) through the Go API and persist the latest state in PostgreSQL.
- A page reload resumes the browser simulation clock for devices persisted as online.
- Telemetry input is range-validated by the API.
- Optional backend-first Teltonika TCP output: when `TELTONIKA_GATEWAY_ADDR` is configured, online telemetry is encoded as Codec 8 Extended (`0x8E`) with an IMEI handshake, CRC-16/IBM, and AVL acknowledgement validation before the API reports success.
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

When PostgreSQL is configured, the simulator API is protected by server-side authentication. Configure `SIM_ADMIN_PASSWORD` (minimum 12 characters) and `SIM_SECURITY_KEY` (minimum 32 characters) in protected runtime configuration before startup; `SIM_ADMIN_EMAIL` defaults to `admin@simulator.local`. The first startup creates the administrator account if it does not already exist.

The application uses HttpOnly SameSite=Strict server-side sessions, CSRF tokens for state-changing API requests, PBKDF2-HMAC-SHA256 password hashing with per-password random salts, optional TOTP MFA with encrypted-at-rest secrets, one-time recovery codes, administrator/operator/viewer roles, security response headers, password changes, and session inventory. Device, telemetry, delete, and Sensor onboarding endpoints require an authenticated session. For production exposure, terminate the application behind HTTPS/TLS and set `SIM_COOKIE_SECURE=true` when served through HTTPS; local Hexa.Build development can use `false` on loopback HTTP.
