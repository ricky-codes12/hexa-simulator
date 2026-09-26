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
