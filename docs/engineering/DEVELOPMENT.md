# Development

Run ./scripts/setup.sh once after creation, then .hexa/project dev start for supervised API + Vite development. Direct go run ./cmd/api serve and (cd web && bun run dev) remain available; Hexa.Build does not hide the normal toolchains.

Persistence is optional until configured. Set DATABASE_URL to a local PostgreSQL instance and run ./scripts/migrate.sh explicitly. Never commit that URL if it contains credentials. Setup and Runtime build never mutate a database implicitly.


## Teltonika Gateway output

The simulator remains standalone by default. To forward online telemetry to a Teltonika-compatible TCP Gateway during development, set `TELTONIKA_GATEWAY_ADDR=host:port`. `TELTONIKA_GATEWAY_TIMEOUT` accepts a Go duration such as `5s` and defaults to five seconds. Do not commit real environment-specific addresses or credentials. The virtual device IMEI must be a 15-digit Teltonika-compatible IMEI when gateway forwarding is enabled.


## HEXA.SENSOR integration

For HTTP Push development, configure `SIM_SENSOR_PUSH_URL` and `SIM_SENSOR_PUSH_KEY` together in the local/protected environment. `SIM_SENSOR_PUSH_URL` is the full Hexa.Sensor connector endpoint (for example a local `/ingest/v1/<instance>` URL); `SIM_SENSOR_PUSH_KEY` is the connector ingest key and is sent as a Bearer credential. `SIM_SENSOR_PUSH_TIMEOUT` is optional and defaults to `5s`. Never commit the real key. These three names are declared in `.hexa/project.yaml` so Hexa.Build may pass protected values into the project adapter; the adapter writes development push credentials only to its mode-0600 project-state environment file and Runtime continues to consume them from `RUNTIME_ENV_FILE`.

The canonical local development endpoints are `http://127.0.0.1:5173` for the Vite UI and `http://127.0.0.1:8080` for the Go API. Vite proxies `/api` and `/healthz` to that API. When debugging UI/API consistency, test `127.0.0.1:8080`; another listening port is a different process and is not authoritative for `.hexa/project dev`.

With these values absent, no HTTP Push forwarder is enabled.

## Demo fleet and protocol status

Migration `003_demo_fleet.sql` adds 350 deterministic Teltonika FMC920 virtual trucks with unique 15-digit IMEIs and forestry-area starting positions. It is idempotent by IMEI and does not delete or rewrite operator-created devices. Device discovery remains server-side, capped at 100 results per request; the UI uses 50-row search pages rather than rendering the whole fleet.

The server-owned simulation runtime reports configured output paths per selected device. `http-push`, `mqtt`, and `teltonika-direct` are `ready` before the first tick, `sending` after a successful forward, and `error` after a failed forward. These are runtime observations, not synthetic frontend health indicators. Unconfigured outputs are shown as such.
