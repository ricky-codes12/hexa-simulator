-- Fill the database to a deterministic 350-device demo fleet target without
-- deleting or rewriting operator-created devices. Safe to re-run.
WITH deficit AS (
  SELECT GREATEST(350 - count(*), 0)::integer AS needed FROM devices
), candidates AS (
  SELECT
    n,
    '35630704' || lpad((2441000 + n)::text, 7, '0') AS imei
  FROM generate_series(1, 1400) AS n
), available AS (
  SELECT c.n, c.imei
  FROM candidates c
  WHERE NOT EXISTS (SELECT 1 FROM devices d WHERE d.imei = c.imei)
  ORDER BY c.n
  LIMIT (SELECT needed FROM deficit)
)
INSERT INTO devices(name, imei, model, latitude, longitude, heading)
SELECT
  'Truck ' || lpad(n::text, 3, '0'),
  imei,
  'Teltonika FMC920',
  -3.020 + ((n - 1) % 14) * 0.0055,
  104.715 + ((n - 1) % 25) * 0.0068,
  ((n * 47) % 360)::double precision
FROM available
ON CONFLICT (imei) DO NOTHING;
