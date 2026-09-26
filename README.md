# hexa-simulator

Hexa Simulator is a Hexa.Build-native Svelte + Go + PostgreSQL application for demonstrating GPS/IoT device behavior without physical tracker hardware.

## MVP

- Device-only simulator workspace with Hexa-styled dark UI.
- Register and delete virtual GPS devices using a name, IMEI/device ID, and model.
- Start/stop a simulation from the device list or live detail panel.
- Inspect a dependency-free live route map with a moving, heading-aware vehicle marker plus speed, ignition, coordinates, and last-telemetry time.
- Online devices emit deterministic Jakarta route telemetry every three seconds (position, speed, heading, ignition) through the Go API and persist the latest state in PostgreSQL.
- A page reload resumes the browser simulation clock for devices persisted as online.
- Telemetry input is range-validated by the API.
- Optional backend-first Teltonika TCP output: when `TELTONIKA_GATEWAY_ADDR` is configured, online telemetry is encoded as Codec 8 Extended (`0x8E`) with an IMEI handshake, CRC-16/IBM, and AVL acknowledgement validation before the API reports success.
- Optional project-owned Gateway mode: a TCP listener accepts the simulator IMEI + Codec 8E packet, validates/decodes it, returns the Teltonika acknowledgement, normalizes telemetry to JSON, and forwards it to a configured HEXA.SENSOR HTTP ingest URL with `X-SECRET-KEY` authentication.
- The simulator does not hardcode a HEXA.SENSOR URL or secret; both remain protected runtime configuration.

## Start here

~~~bash
./scripts/setup.sh
./scripts/migrate.sh   # requires DATABASE_URL
./scripts/verify.sh
.hexa/project dev start
~~~

The web UI is served by Vite at `http://127.0.0.1:5173` during development and proxies `/api` to the Go API on `127.0.0.1:8080`.

Read `AGENTS.md` and `docs/architecture/ARCHITECTURE.md` before extending the project.

## Teltonika -> HEXA.SENSOR Gateway

To exercise the complete demo path, configure `TELTONIKA_GATEWAY_LISTEN` plus `HEXA_SENSOR_URL` and `HEXA_SENSOR_SECRET_KEY`. If `TELTONIKA_GATEWAY_ADDR` is blank, the simulator automatically sends its Codec 8E telemetry to the local listener it started. The Gateway normalizes each accepted record to JSON with `device_id`, `timestamp`, `location.latitude`, `location.longitude`, `speed`, `heading`, `ignition`, `status`, and `protocol`, then performs an HTTP POST with `Content-Type: application/json` and the configured secret in the `X-SECRET-KEY` header.

The current Gateway intentionally supports the single-record Codec 8 Extended subset emitted by this simulator. Fuel is not fabricated because the current simulator wire model does not emit a fuel IO element.
