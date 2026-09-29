-- Add a deterministic 350-device demo fleet without disturbing operator-created devices.
-- IMEIs are valid 15-digit identifiers and the statement is safe to re-run.
INSERT INTO devices(name, imei, model, latitude, longitude, heading)
SELECT
  'Truck ' || lpad(n::text, 3, '0'),
  '35630704' || lpad((2441000 + n)::text, 7, '0'),
  'Teltonika FMC920',
  -3.020 + ((n - 1) % 14) * 0.0055,
  104.715 + ((n - 1) % 25) * 0.0068,
  ((n * 47) % 360)::double precision
FROM generate_series(1, 350) AS n
ON CONFLICT (imei) DO NOTHING;
