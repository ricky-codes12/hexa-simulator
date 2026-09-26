# Testing

./scripts/verify.sh always runs Go formatting/vet/unit tests and real Svelte check/build. If TEST_DATABASE_URL is present it also applies migrations and runs the PostgreSQL integration test. The generated GitLab and GitHub pipelines supply ephemeral PostgreSQL services so the committed starter is continuously tested across all three layers.
