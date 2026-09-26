# Testing

./scripts/verify.sh always runs Go formatting/vet/unit tests and real Svelte check/build. If TEST_DATABASE_URL is present it also applies migrations and runs the PostgreSQL integration test. The generated GitLab and GitHub pipelines supply ephemeral PostgreSQL services so the committed starter is continuously tested across all three layers.


Teltonika protocol unit tests use a loopback TCP listener and require no external Gateway. They verify IMEI framing, Codec 8 Extended packet shape/CRC, handshake acceptance, and AVL record acknowledgement. End-to-end Gateway qualification still requires the actual target Gateway host/port supplied through external configuration.


Gateway unit tests run entirely on loopback: the existing Teltonika client connects to the new TCP listener, performs the IMEI/Codec 8E exchange, and the Gateway forwards to an `httptest` HEXA.SENSOR endpoint. Tests assert normalized fields and the `X-SECRET-KEY` header without embedding any real credential. Decoder tests also reject CRC corruption. External HEXA.SENSOR qualification remains a deployment/integration step using protected configuration.
