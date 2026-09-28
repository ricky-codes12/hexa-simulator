# Testing

./scripts/verify.sh always runs Go formatting/vet/unit tests and real Svelte check/build. If TEST_DATABASE_URL is present it also applies migrations and runs the PostgreSQL integration test. The generated GitLab and GitHub pipelines supply ephemeral PostgreSQL services so the committed starter is continuously tested across all three layers.


Teltonika protocol unit tests use a loopback TCP listener and require no external Gateway. They verify IMEI framing, Codec 8 Extended packet shape/CRC, handshake acceptance, and AVL record acknowledgement. End-to-end Gateway qualification still requires the actual target Gateway host/port supplied through external configuration.


Gateway unit tests run entirely on loopback: the existing Teltonika client connects to the new TCP listener, performs the IMEI/Codec 8E exchange, and the Gateway forwards to an `httptest` HEXA.SENSOR endpoint. Tests assert normalized fields and the `X-SECRET-KEY` header without embedding any real credential. Decoder tests also reject CRC corruption. External HEXA.SENSOR qualification remains a deployment/integration step using protected configuration.


## Local UI/API and Hexa.Sensor integration checks

For `.hexa/project dev`, treat `127.0.0.1:5173` (web) and `127.0.0.1:8080` (API) as one supervised development instance. A create/delete observed through the web UI must be verified against `http://127.0.0.1:8080/api/devices`; do not use an unrelated listener as evidence for the supervised instance.

HTTP Push qualification requires protected `SIM_SENSOR_PUSH_URL` and `SIM_SENSOR_PUSH_KEY` values (with optional `SIM_SENSOR_PUSH_TIMEOUT`). The project contract explicitly permits those variables to reach the adapter. Never print the key during qualification. Verify the Sensor readiness endpoint separately, then verify that fresh simulator telemetry advances the device timestamp and is accepted by the configured Sensor connector.
