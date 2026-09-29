# Demo fleet: running Hexa.Simulator against Hexa.Sensor

- Status: implemented on branch `demo-fleet/sensor-ingress` (2026-09-30), proven end to end
  against a Hexa.Sensor Dev plane: 350 units in Sensor's Live Map over three sources, nothing left
  waiting to be registered, an overspeed behaviour raising Sensor's overspeed alarm.
- Source of the plan: the demo fleet plan (S1–S7, §8) in the Hexa workspace,
  `docs/2026-09-29-hexa_simulator_demo_fleet/PLAN.md`.

## 1. The fleet

| Kind | Code | Units (default 350) | Day | Sensor asset type | Output |
| --- | --- | --- | --- | --- | --- |
| Haul truck | HT | 158 | Mill to a loading point on an estate spine or the log landing and back, 44–54 km/h empty, 30–40 loaded | `truck` | MQTT, every 9th HTTP push |
| Farm tractor | TR | 70 | Lanes 6 m apart across a planting block, 6–10 km/h with `din1` on, headland turns with it off. TR-004 waits beside compartment C07 | `tractor` | Teltonika TCP |
| Dozer | DZ | 18 | Pushes and reverses around a spot deep inside Sialang, long idles | `heavy` | Teltonika TCP |
| Excavator | EX | 18 | Small moves at a loading point, long stretches loading with the engine on | `excavator` | Teltonika TCP |
| Pickup | PU | 52 | Estate office (mill, nursery or log landing) to two blocks and back | `pickup` | MQTT |
| Water truck | WT | 17 | Rounds of the estate haul roads | `truck` | HTTP push |
| Fuel bowser | FB | 17 | Rounds of the loaders, or of the dozers in Sialang | `truck` | HTTP push |

- IMEIs are `3598150` + the kind's digit + the unit number + a Luhn digit (HT-012 is
  `359815010000121`). Hexa.Sensor's own simulator fleet uses `3563070424…`, so both fleets share
  the estates without colliding.
- Every unit's whole day stays inside the operating area (geofence OPS), out of the conservation
  zone, heavy equipment inside the concession, and trucks under the 60 km/h limit: the baseline
  raises no alarm (`internal/fleet/fleet_test.go` checks this for all 350).
- A unit's itinerary clock is stored with its last report, so a restart continues where the fleet
  was. **Reset to T0** starts every unit at its seeded point again.

## 2. Wire Hexa.Sensor

Sensor needs three sources, one device type per kind and transport, and the registrations. The
simulator's `sensor-onboard` command does all of it through Sensor's public admin API, and running
it again changes nothing:

~~~bash
SENSOR_PASSWORD=... SENSOR_MQTT_PASSWORD=<broker password of hexa-sensor> \
DATABASE_URL=<the simulator's database> \
  hexa-simulator sensor-onboard --sensor http://127.0.0.1:19190 --tenant smf --user admin \
    --code 123456 --sources --mqtt-broker mqtt://127.0.0.1:19300 --tcp-listen :19192 \
    --push-key-file ./push.env
~~~

(`hexa-simulator` is the API binary, `bin/api` in development.) It creates:

| Source key | Name in Sensor | Plugin | Settings |
| --- | --- | --- | --- |
| `estate-broker` | Estate broker | `mqtt-subscribe` | the broker, topic `+/data`, user `hexa-sensor`, hardware ID from topic level 0 |
| `fmc-direct` | FMC trackers direct | `teltonika-tcp` | `listen` from `--tcp-listen` |
| `vendor-cloud` | Vendor cloud push | `http-push` | a new ingest key, written with its URL to `--push-key-file` (mode 0600) |

then the device types (`haul-truck-mqtt`, `farm-tractor-tcp`, `water-truck-http`, …) and the CSV
imports of estates, devices, assets and assignments. Names Sensor shows carry no demo wording
(Sensor ADR 0035); the `source=hexa-simulator` labels stay as identifiers.

Without the command: download **Export for Hexa.Sensor** from the Fleet panel, create each object
of `00_device_types.json` with `POST /api/v1/admin/device-profiles`, then import the four CSV files
in order under Sensor's **Imports**.

Sensor-side prerequisites:
- `HEXA_SENSOR_OUTBOUND_KEY` on Sensor, so the MQTT source can store the broker password.
- A broker that is not on loopback must be in the ingest node's `HEXA_SENSOR_MQTT_BROKER_ALLOW`.
- `teltonika-tcp` allows 200 connections per source address by default. The default fleet opens
  106 sessions from one host; raise `max_connections_per_ip` for more than 200 TCP units.
- Generic plugins (`mqtt-subscribe`, `http-push`) accept only the fields their manifests declare,
  so `power_cut`, `gsm_signal`, `gnss_status` and `din1` arrive there as `x.power_cut` and so on.
  The Teltonika device types map the FMC IO elements to Sensor's canonical keys.

Then point the simulator at the sources: `SIM_MQTT_URL` with user `hexa-simulator`,
`TELTONIKA_GATEWAY_ADDR` at the TCP listener, and the two lines of the push key file.

## 3. Run the demo

1. **Fleet → Reset to T0**, so the run plays like the rehearsal.
2. **Fleet → Timeline → Start** `demo-30min`. The header shows `T+m:ss`; the panel shows the next
   event and its countdown.

| At | Event | Sensor shows |
| --- | --- | --- |
| T+2 | HT-012 drives 82 km/h for 90 s | "Log truck overspeed" (proven) |
| T+5 | TR-004 drives into compartment C07 and works it for 20 min | coverage, if C07's work order lists TR-004's asset |
| T+8 | DZ-002 loses vehicle power for 10 min | power-cut alarm once HS-006b ships; `x.power_cut`/0 V until then |
| T+10 | WT-003 idles with the engine on for 12 min | idle alarm once HS-006b ships |
| T+12 | Five Kenanga pickups send nothing for 6 min | late, then offline |
| T+15 | HT-031 leaves the operating area and comes back | "Left the operating area" |
| T+18 | The five pickups report again | back to reporting |
| T+20 | FB-002 drops 60% of its records for 5 min | gaps, late |
| T+22 | EX-005 reports in 60 s bursts for 5 min | records arrive in batches |
| T+25 | PU-010 parks, a heartbeat every 5 min | parked, then stale |

Any behaviour can also be given by hand: to one unit from its **Device** panel, or to a group
(kind, estate, output) from **Fleet → Apply to a group**, where a group can also be routed to
another output.

## 4. On bn2 (plan S7)

1. Merge, then apply the migrations to the Runtime database: `DATABASE_URL=... ./scripts/migrate.sh`
   (004 adds the fleet columns; the API refuses to start without them). The old 350-truck grid is
   removed by 004 and the new fleet is seeded on the next start.
2. In the Runtime `runtime.env`:

   | Setting | Value |
   | --- | --- |
   | `SIM_MQTT_URL` | `mqtt://127.0.0.1:19300` (the demo kit broker) |
   | `SIM_MQTT_USERNAME`, `SIM_MQTT_PASSWORD` | `hexa-simulator` and its password from `bin/kit --plane runtime info` |
   | `TELTONIKA_GATEWAY_ADDR` | `127.0.0.1:19192` (Sensor Runtime TCP ingress) |
   | `SIM_SENSOR_PUSH_URL`, `SIM_SENSOR_PUSH_KEY` | from `sensor-onboard --push-key-file` |

3. Run `sensor-onboard --sources` against Sensor Runtime (19190) as in §2.
4. How bn2 gets new releases (bn2 Agent over GitHub, or manual reconcile) is still the PO's and
   Ricky's call.

## 5. Plan status

| Item | State |
| --- | --- |
| S1 per-device output routing | Done: device and group routing, `all` and `none` |
| S2 believable fleet on Sensor's world | Done: seven kinds, deterministic from a seed, IMEIs clear of Sensor's fleet |
| S3 behaviours and timeline | Done: eight behaviours, `demo-30min`, countdown, restart |
| S4 fields for rules | Done: telemetry-v1 attributes and FMC IO elements from the profile's fields |
| S5 fleet onboarding export | Done: device types plus the four CSVs, `sensor-onboard` |
| S6 fleet controls | Done: group behaviours and routing, start and stop, reset to T0, counters |
| S7 bn2 | Configuration documented in §4; the deployment itself is not done |
| §8 any IoT device | Not started. Kinds are the seed of device profiles; behaviours act on records, not on GPS |

## 6. Known limits

- Buffered replay after a signal loss is out of scope (plan §0); a silent unit's records are lost.
- The `work` behaviour drives to the compartment over the haul roads and works it lane by lane;
  it does not add the unit to a Sensor work order.
- Sensor's HS-006b (offline, idle, power cut alarms) is not built yet; those behaviours already send
  what it will read.
