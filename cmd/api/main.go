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
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	var store apphttp.DeviceStore
	var authStore apphttp.AuthStore
	if databaseURL := os.Getenv("DATABASE_URL"); databaseURL != "" {
		postgres, err := postgresstore.Open(ctx, databaseURL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "database: %v\n", err)
			os.Exit(1)
		}
		defer postgres.Close()
		authPostgres, err := postgresstore.Open(ctx, databaseURL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "authentication database: %v\n", err)
			os.Exit(1)
		}
		defer authPostgres.Close()
		if total, err := postgres.EnsureDemoFleet(ctx, 350); err != nil {
			fmt.Fprintf(os.Stderr, "demo fleet: %v\n", err)
			os.Exit(1)
		} else {
			fmt.Printf("demo fleet: %d devices ready\n", total)
		}
		store = postgres
		authStore = authPostgres
		adminPassword := os.Getenv("SIM_ADMIN_PASSWORD")
		if len(adminPassword) < 12 {
			fmt.Fprintln(os.Stderr, "SIM_ADMIN_PASSWORD must be set to at least 12 characters")
			os.Exit(1)
		}
		adminUsername := os.Getenv("SIM_ADMIN_USERNAME")
		if adminUsername == "" {
			adminUsername = os.Getenv("SIM_ADMIN_EMAIL") // Backward-compatible configuration fallback.
		}
		if err := apphttp.EnsureBootstrapAdmin(ctx, authStore, adminUsername, adminPassword); err != nil {
			fmt.Fprintf(os.Stderr, "bootstrap admin: %v\n", err)
			os.Exit(1)
		}
	}
	gatewayAddress := os.Getenv("TELTONIKA_GATEWAY_ADDR")
	pushURL := os.Getenv("SIM_SENSOR_PUSH_URL")
	pushKey := os.Getenv("SIM_SENSOR_PUSH_KEY")
	var forwarders []apphttp.TelemetryForwarder
	if gatewayAddress != "" {
		tcpClient := &teltonika.Client{Address: gatewayAddress, Timeout: gatewayTimeout(), Codec: os.Getenv("TELTONIKA_CODEC")}
		defer tcpClient.Close()
		forwarders = append(forwarders, teltonikaForwarder{client: tcpClient})
	}
	if pushURL != "" || pushKey != "" {
		if pushURL == "" || pushKey == "" {
			fmt.Fprintln(os.Stderr, "SIM_SENSOR_PUSH_URL and SIM_SENSOR_PUSH_KEY must be configured together")
			os.Exit(1)
		}
		forwarders = append(forwarders, sensorPushForwarder{client: sensorpush.Client{URL: pushURL, Key: pushKey, Timeout: sensorPushTimeout()}})
	}
	var mqttClient *mqttout.Client
	if mqttURL := os.Getenv("SIM_MQTT_URL"); mqttURL != "" {
		topic := os.Getenv("SIM_MQTT_TOPIC")
		if topic == "" {
			topic = "{imei}/data"
		}
		mqttClient = &mqttout.Client{URL: mqttURL, Topic: topic, Timeout: mqttTimeout()}
		defer mqttClient.Close()
		forwarders = append(forwarders, mqttForwarder{client: mqttClient})
	}
	var runtime *apphttp.SimulationRuntime
	if store != nil {
		runtime = apphttp.NewSimulationRuntime(ctx, store, 3*time.Second, forwarders...)
		started, err := runtime.StartFleet()
		if err != nil {
			fmt.Fprintf(os.Stderr, "simulation fleet runtime: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("simulation fleet runtime: %d devices running\n", started)
	}
	server := &http.Server{
		Addr:              address,
		Handler:           apphttp.SecureHandlerWithRuntime(revision, store, authStore, os.Getenv("WEB_ROOT"), runtime, forwarders...),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	fmt.Printf("listening: http://%s revision=%s database=%t teltonika_gateway=%t sensor_push=%t\n", address, revision, store != nil, gatewayAddress != "", pushURL != "")
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

type teltonikaForwarder struct{ client *teltonika.Client }

func (f teltonikaForwarder) OutputName() string { return "teltonika-direct" }

func (f teltonikaForwarder) ForwardTelemetry(ctx context.Context, device apphttp.Device) error {
	return f.client.Send(ctx, teltonika.Telemetry{
		IMEI: device.IMEI, Timestamp: device.UpdatedAt, Latitude: device.Latitude, Longitude: device.Longitude,
		Speed: device.Speed, Heading: device.Heading, Ignition: device.Ignition,
	})
}

func gatewayTimeout() time.Duration {
	value := os.Getenv("TELTONIKA_GATEWAY_TIMEOUT")
	if value == "" {
		return 5 * time.Second
	}
	timeout, err := time.ParseDuration(value)
	if err != nil || timeout <= 0 {
		fmt.Fprintf(os.Stderr, "invalid TELTONIKA_GATEWAY_TIMEOUT %q; using 5s\n", value)
		return 5 * time.Second
	}
	return timeout
}

type sensorPushForwarder struct{ client sensorpush.Client }

func (f sensorPushForwarder) OutputName() string { return "http-push" }

func (f sensorPushForwarder) ForwardTelemetry(ctx context.Context, device apphttp.Device) error {
	return f.client.Send(ctx, sensorpush.Telemetry{
		HardwareID: device.IMEI, DeviceTime: device.UpdatedAt, Latitude: device.Latitude, Longitude: device.Longitude,
		Speed: device.Speed, Heading: device.Heading, Ignition: device.Ignition, Movement: device.Speed > 0,
	})
}

func sensorPushTimeout() time.Duration {
	value := os.Getenv("SIM_SENSOR_PUSH_TIMEOUT")
	if value == "" {
		return 5 * time.Second
	}
	timeout, err := time.ParseDuration(value)
	if err != nil || timeout <= 0 {
		fmt.Fprintf(os.Stderr, "invalid SIM_SENSOR_PUSH_TIMEOUT %q; using 5s\n", value)
		return 5 * time.Second
	}
	return timeout
}

type mqttForwarder struct{ client *mqttout.Client }

func (f mqttForwarder) OutputName() string { return "mqtt" }

func (f mqttForwarder) ForwardTelemetry(ctx context.Context, device apphttp.Device) error {
	return f.client.Publish(ctx, mqttout.Telemetry{HardwareID: device.IMEI, DeviceTime: device.UpdatedAt, Latitude: device.Latitude, Longitude: device.Longitude, Speed: device.Speed, Heading: device.Heading, Ignition: device.Ignition, Movement: device.Speed > 0})
}
func mqttTimeout() time.Duration {
	value := os.Getenv("SIM_MQTT_TIMEOUT")
	if value == "" {
		return 5 * time.Second
	}
	timeout, err := time.ParseDuration(value)
	if err != nil || timeout <= 0 {
		fmt.Fprintf(os.Stderr, "invalid SIM_MQTT_TIMEOUT %q; using 5s\n", value)
		return 5 * time.Second
	}
	return timeout
}
