-- Fleet plan S1-S3: per-device output routing, the fleet composition (kind and estate per
-- device), telemetry attributes, behaviours, and the fleet clock for "reset to T0". Safe to
-- re-run.
ALTER TABLE devices ADD COLUMN IF NOT EXISTS kind TEXT NOT NULL DEFAULT 'haul-truck';
ALTER TABLE devices ADD COLUMN IF NOT EXISTS output TEXT NOT NULL DEFAULT 'all';
ALTER TABLE devices ADD COLUMN IF NOT EXISTS estate TEXT NOT NULL DEFAULT '';
ALTER TABLE devices ADD COLUMN IF NOT EXISTS seeded BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE devices ADD COLUMN IF NOT EXISTS attributes JSONB NOT NULL DEFAULT '{}'::jsonb;
-- The unit's itinerary clock in seconds when it last reported; NULL starts it from its seeded
-- phase.
ALTER TABLE devices ADD COLUMN IF NOT EXISTS plan_clock DOUBLE PRECISION;
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'devices_output_check') THEN
    ALTER TABLE devices ADD CONSTRAINT devices_output_check CHECK (output IN ('mqtt', 'teltonika', 'http-push', 'all', 'none'));
  END IF;
END $$;
CREATE INDEX IF NOT EXISTS devices_kind_idx ON devices(kind);

-- Remove the grid fleet 003 used to generate: its IMEIs collide with Hexa.Sensor's simulator
-- fleet. Only rows matching that generator exactly are removed; operator-created devices stay.
DELETE FROM devices
WHERE seeded = false
  AND imei ~ '^35630704244[0-9]{4}$'
  AND name ~ '^Truck [0-9]{3}$'
  AND model = 'Teltonika FMC920';

-- Behaviours given to a unit by hand or by a scenario timeline, active from starts_at until
-- ends_at.
CREATE TABLE IF NOT EXISTS sim_behaviours (
  id BIGSERIAL PRIMARY KEY,
  device_id BIGINT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
  type TEXT NOT NULL,
  params JSONB NOT NULL DEFAULT '{}'::jsonb,
  starts_at TIMESTAMPTZ NOT NULL,
  ends_at TIMESTAMPTZ NOT NULL,
  source TEXT NOT NULL DEFAULT 'manual',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK (ends_at >= starts_at)
);
CREATE INDEX IF NOT EXISTS sim_behaviours_ends_at_idx ON sim_behaviours(ends_at);

-- One row: when the fleet's T0 was, and the scenario timeline that is running.
CREATE TABLE IF NOT EXISTS sim_fleet (
  id SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
  epoch TIMESTAMPTZ NOT NULL DEFAULT now(),
  scenario TEXT NOT NULL DEFAULT '',
  scenario_started_at TIMESTAMPTZ,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO sim_fleet(id) VALUES (1) ON CONFLICT (id) DO NOTHING;
