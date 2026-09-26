package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Todo struct {
	ID        int64     `json:"id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
}

type TodoStore interface {
	Ping(context.Context) error
	List(context.Context) ([]Todo, error)
	Create(context.Context, string) (Todo, error)
}

type healthResponse struct {
	Revision           string `json:"revision"`
	DatabaseConfigured bool   `json:"database_configured"`
	DatabaseReady      bool   `json:"database_ready"`
}

func Handler(revision string, store TodoStore, webRoot string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(writer http.ResponseWriter, request *http.Request) {
		ready := false
		status := http.StatusOK
		if store != nil {
			ready = store.Ping(request.Context()) == nil
			if !ready {
				status = http.StatusServiceUnavailable
			}
		}
		writeJSON(writer, status, healthResponse{Revision: revision, DatabaseConfigured: store != nil, DatabaseReady: ready})
	})
	mux.HandleFunc("GET /api/todos", func(writer http.ResponseWriter, request *http.Request) {
		if store == nil {
			writeError(writer, http.StatusServiceUnavailable, "PostgreSQL is not configured; set DATABASE_URL")
			return
		}
		items, err := store.List(request.Context())
		if err != nil {
			writeError(writer, http.StatusInternalServerError, "load todos")
			return
		}
		writeJSON(writer, http.StatusOK, map[string]any{"items": items})
	})
	mux.HandleFunc("POST /api/todos", func(writer http.ResponseWriter, request *http.Request) {
		if store == nil {
			writeError(writer, http.StatusServiceUnavailable, "PostgreSQL is not configured; set DATABASE_URL")
			return
		}
		var input struct {
			Title string `json:"title"`
		}
		if err := json.NewDecoder(request.Body).Decode(&input); err != nil || strings.TrimSpace(input.Title) == "" {
			writeError(writer, http.StatusBadRequest, "title is required")
			return
		}
		item, err := store.Create(request.Context(), strings.TrimSpace(input.Title))
		if err != nil {
			writeError(writer, http.StatusInternalServerError, "create todo")
			return
		}
		writeJSON(writer, http.StatusCreated, item)
	})
	if strings.TrimSpace(webRoot) != "" {
		root := filepath.Clean(webRoot)
		assets := http.FileServer(http.Dir(root))
		mux.Handle("GET /assets/", assets)
		mux.HandleFunc("GET /", func(writer http.ResponseWriter, request *http.Request) {
			if strings.HasPrefix(request.URL.Path, "/api/") || request.URL.Path == "/healthz" {
				http.NotFound(writer, request)
				return
			}
			index := filepath.Join(root, "index.html")
			if _, err := os.Stat(index); err != nil {
				http.NotFound(writer, request)
				return
			}
			http.ServeFile(writer, request, index)
		})
	}
	return mux
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func writeError(writer http.ResponseWriter, status int, message string) {
	writeJSON(writer, status, map[string]string{"error": message})
}
