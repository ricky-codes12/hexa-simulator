# Architecture

## Shape

~~~text
web/                     Svelte 5 + Vite console (Live Map, Fleet and Device panels)
cmd/api/                 Go process: serve, health, version, sensor-onboard
internal/world/          Hexa.Sensor's forestry world: estates, blocks, fences, roads, routing
internal/fleet/          Device profiles (kinds), roster from a composition and seed, itineraries
internal/scenario/       Behaviours and scenario timelines
internal/telemetry/      The record every output encodes (telemetry-v1 shaped)
internal/httpapi/        HTTP API, the fleet runtime (clock, behaviours, output dispatch), export
internal/mqttout/        MQTT 3.1.1 QoS 1 publisher
internal/teltonika/      Teltonika TCP client and Codec 8/8E encoder
internal/sensorpush/     Hexa.Sensor http-push client
internal/postgresstore/  PostgreSQL adapter
db/migrations/           project-owned schema migrations
.hexa/project            Hexa.Build Dev + Runtime lifecycle adapter
~~~

## Invariants

- Browser code talks to the application through /api; it never receives database credentials or
  output secrets.
- PostgreSQL is an external persistent dependency. Runtime release roots remain immutable, and
  migrations are applied explicitly (`scripts/migrate.sh`); the API checks the schema at startup
  and names the migration it misses instead of migrating.
- Hexa.Sensor owns the world. `internal/world` mirrors its seeded world revision (`world.Revision`);
  when Sensor moves its world, this package moves with it.
- The contract with Sensor is the wire: telemetry-v1 JSON, Teltonika Codec 8/8E, and Sensor's
  public admin API for onboarding. Nothing depends on Sensor's internals.
- Canonical verification is ./scripts/verify.sh; CI and Hexa.Build call it rather than duplicating
  test policy.
- /healthz remains cheap and reports database configuration/readiness separately.
- Svelte code uses Svelte 5 conventions. New event handlers use event properties such as
  onclick/onsubmit, not legacy on: directives.

## Fleet runtime

The Go API owns the simulation clock; browsers only observe and steer it. `SimulationRuntime`
(`internal/httpapi/simulation.go`) steps every unit once a second:

1. **Motion.** A unit follows its itinerary (`internal/fleet`): a repeating day of drives along
   paths (with acceleration and braking) and stops, built deterministically from its kind, its
   number and the seed. The itinerary is a pure function of the unit's clock, so a restart resumes
   from the persisted clock and Reset to T0 sets every clock back to its seeded phase.
   Behaviours override it: `park` and `idle` hold the unit; a hand-driven unit (manual or target
   mode) leaves it; a `geofence-exit` or `work` behaviour runs a detour (out of the operating area
   and back, or to a compartment over the haul roads and lane work inside it) while the itinerary
   clock waits; `overspeed` runs the clock faster so the unit covers the distance its speed claims.
2. **Signals.** An electrical model gives vehicle voltage (24 V or 12 V systems, charging with the
   engine on), the tracker's battery (draining during a `power-cut`), GSM by location, GNSS status
   and an odometer from the distance moved.
3. **Reports.** When a unit's report is due (`SIM_REPORT_INTERVAL`, or a `park` heartbeat), the
   runtime builds one record with the attributes the kind's profile lists, applies the sending
   behaviours (`signal-loss` sends nothing, `intermittent` drops a share or holds records for a
   burst) and hands the result to the unit's output only.
4. **Persistence.** The latest state of every unit that reported is written in one statement per
   step, including its itinerary clock.

Behaviours are stored with absolute start and end times (`sim_behaviours`), so a restart keeps
them. A scenario timeline (`internal/scenario`) is a list of offsets, behaviours and targets; the
runtime gives each event's behaviour to its target when its time comes, and the running timeline is
stored in `sim_fleet` with the fleet's T0.

## Outputs

Each device has one output setting: `mqtt`, `teltonika`, `http-push`, `all` (every configured
output, for protocol testing) or `none`. Each configured output has a bounded queue and its own
batching:

| Output | Delivery |
| --- | --- |
| MQTT | One broker connection with username and password, QoS 1 with PUBACK; one message per device report on the topic template (`{imei}/data` by default); a burst as `{"records": [...]}`. Keep-alive pings when idle; a refused login is reported in words |
| Teltonika TCP | One persistent session per IMEI, as each tracker has; Codec 8E (default) or 8; up to 50 records per packet, each packet acknowledged with its count. An IMEI Sensor refuses waits 20 s before it tries again |
| HTTP push | Records of many devices in one `{"records": [...]}` request (up to 200), Bearer ingest key; Sensor's per-record rejections (`unknown_device`) are handed back to their devices |

A slow or failing output only fills its own queue; the clock never waits for it. The console shows
per output how many records were sent, rejected (the device is not registered) and failed.

## Onboarding export

`/api/fleet/sensor-onboarding.zip` (optionally filtered by kind, estate or output) holds
`00_device_types.json` (one Sensor device type per kind and transport, because a Sensor device type
reads one plugin) and Sensor's four CSV imports. Assignments start at a fixed date for seeded units,
so importing again changes nothing. `hexa-simulator sensor-onboard` applies the package through
Sensor's admin API and can create Sensor's three sources.

## Simulator authentication and MFA enrollment

Simulator access uses server-side sessions and requires TOTP enrollment before an authenticated
account can use simulator or administration APIs. Password verification is the first sign-in step.
Accounts with MFA already enabled receive an explicit MFA challenge before a session is created.
Accounts without MFA receive a restricted session that may access only authentication and
MFA-enrollment endpoints until a TOTP secret is verified.

The enrollment UI renders the backend-issued `otpauth://` URI as a local QR code in the browser and
also exposes the setup key as a fallback. The QR image is generated client-side; the TOTP secret is
not sent to any third-party QR service. After successful verification, recovery codes are shown
once and normal simulator access is enabled.
