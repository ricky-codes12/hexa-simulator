# hexa-simulator

Hexa.Simulator is a Hexa.Build-native Svelte + Go + PostgreSQL application that plays **the field**
for Hexa.Sensor: a fleet of virtual GPS trackers that reach Sensor the way real devices do, over
MQTT, Teltonika TCP and HTTP push, and misbehave on cue so Sensor has something to raise alarms
about.

## What it does

- **A believable fleet.** 350 units by default (`SIM_FLEET`): haul trucks on the estate haul
  roads, farm tractors working planting blocks lane by lane with the implement down, dozers in the
  Sialang concession, excavators at the loading points, pickups visiting blocks from their estate
  office, water trucks and fuel bowsers on scheduled rounds. Every unit is deterministic from a
  seed (`SIM_SEED`, default 7), so every run looks the same.
- **Hexa.Sensor's world.** Estates, blocks, roads, the conservation zone and compartment C07 are
  Hexa.Sensor's seeded forestry world (revision 3, Musi Banyuasin), mirrored in `internal/world`. The
  fleet stays inside the operating area and raises no alarm by itself.
- **One output per device.** Each device sends over `mqtt`, `teltonika`, `http-push`, `all` (every
  configured output, for protocol testing) or `none`. The default split is about 55% MQTT, 30%
  Teltonika TCP and 15% HTTP push; routing is changed per device or per group.
- **Fields Sensor's rules read.** Every record carries ignition, movement, vehicle and tracker
  battery voltage, power cut, GNSS status, GSM signal and odometer; tractors add `din1` (the
  implement). Teltonika records carry the same values as FMC IO elements (1, 16, 21, 66, 67, 69,
  239, 240).
- **Behaviours and a timeline.** Overspeed, leaving the operating area, signal loss, intermittent
  or burst reporting, power cut, idle, park and working a compartment can be given to a device or a
  group, now or later. The `forestry-30min` timeline plays them in a fixed order with a countdown;
  **Reset to T0** puts the fleet back at its start.
- **Onboarding for Sensor.** One export registers the fleet in Sensor: its device types and the four
  CSV imports (`/api/fleet/sensor-onboarding.zip`). `hexa-simulator sensor-onboard` applies it,
  and with `--sources` creates Sensor's three sources too.
- **Server-owned clock.** The Go API runs the fleet whether or not a browser is open; outputs sit
  behind bounded queues, so a slow broker or listener never stalls it.

Read [docs/FLEET.md](docs/FLEET.md) to run the fleet against Hexa.Sensor.

## Start here

~~~bash
./scripts/setup.sh
DATABASE_URL=... ./scripts/migrate.sh
./scripts/verify.sh
.hexa/project dev start    # SIM_DEV_API_PORT / SIM_DEV_WEB_PORT override 8080 / 5173
~~~

The web UI is served by Vite (default `http://127.0.0.1:5173`) and proxies `/api` to the Go API
(default `127.0.0.1:8080`).

## Configuration

All values live in protected environment configuration, never in Git
([config/runtime.env.example](config/runtime.env.example) lists them).

| Setting | Meaning |
| --- | --- |
| `DATABASE_URL` | PostgreSQL. Apply `db/migrations` first; the API refuses to start on an older schema |
| `SIM_ADMIN_USERNAME`, `SIM_ADMIN_PASSWORD`, `SIM_SECURITY_KEY` | First administrator and the key that encrypts MFA secrets |
| `SIM_FLEET` | Empty for 350 units, a total such as `120`, `off`, or counts such as `haul-truck=40,farm-tractor=10` |
| `SIM_SEED` | Seed of the fleet's days (default 7) |
| `SIM_REPORT_INTERVAL` | How often a unit reports (default `5s`) |
| `SIM_MQTT_URL`, `SIM_MQTT_USERNAME`, `SIM_MQTT_PASSWORD`, `SIM_MQTT_CLIENT_ID`, `SIM_MQTT_TOPIC` | MQTT 3.1.1 QoS 1 output; topic template default `{imei}/data` (`{kind}` and `{estate}` also work) |
| `TELTONIKA_GATEWAY_ADDR`, `TELTONIKA_CODEC` | Teltonika Direct: one TCP session per IMEI, Codec 8E (default) or 8 |
| `SIM_SENSOR_PUSH_URL`, `SIM_SENSOR_PUSH_KEY` | HTTP push to a Sensor `http-push` source, batched up to 200 records per request |

## Simulator authentication and security

When PostgreSQL is configured, the simulator API is protected by server-side authentication. The
first startup creates the administrator from `SIM_ADMIN_USERNAME` and `SIM_ADMIN_PASSWORD` (at
least 12 characters); `SIM_SECURITY_KEY` needs at least 32 characters.

The application uses HttpOnly SameSite=Strict server-side sessions, CSRF tokens for state-changing
API requests, PBKDF2-HMAC-SHA256 password hashing with per-password random salts, required TOTP MFA
with encrypted-at-rest secrets, one-time recovery codes, administrator/operator/viewer roles,
security response headers, password changes, and session inventory. For production exposure,
terminate TLS in front of the application and set `SIM_COOKIE_SECURE=true`; local Hexa.Build
development can use `false` on loopback HTTP.
