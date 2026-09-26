# hexa-simulator

Hexa Simulator is a Hexa.Build-native Svelte + Go + PostgreSQL application for demonstrating GPS/IoT device behavior without physical tracker hardware.

## MVP

- Device-only simulator workspace with Hexa-styled dark UI.
- Register virtual GPS devices using a name, IMEI/device ID, and model.
- Start/stop a simulation from the device list or detail panel.
- While running, the browser emits a telemetry sample every three seconds (position, speed, heading, ignition) through the Go API and persists the latest state in PostgreSQL.
- The simulator does not claim to implement Teltonika wire protocols yet; the current boundary is an application-level telemetry simulator ready for a future protocol/output adapter.

## Start here

~~~bash
./scripts/setup.sh
./scripts/migrate.sh   # requires DATABASE_URL
./scripts/verify.sh
.hexa/project dev start
~~~

The web UI is served by Vite at `http://127.0.0.1:5173` during development and proxies `/api` to the Go API on `127.0.0.1:8080`.

Read `AGENTS.md` and `docs/architecture/ARCHITECTURE.md` before extending the project.
