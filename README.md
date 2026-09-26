# hexa-simulator

Hexa Simulator is a Hexa.Build-native Svelte + Go + PostgreSQL application for demonstrating GPS/IoT device behavior without physical tracker hardware.

## MVP

- Device-only simulator workspace with Hexa-styled dark UI.
- Register and delete virtual GPS devices using a name, IMEI/device ID, and model.
- Start/stop a simulation from the device list or detail panel.
- Online devices emit deterministic Jakarta route telemetry every three seconds (position, speed, heading, ignition) through the Go API and persist the latest state in PostgreSQL.
- A page reload resumes the browser simulation clock for devices persisted as online.
- Telemetry input is range-validated by the API.
- The simulator does not claim to implement Teltonika wire protocols or an APSS Tensor contract yet; the current boundary is an application-level telemetry simulator ready for a future output adapter.

## Start here

~~~bash
./scripts/setup.sh
./scripts/migrate.sh   # requires DATABASE_URL
./scripts/verify.sh
.hexa/project dev start
~~~

The web UI is served by Vite at `http://127.0.0.1:5173` during development and proxies `/api` to the Go API on `127.0.0.1:8080`.

Read `AGENTS.md` and `docs/architecture/ARCHITECTURE.md` before extending the project.
