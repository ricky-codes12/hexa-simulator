package postgresstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/lib/pq"
	"hexa-simulator/internal/httpapi"
)

type Store struct{ db *sql.DB }

func Open(ctx context.Context, databaseURL string) (*Store, error) {
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open PostgreSQL: %w", err)
	}
	// The simulator runs hundreds of device loops concurrently. Keep their
	// database work inside a bounded pool so one instance cannot exhaust the
	// PostgreSQL server and lock authentication or health checks out.
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(10)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping PostgreSQL: %w", err)
	}
	return &Store{db: db}, nil
}
func (s *Store) Close() error { return s.db.Close() }

const deviceColumns = `id,name,imei,model,kind,output,estate,seeded,status,latitude,longitude,speed,heading,ignition,attributes,plan_clock,updated_at,created_at`

func scanDevice(row interface{ Scan(...any) error }) (httpapi.Device, error) {
	var d httpapi.Device
	var attrs []byte
	var clock sql.NullFloat64
	err := row.Scan(&d.ID, &d.Name, &d.IMEI, &d.Model, &d.Kind, &d.Output, &d.Estate, &d.Seeded, &d.Status, &d.Latitude, &d.Longitude, &d.Speed, &d.Heading, &d.Ignition, &attrs, &clock, &d.UpdatedAt, &d.CreatedAt)
	if err != nil {
		return d, err
	}
	d.Attributes = map[string]any{}
	if len(attrs) > 0 {
		_ = json.Unmarshal(attrs, &d.Attributes)
	}
	if clock.Valid {
		v := clock.Float64
		d.PlanClock = &v
	}
	return d, nil
}

func scanDevices(rows *sql.Rows) ([]httpapi.Device, error) {
	defer rows.Close()
	items := []httpapi.Device{}
	for rows.Next() {
		d, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, d)
	}
	return items, rows.Err()
}

// CheckSchema fails when the database is behind this build, naming the migration to apply.
// Migrations stay an explicit operation (scripts/migrate.sh), never a side effect of startup.
func (s *Store) CheckSchema(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `SELECT `+deviceColumns+` FROM devices LIMIT 0`); err != nil {
		return fmt.Errorf("the devices table is behind this build (%v): apply db/migrations with scripts/migrate.sh", err)
	}
	if _, err := s.db.ExecContext(ctx, `SELECT id, epoch, scenario, scenario_started_at FROM sim_fleet LIMIT 0`); err != nil {
		return fmt.Errorf("sim_fleet is missing (%v): apply db/migrations/004_fleet.sql with scripts/migrate.sh", err)
	}
	return nil
}

// EnsureFleet makes the seeded fleet match the roster: it adds missing units, removes seeded
// units the roster no longer has, and leaves operator-created devices and operator changes to
// seeded units (such as their output) alone. It returns the number of devices afterwards.
func (s *Store) EnsureFleet(ctx context.Context, roster []httpapi.SeedDevice) (total, added, removed int, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, 0, err
	}
	defer tx.Rollback()
	// Serialize provisioning across overlapping starts.
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(35981500)`); err != nil {
		return 0, 0, 0, err
	}
	imeis := make([]string, len(roster))
	for i, u := range roster {
		imeis[i] = u.IMEI
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM devices WHERE seeded AND NOT (imei = ANY($1))`, pq.Array(imeis))
	if err != nil {
		return 0, 0, 0, err
	}
	n, _ := res.RowsAffected()
	removed = int(n)
	for _, u := range roster {
		res, err := tx.ExecContext(ctx, `INSERT INTO devices(name,imei,model,kind,output,estate,seeded,status) VALUES($1,$2,$3,$4,$5,$6,true,'offline') ON CONFLICT (imei) DO NOTHING`,
			u.Name, u.IMEI, u.Model, u.Kind, u.Output, u.Estate)
		if err != nil {
			return 0, 0, 0, fmt.Errorf("seed %s: %w", u.Name, err)
		}
		n, _ := res.RowsAffected()
		added += int(n)
	}
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM devices`).Scan(&total); err != nil {
		return 0, 0, 0, err
	}
	return total, added, removed, tx.Commit()
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
	return scanDevice(s.db.QueryRowContext(ctx, `SELECT `+deviceColumns+` FROM devices WHERE id=$1`, id))
}

func (s *Store) ListDevices(ctx context.Context) ([]httpapi.Device, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+deviceColumns+` FROM devices ORDER BY id`)
	if err != nil {
		return nil, err
	}
	return scanDevices(rows)
}

func (s *Store) SearchDevices(ctx context.Context, query string, limit, offset int) ([]httpapi.Device, int, error) {
	pattern := "%" + query + "%"
	const where = `WHERE $1='' OR name ILIKE $2 OR imei ILIKE $2 OR model ILIKE $2 OR kind ILIKE $2 OR estate ILIKE $2 OR output ILIKE $2`
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM devices `+where, query, pattern).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+deviceColumns+` FROM devices `+where+` ORDER BY name, id LIMIT $3 OFFSET $4`, query, pattern, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	items, err := scanDevices(rows)
	return items, total, err
}

func (s *Store) CreateDevice(ctx context.Context, input httpapi.DeviceInput) (httpapi.Device, error) {
	return scanDevice(s.db.QueryRowContext(ctx, `INSERT INTO devices(name,imei,model,kind,output,estate) VALUES($1,$2,$3,$4,$5,$6) RETURNING `+deviceColumns,
		input.Name, input.IMEI, input.Model, input.Kind, input.Output, input.Estate))
}

func (s *Store) UpdateDevice(ctx context.Context, id int64, p httpapi.DevicePatch) (httpapi.Device, error) {
	return scanDevice(s.db.QueryRowContext(ctx, `UPDATE devices SET name=COALESCE($2,name), output=COALESCE($3,output), kind=COALESCE($4,kind), estate=COALESCE($5,estate)
		WHERE id=$1 RETURNING `+deviceColumns, id, p.Name, p.Output, p.Kind, p.Estate))
}

func (s *Store) SetOutput(ctx context.Context, ids []int64, output string) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	res, err := s.db.ExecContext(ctx, `UPDATE devices SET output=$2 WHERE id = ANY($1)`, pq.Array(ids), output)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

func (s *Store) UpdateTelemetry(ctx context.Context, id int64, input httpapi.TelemetryInput) (httpapi.Device, error) {
	return scanDevice(s.db.QueryRowContext(ctx, `UPDATE devices SET status=$2,latitude=$3,longitude=$4,speed=$5,heading=$6,ignition=$7,updated_at=now() WHERE id=$1 RETURNING `+deviceColumns,
		id, input.Status, input.Latitude, input.Longitude, input.Speed, input.Heading, input.Ignition))
}

// PersistTelemetry writes the latest state of many units in one statement.
func (s *Store) PersistTelemetry(ctx context.Context, updates []httpapi.TelemetryUpdate) error {
	if len(updates) == 0 {
		return nil
	}
	n := len(updates)
	ids, statuses := make([]int64, n), make([]string, n)
	lats, lons, speeds, headings := make([]float64, n), make([]float64, n), make([]float64, n), make([]float64, n)
	ignitions := make([]bool, n)
	attrs := make([]string, n)
	clocks := make([]sql.NullFloat64, n)
	times := make([]string, n)
	for i, u := range updates {
		ids[i], statuses[i], lats[i], lons[i], speeds[i], headings[i], ignitions[i] = u.ID, u.Status, u.Latitude, u.Longitude, u.Speed, u.Heading, u.Ignition
		b, err := json.Marshal(u.Attributes)
		if err != nil || u.Attributes == nil {
			b = []byte("{}")
		}
		attrs[i] = string(b)
		if u.PlanClock != nil {
			clocks[i] = sql.NullFloat64{Float64: *u.PlanClock, Valid: true}
		}
		times[i] = u.At.UTC().Format(time.RFC3339Nano)
	}
	_, err := s.db.ExecContext(ctx, `UPDATE devices d SET status=u.status, latitude=u.lat, longitude=u.lon, speed=u.speed, heading=u.heading, ignition=u.ignition,
		attributes=u.attrs::jsonb, plan_clock=u.clock, updated_at=u.at
		FROM unnest($1::bigint[], $2::text[], $3::float8[], $4::float8[], $5::float8[], $6::float8[], $7::bool[], $8::text[], $9::float8[], $10::timestamptz[])
		AS u(id, status, lat, lon, speed, heading, ignition, attrs, clock, at) WHERE d.id = u.id`,
		pq.Array(ids), pq.Array(statuses), pq.Array(lats), pq.Array(lons), pq.Array(speeds), pq.Array(headings), pq.Array(ignitions), pq.Array(attrs), pq.Array(clocks), pq.Array(times))
	return err
}

func (s *Store) DeleteDevice(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM devices WHERE id=$1`, id)
	return err
}

// ---- behaviours and the fleet clock ----------------------------------------------------------

func (s *Store) CreateBehaviours(ctx context.Context, bs []httpapi.Behaviour) ([]httpapi.Behaviour, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	out := make([]httpapi.Behaviour, 0, len(bs))
	for _, b := range bs {
		params, err := json.Marshal(b.Params)
		if err != nil {
			return nil, err
		}
		if err := tx.QueryRowContext(ctx, `INSERT INTO sim_behaviours(device_id,type,params,starts_at,ends_at,source) VALUES($1,$2,$3::jsonb,$4,$5,$6) RETURNING id`,
			b.DeviceID, b.Type, string(params), b.StartsAt, b.EndsAt, b.Source).Scan(&b.ID); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, tx.Commit()
}

func (s *Store) ActiveBehaviours(ctx context.Context, at time.Time) ([]httpapi.Behaviour, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,device_id,type,params,starts_at,ends_at,source FROM sim_behaviours WHERE ends_at > $1 ORDER BY starts_at, id`, at)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []httpapi.Behaviour
	for rows.Next() {
		var b httpapi.Behaviour
		var params []byte
		if err := rows.Scan(&b.ID, &b.DeviceID, &b.Type, &params, &b.StartsAt, &b.EndsAt, &b.Source); err != nil {
			return nil, err
		}
		b.Params = map[string]any{}
		_ = json.Unmarshal(params, &b.Params)
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *Store) DeleteBehaviours(ctx context.Context, f httpapi.BehaviourFilter) (int, error) {
	var res sql.Result
	var err error
	switch {
	case f.All:
		res, err = s.db.ExecContext(ctx, `DELETE FROM sim_behaviours`)
	case f.Source != "":
		res, err = s.db.ExecContext(ctx, `DELETE FROM sim_behaviours WHERE source=$1`, f.Source)
	case len(f.DeviceIDs) > 0:
		res, err = s.db.ExecContext(ctx, `DELETE FROM sim_behaviours WHERE device_id = ANY($1)`, pq.Array(f.DeviceIDs))
	default:
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

func (s *Store) FleetState(ctx context.Context) (httpapi.FleetState, error) {
	var st httpapi.FleetState
	var started sql.NullTime
	err := s.db.QueryRowContext(ctx, `SELECT epoch, scenario, scenario_started_at FROM sim_fleet WHERE id=1`).Scan(&st.Epoch, &st.Scenario, &started)
	if err == sql.ErrNoRows {
		_, err = s.db.ExecContext(ctx, `INSERT INTO sim_fleet(id) VALUES (1) ON CONFLICT (id) DO NOTHING`)
		return httpapi.FleetState{Epoch: time.Now()}, err
	}
	if started.Valid {
		st.ScenarioStartedAt = &started.Time
	}
	return st, err
}

func (s *Store) SaveFleetState(ctx context.Context, st httpapi.FleetState) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO sim_fleet(id, epoch, scenario, scenario_started_at, updated_at) VALUES (1, $1, $2, $3, now())
		ON CONFLICT (id) DO UPDATE SET epoch=EXCLUDED.epoch, scenario=EXCLUDED.scenario, scenario_started_at=EXCLUDED.scenario_started_at, updated_at=now()`,
		st.Epoch, st.Scenario, st.ScenarioStartedAt)
	return err
}

// ResetFleet clears behaviours and the timeline and makes now the fleet's T0.
func (s *Store) ResetFleet(ctx context.Context, epoch time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM sim_behaviours`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO sim_fleet(id, epoch, scenario, scenario_started_at, updated_at) VALUES (1, $1, '', NULL, now())
		ON CONFLICT (id) DO UPDATE SET epoch=$1, scenario='', scenario_started_at=NULL, updated_at=now()`, epoch); err != nil {
		return err
	}
	return tx.Commit()
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
