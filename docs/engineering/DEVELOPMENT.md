# Development

Run ./scripts/setup.sh once after creation, then .hexa/project dev start for supervised API + Vite
development. Direct go run ./cmd/api serve and (cd web && bun run dev) remain available;
Hexa.Build does not hide the normal toolchains.

## Dev plane

`.hexa/project dev start` builds the API, starts it and Vite as transient user-systemd units, and
passes every simulator setting in the environment (`SIM_*`, `TELTONIKA_*`, `DATABASE_URL`) to the
API through a mode-0600 file in the project state directory. The ports default to 8080 (API) and
5173 (web), which collide with hexa-ai's Dev plane; set `SIM_DEV_API_PORT` and `SIM_DEV_WEB_PORT`
to move them. Keep the settings in a protected file outside the repository and load it before
`dev start`. Hexa.Build refuses to pass secret-like names (`*PASSWORD*`, `*TOKEN*`, `*SECRET*`, …)
through `.hexa/project.yaml`, so the admin password, security key and broker password reach the
Dev plane only when the adapter is started from this shell; Runtime reads them from its protected
`runtime.env`:

~~~bash
set -a; . ~/somewhere-safe/hexa-simulator-dev.env; set +a
.hexa/project dev start
~~~

## Database

Persistence needs PostgreSQL. Set DATABASE_URL to a disposable local database and run
./scripts/migrate.sh explicitly (it needs `psql`). Migrations are idempotent and re-applied in
order; `004_fleet.sql` adds the fleet columns, behaviours and the fleet clock, and removes the old
350-truck grid whose IMEIs collided with Hexa.Sensor's own fleet. Startup then seeds the fleet from
`SIM_FLEET` and `SIM_SEED`, keeping operator-created devices and operator changes to seeded
ones (such as their output).

## The fleet and Hexa.Sensor

- The world is Hexa.Sensor's (`internal/world`). Change it only to follow Sensor's seeded world,
  and bump `world.Revision` with it.
- A new kind of device is a `fleet.Kind` (its fields, asset type, default output) plus an
  itinerary builder in `internal/fleet/plans.go`. Keep its day inside the operating area and out of
  the conservation zone; `TestItinerariesRaiseNoAlarmsByThemselves` checks every seeded unit.
- A new behaviour is a `scenario.Spec` plus its effect in `SimulationRuntime.advance` (motion) or
  `filter` (what is sent). Write it against records, not GPS, when it can be.
- Wire a Sensor Dev plane with `bin/api sensor-onboard --sensor http://127.0.0.1:19180 --sources …`
  (docs/FLEET.md §2). It signs in as a tenant administrator; `SENSOR_TOTP_SECRET` lets a
  Dev-only account run it unattended.

Never commit a database URL with credentials, a broker password or a Sensor ingest key.
