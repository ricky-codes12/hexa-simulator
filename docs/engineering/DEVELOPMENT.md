# Development

Run ./scripts/setup.sh once after creation, then .hexa/project dev start for supervised API + Vite development. Direct go run ./cmd/api serve and (cd web && bun run dev) remain available; Hexa.Build does not hide the normal toolchains.

Persistence is optional until configured. Set DATABASE_URL to a local PostgreSQL instance and run ./scripts/migrate.sh explicitly. Never commit that URL if it contains credentials. Setup and Runtime build never mutate a database implicitly.


## Teltonika Gateway output

The simulator remains standalone by default. To forward online telemetry to a Teltonika-compatible TCP Gateway during development, set `TELTONIKA_GATEWAY_ADDR=host:port`. `TELTONIKA_GATEWAY_TIMEOUT` accepts a Go duration such as `5s` and defaults to five seconds. Do not commit real environment-specific addresses or credentials. The virtual device IMEI must be a 15-digit Teltonika-compatible IMEI when gateway forwarding is enabled.


## HEXA.SENSOR integration

For HTTP Push development, configure `SIM_SENSOR_PUSH_URL` and `SIM_SENSOR_PUSH_KEY` together in the local/protected environment. `SIM_SENSOR_PUSH_URL` is the full Hexa.Sensor connector endpoint (for example a local `/ingest/v1/<instance>` URL); `SIM_SENSOR_PUSH_KEY` is the connector ingest key and is sent as a Bearer credential. `SIM_SENSOR_PUSH_TIMEOUT` is optional and defaults to `5s`. Never commit the real key.

With these values absent, no HTTP Push forwarder is enabled.
