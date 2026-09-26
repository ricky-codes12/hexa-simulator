package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeStore struct {
	items   []Todo
	pingErr error
}

func (f *fakeStore) Ping(context.Context) error { return f.pingErr }
func (f *fakeStore) List(context.Context) ([]Todo, error) {
	return append([]Todo(nil), f.items...), nil
}
func (f *fakeStore) Create(_ context.Context, title string) (Todo, error) {
	item := Todo{ID: int64(len(f.items) + 1), Title: title, CreatedAt: time.Unix(1, 0).UTC()}
	f.items = append(f.items, item)
	return item, nil
}

func TestHealthWorksWithoutDatabaseAndTodosExplainConfiguration(t *testing.T) {
	handler := Handler("test-revision", nil, "")
	for _, tc := range []struct {
		path       string
		wantStatus int
		want       string
	}{
		{path: "/healthz", wantStatus: http.StatusOK, want: `"database_configured":false`},
		{path: "/api/todos", wantStatus: http.StatusServiceUnavailable, want: "DATABASE_URL"},
	} {
		request := httptest.NewRequest(http.MethodGet, tc.path, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != tc.wantStatus || !strings.Contains(response.Body.String(), tc.want) {
			t.Fatalf("%s status=%d body=%s", tc.path, response.Code, response.Body.String())
		}
	}
}

func TestConfiguredButUnreadyDatabaseFailsHealth(t *testing.T) {
	handler := Handler("test", &fakeStore{pingErr: errors.New("database not ready")}, "")
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), `"database_ready":false`) {
		t.Fatalf("health status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestTodosUseStore(t *testing.T) {
	handler := Handler("test", &fakeStore{}, "")
	create := httptest.NewRequest(http.MethodPost, "/api/todos", strings.NewReader(`{"title":"Ship starter"}`))
	created := httptest.NewRecorder()
	handler.ServeHTTP(created, create)
	if created.Code != http.StatusCreated || !strings.Contains(created.Body.String(), "Ship starter") {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	list := httptest.NewRequest(http.MethodGet, "/api/todos", nil)
	listed := httptest.NewRecorder()
	handler.ServeHTTP(listed, list)
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), "Ship starter") {
		t.Fatalf("list status=%d body=%s", listed.Code, listed.Body.String())
	}
}
