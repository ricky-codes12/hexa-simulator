package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	apphttp "hexa-simulator/internal/httpapi"
	"hexa-simulator/internal/postgresstore"
)

var revision = "dev"

func main() {
	command := "serve"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}
	switch command {
	case "version":
		fmt.Printf("revision: %s\n", revision)
	case "health":
		health(os.Args[2:])
	case "serve":
		serve()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", command)
		os.Exit(2)
	}
}

func serve() {
	address := os.Getenv("APP_LISTEN")
	if address == "" {
		address = "127.0.0.1:8080"
	}
	ctx := context.Background()
	var store apphttp.TodoStore
	if databaseURL := os.Getenv("DATABASE_URL"); databaseURL != "" {
		postgres, err := postgresstore.Open(ctx, databaseURL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "database: %v\n", err)
			os.Exit(1)
		}
		defer postgres.Close()
		store = postgres
	}
	server := &http.Server{
		Addr:              address,
		Handler:           apphttp.Handler(revision, store, os.Getenv("WEB_ROOT")),
		ReadHeaderTimeout: 5 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	fmt.Printf("listening: http://%s revision=%s database=%t\n", address, revision, store != nil)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func health(args []string) {
	flags := flag.NewFlagSet("health", flag.ExitOnError)
	address := flags.String("address", "127.0.0.1:8080", "HTTP listen address")
	_ = flags.Parse(args)
	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Get("http://" + *address + "/healthz")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "health status: %s\n", response.Status)
		os.Exit(1)
	}
	fmt.Println("healthy")
}
