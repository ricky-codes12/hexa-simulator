package postgresstore

import (
	"context"
	"database/sql"
	"fmt"

	"hexa-simulator/internal/httpapi"
	_ "github.com/lib/pq"
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
	if err := s.db.QueryRowContext(ctx, "SELECT to_regclass('public.todos') IS NOT NULL").Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return fmt.Errorf("required table todos is not migrated")
	}
	return nil
}
func (s *Store) List(ctx context.Context) ([]httpapi.Todo, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, title, created_at FROM todos ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []httpapi.Todo{}
	for rows.Next() {
		var item httpapi.Todo
		if err := rows.Scan(&item.ID, &item.Title, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
func (s *Store) Create(ctx context.Context, title string) (httpapi.Todo, error) {
	var item httpapi.Todo
	err := s.db.QueryRowContext(ctx, `INSERT INTO todos(title) VALUES($1) RETURNING id, title, created_at`, title).Scan(&item.ID, &item.Title, &item.CreatedAt)
	return item, err
}
