# Testing

./scripts/verify.sh always runs Go formatting/vet/unit tests and real Svelte check/build. If TEST_DATABASE_URL is present it also applies migrations and runs the PostgreSQL integration test. The generated GitLab and GitHub pipelines supply ephemeral PostgreSQL services so the committed starter is continuously tested across all three layers.


Teltonika protocol unit tests use a loopback TCP listener and require no external Gateway. They verify IMEI framing, Codec 8 Extended packet shape/CRC, handshake acceptance, and AVL record acknowledgement. End-to-end Gateway qualification still requires the actual target Gateway host/port supplied through external configuration.
