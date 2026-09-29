package postgresstore

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/lib/pq"
	"hexa-simulator/internal/httpapi"
)

type Store struct{ db *sql.DB }

func Open(ctx context.Context, databaseURL string) (*Store, error) {
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open PostgreSQL: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping PostgreSQL: %w", err)
	}
	return &Store{db: db}, nil
}
func (s *Store) Close() error { return s.db.Close() }

// EnsureDemoFleet fills the database up to target devices without deleting or
// rewriting operator-created rows. Seed candidates are deterministic and
// conflict-safe by IMEI, so repeated startup calls are idempotent.
func (s *Store) EnsureDemoFleet(ctx context.Context, target int) (int, error) {
	if target <= 0 {
		return 0, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	// Serialize fleet provisioning across overlapping runtime starts.
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(35630704)`); err != nil {
		return 0, err
	}
	var current int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM devices`).Scan(&current); err != nil {
		return 0, err
	}
	if current >= target {
		if err := tx.Commit(); err != nil {
			return 0, err
		}
		return current, nil
	}
	needed := target - current
	result, err := tx.ExecContext(ctx, `
WITH candidates AS (
  SELECT
    n,
    '35630704' || lpad((2441000 + n)::text, 7, '0') AS imei
  FROM generate_series(1, 1400) AS n
), available AS (
  SELECT n, imei
  FROM candidates c
  WHERE NOT EXISTS (SELECT 1 FROM devices d WHERE d.imei = c.imei)
  ORDER BY n
  LIMIT $1
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
ON CONFLICT (imei) DO NOTHING`, needed)
	if err != nil {
		return 0, err
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if int(inserted) != needed {
		return 0, fmt.Errorf("demo fleet provisioning inserted %d of %d required devices", inserted, needed)
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return current + int(inserted), nil
}
func (s *Store) Ping(ctx context.Context) error {
	var ready bool
	if err := s.db.QueryRowContext(ctx, "SELECT to_regclass('public.devices') IS NOT NULL").Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return fmt.Errorf("required table devices is not migrated")
	}
	return nil
}
func (s *Store) GetDevice(ctx context.Context, id int64) (httpapi.Device, error) {
	var d httpapi.Device
	err := s.db.QueryRowContext(ctx, `SELECT id,name,imei,model,status,latitude,longitude,speed,heading,ignition,updated_at,created_at FROM devices WHERE id=$1`, id).Scan(&d.ID, &d.Name, &d.IMEI, &d.Model, &d.Status, &d.Latitude, &d.Longitude, &d.Speed, &d.Heading, &d.Ignition, &d.UpdatedAt, &d.CreatedAt)
	return d, err
}

func (s *Store) ListDevices(ctx context.Context) ([]httpapi.Device, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,imei,model,status,latitude,longitude,speed,heading,ignition,updated_at,created_at FROM devices ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []httpapi.Device{}
	for rows.Next() {
		var d httpapi.Device
		if err := rows.Scan(&d.ID, &d.Name, &d.IMEI, &d.Model, &d.Status, &d.Latitude, &d.Longitude, &d.Speed, &d.Heading, &d.Ignition, &d.UpdatedAt, &d.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, d)
	}
	return items, rows.Err()
}

func (s *Store) SearchDevices(ctx context.Context, query string, limit, offset int) ([]httpapi.Device, int, error) {
	pattern := "%" + query + "%"
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM devices WHERE $1='' OR name ILIKE $2 OR imei ILIKE $2 OR model ILIKE $2`, query, pattern).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,imei,model,status,latitude,longitude,speed,heading,ignition,updated_at,created_at FROM devices WHERE $1='' OR name ILIKE $2 OR imei ILIKE $2 OR model ILIKE $2 ORDER BY updated_at DESC,id LIMIT $3 OFFSET $4`, query, pattern, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []httpapi.Device{}
	for rows.Next() {
		var d httpapi.Device
		if err := rows.Scan(&d.ID, &d.Name, &d.IMEI, &d.Model, &d.Status, &d.Latitude, &d.Longitude, &d.Speed, &d.Heading, &d.Ignition, &d.UpdatedAt, &d.CreatedAt); err != nil {
			return nil, 0, err
		}
		items = append(items, d)
	}
	return items, total, rows.Err()
}
func (s *Store) CreateDevice(ctx context.Context, input httpapi.DeviceInput) (httpapi.Device, error) {
	var d httpapi.Device
	err := s.db.QueryRowContext(ctx, `INSERT INTO devices(name,imei,model) VALUES($1,$2,$3) RETURNING id,name,imei,model,status,latitude,longitude,speed,heading,ignition,updated_at,created_at`, input.Name, input.IMEI, input.Model).Scan(&d.ID, &d.Name, &d.IMEI, &d.Model, &d.Status, &d.Latitude, &d.Longitude, &d.Speed, &d.Heading, &d.Ignition, &d.UpdatedAt, &d.CreatedAt)
	return d, err
}
func (s *Store) UpdateTelemetry(ctx context.Context, id int64, input httpapi.TelemetryInput) (httpapi.Device, error) {
	var d httpapi.Device
	err := s.db.QueryRowContext(ctx, `UPDATE devices SET status=$2,latitude=$3,longitude=$4,speed=$5,heading=$6,ignition=$7,updated_at=now() WHERE id=$1 RETURNING id,name,imei,model,status,latitude,longitude,speed,heading,ignition,updated_at,created_at`, id, input.Status, input.Latitude, input.Longitude, input.Speed, input.Heading, input.Ignition).Scan(&d.ID, &d.Name, &d.IMEI, &d.Model, &d.Status, &d.Latitude, &d.Longitude, &d.Speed, &d.Heading, &d.Ignition, &d.UpdatedAt, &d.CreatedAt)
	return d, err
}

func (s *Store) DeleteDevice(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM devices WHERE id=$1`, id)
	return err
}

func (s *Store) EnsureAdmin(ctx context.Context, email, displayName, passwordHash string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO simulator_users(email,display_name,role,password_hash) VALUES($1,$2,'administrator',$3) ON CONFLICT(email) DO NOTHING`, email, displayName, passwordHash)
	return err
}
func scanUser(row interface{ Scan(...any) error }) (httpapi.User, error) {
	var u httpapi.User
	err := row.Scan(&u.ID, &u.Email, &u.DisplayName, &u.Role, &u.PasswordHash, &u.MFASecret, &u.MFAEnabled)
	return u, err
}
func (s *Store) UserByEmail(ctx context.Context, email string) (httpapi.User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT id,email,display_name,role,password_hash,mfa_secret,mfa_enabled FROM simulator_users WHERE email=$1`, email))
}
func (s *Store) UserByID(ctx context.Context, id int64) (httpapi.User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT id,email,display_name,role,password_hash,mfa_secret,mfa_enabled FROM simulator_users WHERE id=$1`, id))
}
func (s *Store) ListUsers(ctx context.Context) ([]httpapi.User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,email,display_name,role,password_hash,mfa_secret,mfa_enabled FROM simulator_users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []httpapi.User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}
func (s *Store) CreateUser(ctx context.Context, email, name, role, passwordHash string) (httpapi.User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `INSERT INTO simulator_users(email,display_name,role,password_hash) VALUES($1,$2,$3,$4) RETURNING id,email,display_name,role,password_hash,mfa_secret,mfa_enabled`, email, name, role, passwordHash))
}
func (s *Store) UpdatePassword(ctx context.Context, id int64, h string) error {
	_, e := s.db.ExecContext(ctx, `UPDATE simulator_users SET password_hash=$2,updated_at=now() WHERE id=$1`, id, h)
	return e
}
func (s *Store) UpdateMFA(ctx context.Context, id int64, secret string, enabled bool) error {
	_, e := s.db.ExecContext(ctx, `UPDATE simulator_users SET mfa_secret=$2,mfa_enabled=$3,updated_at=now() WHERE id=$1`, id, secret, enabled)
	return e
}
func (s *Store) CreateSession(ctx context.Context, x httpapi.Session) error {
	_, e := s.db.ExecContext(ctx, `INSERT INTO simulator_sessions(token_hash,user_id,csrf_token,user_agent,ip_address,created_at,last_seen_at,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, x.TokenHash, x.UserID, x.CSRFToken, x.UserAgent, x.IPAddress, x.CreatedAt, x.LastSeenAt, x.ExpiresAt)
	return e
}
func scanSession(row interface{ Scan(...any) error }) (httpapi.Session, error) {
	var x httpapi.Session
	e := row.Scan(&x.TokenHash, &x.UserID, &x.CSRFToken, &x.UserAgent, &x.IPAddress, &x.CreatedAt, &x.LastSeenAt, &x.ExpiresAt)
	return x, e
}
func (s *Store) SessionByHash(ctx context.Context, h string) (httpapi.Session, error) {
	return scanSession(s.db.QueryRowContext(ctx, `SELECT token_hash,user_id,csrf_token,user_agent,ip_address,created_at,last_seen_at,expires_at FROM simulator_sessions WHERE token_hash=$1`, h))
}
func (s *Store) TouchSession(ctx context.Context, h string) error {
	_, e := s.db.ExecContext(ctx, `UPDATE simulator_sessions SET last_seen_at=now() WHERE token_hash=$1`, h)
	return e
}
func (s *Store) DeleteSession(ctx context.Context, h string) error {
	_, e := s.db.ExecContext(ctx, `DELETE FROM simulator_sessions WHERE token_hash=$1`, h)
	return e
}
func (s *Store) DeleteUserSessions(ctx context.Context, id int64, except string) error {
	_, e := s.db.ExecContext(ctx, `DELETE FROM simulator_sessions WHERE user_id=$1 AND token_hash<>$2`, id, except)
	return e
}
func (s *Store) ListSessions(ctx context.Context, id int64) ([]httpapi.Session, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT token_hash,user_id,csrf_token,user_agent,ip_address,created_at,last_seen_at,expires_at FROM simulator_sessions WHERE user_id=$1 ORDER BY last_seen_at DESC`, id)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []httpapi.Session
	for rows.Next() {
		x, e := scanSession(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
func (s *Store) ReplaceRecoveryCodes(ctx context.Context, id int64, codes []string) error {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `DELETE FROM simulator_recovery_codes WHERE user_id=$1`, id); e != nil {
		return e
	}
	for _, c := range codes {
		if _, e = tx.ExecContext(ctx, `INSERT INTO simulator_recovery_codes(user_id,code_hash) VALUES($1,$2)`, id, c); e != nil {
			return e
		}
	}
	return tx.Commit()
}
func (s *Store) UseRecoveryCode(ctx context.Context, id int64, h string) (bool, error) {
	res, e := s.db.ExecContext(ctx, `UPDATE simulator_recovery_codes SET used_at=now() WHERE user_id=$1 AND code_hash=$2 AND used_at IS NULL`, id, h)
	if e != nil {
		return false, e
	}
	n, e := res.RowsAffected()
	return n == 1, e
}
