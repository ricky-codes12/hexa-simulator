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
