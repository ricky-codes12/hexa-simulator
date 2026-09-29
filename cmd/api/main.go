package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"hexa-simulator/internal/fleet"
	apphttp "hexa-simulator/internal/httpapi"
	"hexa-simulator/internal/mqttout"
	"hexa-simulator/internal/postgresstore"
	"hexa-simulator/internal/sensorpush"
	"hexa-simulator/internal/teltonika"
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
	case "sensor-onboard":
		os.Exit(sensorOnboard(os.Args[2:]))
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", command)
		os.Exit(2)
	}
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

func serve() {
	address := os.Getenv("APP_LISTEN")
	if address == "" {
		address = "127.0.0.1:8080"
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	outputs, err := configuredOutputs()
	if err != nil {
		fail("outputs: %v", err)
	}
	var names []string
	for _, o := range outputs {
		names = append(names, o.Name())
	}

	var store apphttp.DeviceStore
	var authStore apphttp.AuthStore
	var runtime *apphttp.SimulationRuntime
	if databaseURL := os.Getenv("DATABASE_URL"); databaseURL != "" {
		postgres, err := postgresstore.Open(ctx, databaseURL)
		if err != nil {
			fail("database: %v", err)
		}
		defer postgres.Close()
		if err := postgres.CheckSchema(ctx); err != nil {
			fail("database: %v", err)
		}
		authPostgres, err := postgresstore.Open(ctx, databaseURL)
		if err != nil {
			fail("authentication database: %v", err)
		}
		defer authPostgres.Close()

		composition, err := fleet.ParseComposition(os.Getenv("SIM_DEMO_FLEET"))
		if err != nil {
			fail("SIM_DEMO_FLEET: %v", err)
		}
		seed := int64(7)
		if raw := strings.TrimSpace(os.Getenv("SIM_SEED")); raw != "" {
			if seed, err = strconv.ParseInt(raw, 10, 64); err != nil {
				fail("SIM_SEED must be an integer")
			}
		}
		var roster []apphttp.SeedDevice
		for _, u := range fleet.Roster(composition, seed) {
			roster = append(roster, apphttp.SeedDevice{Name: u.Name, IMEI: u.IMEI, Model: u.Model, Kind: u.Kind.Key, Output: u.Output, Estate: u.Estate})
		}
		total, added, removed, err := postgres.EnsureFleet(ctx, roster)
		if err != nil {
			fail("demo fleet: %v", err)
		}
		fmt.Printf("demo fleet: %d devices (%d seeded: %s; %d added, %d removed)\n", total, len(roster), composition, added, removed)

		store = postgres
		authStore = authPostgres
		adminPassword := os.Getenv("SIM_ADMIN_PASSWORD")
		if len(adminPassword) < 12 {
			fail("SIM_ADMIN_PASSWORD must be set to at least 12 characters")
		}
		adminUsername := os.Getenv("SIM_ADMIN_USERNAME")
		if adminUsername == "" {
			adminUsername = os.Getenv("SIM_ADMIN_EMAIL") // Backward-compatible configuration fallback.
		}
		if err := apphttp.EnsureBootstrapAdmin(ctx, authStore, adminUsername, adminPassword); err != nil {
			fail("bootstrap admin: %v", err)
		}

		runtime = apphttp.NewSimulationRuntime(ctx, postgres, apphttp.RuntimeOptions{Interval: duration("SIM_REPORT_INTERVAL", 5*time.Second), Seed: seed, Outputs: outputs})
		started, err := runtime.StartFleet()
		if err != nil {
			fail("simulation fleet runtime: %v", err)
		}
		configured := strings.Join(names, ", ")
		if configured == "" {
			configured = "none configured"
		}
		fmt.Printf("simulation fleet runtime: %d devices running, outputs: %s\n", started, configured)
	}
	server := &http.Server{
		Addr:              address,
		Handler:           apphttp.SecureHandlerWithRuntime(revision, store, authStore, os.Getenv("WEB_ROOT"), runtime),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	fmt.Printf("listening: http://%s revision=%s database=%t outputs=%s\n", address, revision, store != nil, strings.Join(names, ","))
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fail("%v", err)
	}
}

// configuredOutputs builds the outputs whose settings are present. Secrets come only from the
// protected environment.
func configuredOutputs() ([]apphttp.Output, error) {
	var out []apphttp.Output
	if url := os.Getenv("SIM_MQTT_URL"); url != "" {
		topic := os.Getenv("SIM_MQTT_TOPIC")
		if topic == "" {
			topic = "{imei}/data"
		}
		client := &mqttout.Client{URL: url, Topic: topic, ClientID: os.Getenv("SIM_MQTT_CLIENT_ID"), Username: os.Getenv("SIM_MQTT_USERNAME"),
			Password: os.Getenv("SIM_MQTT_PASSWORD"), Timeout: duration("SIM_MQTT_TIMEOUT", 5*time.Second)}
		out = append(out, apphttp.MQTTOutput{Client: client})
	}
	if addr := os.Getenv("TELTONIKA_GATEWAY_ADDR"); addr != "" {
		codec := os.Getenv("TELTONIKA_CODEC")
		if codec != "" && !strings.EqualFold(codec, "8") && !strings.EqualFold(codec, "8E") {
			return nil, fmt.Errorf("TELTONIKA_CODEC must be 8 or 8E")
		}
		out = append(out, apphttp.TeltonikaOutput{Client: &teltonika.Client{Address: addr, Timeout: duration("TELTONIKA_GATEWAY_TIMEOUT", 5*time.Second), Codec: codec}})
	}
	pushURL, pushKey := os.Getenv("SIM_SENSOR_PUSH_URL"), os.Getenv("SIM_SENSOR_PUSH_KEY")
	if pushURL != "" || pushKey != "" {
		if pushURL == "" || pushKey == "" {
			return nil, fmt.Errorf("SIM_SENSOR_PUSH_URL and SIM_SENSOR_PUSH_KEY must be configured together")
		}
		out = append(out, apphttp.HTTPPushOutput{Client: sensorpush.Client{URL: pushURL, Key: pushKey, Timeout: duration("SIM_SENSOR_PUSH_TIMEOUT", 5*time.Second)}})
	}
	return out, nil
}

func duration(name string, def time.Duration) time.Duration {
	value := os.Getenv(name)
	if value == "" {
		return def
	}
	d, err := time.ParseDuration(value)
	if err != nil || d <= 0 {
		fmt.Fprintf(os.Stderr, "invalid %s %q; using %s\n", name, value, def)
		return def
	}
	return d
}

func health(args []string) {
	flags := flag.NewFlagSet("health", flag.ExitOnError)
	address := flags.String("address", "127.0.0.1:8080", "HTTP listen address")
	_ = flags.Parse(args)
	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Get("http://" + *address + "/healthz")
	if err != nil {
		fail("%v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		fail("health status: %s", response.Status)
	}
	fmt.Println("healthy")
}
