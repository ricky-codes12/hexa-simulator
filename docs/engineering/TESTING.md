# Testing

./scripts/verify.sh always runs Go formatting/vet/unit tests and real Svelte check/build. If
TEST_DATABASE_URL is present it also applies migrations and runs the PostgreSQL integration tests.
The generated GitLab and GitHub pipelines supply ephemeral PostgreSQL services so the committed
starter is continuously tested across all three layers.

## What the unit tests prove

| Package | Proves |
| --- | --- |
| `internal/world` | Planting blocks match Sensor's grid; routes stay on haul roads inside the operating area; an exit point is outside it |
| `internal/fleet` | The default composition is 350 with the plan's shares; IMEIs are unique, Luhn-valid and clear of Sensor's fleet; every seeded unit's whole day raises no alarm by itself; itineraries are deterministic; tractors work lanes with the implement down |
| `internal/scenario` | Timelines are ordered and valid; settings are checked; targets pick units by name, kind, estate and limit |
| `internal/teltonika` | Codec 8 and 8E round trips with the FMC IO elements, Teltonika's documented example packet, multi-record packets and their acknowledgements, the backoff after a refused IMEI |
| `internal/mqttout` | CONNECT with username and password (from settings or the URL), telemetry-v1 payloads, bursts as `{"records": [...]}`, a refused password named |
| `internal/sensorpush` | Batched telemetry-v1 requests, Sensor's per-record rejections read back, non-2xx and timeouts as errors |
| `internal/httpapi` | Each device sends only to its output; the clock runs without a browser and past a blocked output; every behaviour changes what is sent as specified; geofence exit leaves and returns; compartment work stays inside C07; the timeline fires on time and Reset to T0 clears it; rejections show on the device; a restart resumes the itinerary clock; the export's device types and CSV rows; the fleet endpoints |

The runtime tests step the fleet on a fake clock, so they are fast and deterministic.

## Qualification against Hexa.Sensor

Unit tests cannot prove the external paths. With a Sensor wired as in docs/DEMO_FLEET.md §2, check:

1. Sensor's three sources are healthy and their counters rise; its Live Map shows the units split
   over MQTT, Teltonika TCP and HTTP push as in the simulator's Fleet panel.
2. Nothing waits to be registered in Sensor after the onboarding.
3. A behaviour raises its Sensor reaction, for example an overspeed on a haul truck raises
   "Log truck overspeed".
4. With the browser closed, Sensor keeps receiving records.
