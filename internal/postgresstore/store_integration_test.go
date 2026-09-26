//go:build integration

package postgresstore

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestPostgreSQLRoundTrip(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Fatal("TEST_DATABASE_URL is required for integration tests")
	}
	store, err := Open(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	title := "integration-" + time.Now().UTC().Format("20060102T150405.000000000")
	created, err := store.Create(context.Background(), title)
	if err != nil {
		t.Fatal(err)
	}
	items, err := store.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range items {
		if item.ID == created.ID && item.Title == title {
			found = true
		}
	}
	if !found {
		t.Fatalf("created item not returned: %+v", items)
	}
}
