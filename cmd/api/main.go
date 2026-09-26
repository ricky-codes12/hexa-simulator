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

	"hexa-simulator/internal/gateway"
	apphttp "hexa-simulator/internal/httpapi"
	"hexa-simulator/internal/postgresstore"
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
	if databaseURL := os.Getenv("DATABASE_URL"); databaseURL != "" {
		postgres, err := postgresstore.Open(ctx, databaseURL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "database: %v\n", err)
			os.Exit(1)
		}
		defer postgres.Close()
		store = postgres
	}
	gatewayAddress := os.Getenv("TELTONIKA_GATEWAY_ADDR")
	var localGateway *gateway.Server
	if gatewayListen := os.Getenv("TELTONIKA_GATEWAY_LISTEN"); gatewayListen != "" {
		var err error
		localGateway, err = gateway.New(gateway.Config{
			ListenAddress: gatewayListen,
			SensorURL:     os.Getenv("HEXA_SENSOR_URL"),
			SecretKey:     os.Getenv("HEXA_SENSOR_SECRET_KEY"),
			Timeout:       sensorTimeout(),
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "gateway configuration: %v\n", err)
			os.Exit(1)
		}
		if err := localGateway.Start(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "gateway: %v\n", err)
			os.Exit(1)
		}
		if gatewayAddress == "" {
			gatewayAddress = localGateway.Addr()
		}
	}
	var forwarders []apphttp.TelemetryForwarder
	if gatewayAddress != "" {
		forwarders = append(forwarders, teltonikaForwarder{client: teltonika.Client{Address: gatewayAddress, Timeout: gatewayTimeout()}})
	}
	server := &http.Server{
		Addr:              address,
		Handler:           apphttp.Handler(revision, store, os.Getenv("WEB_ROOT"), forwarders...),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	fmt.Printf("listening: http://%s revision=%s database=%t teltonika_gateway=%t local_gateway=%t\n", address, revision, store != nil, len(forwarders) > 0, localGateway != nil)
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

type teltonikaForwarder struct{ client teltonika.Client }

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

func sensorTimeout() time.Duration {
	value := os.Getenv("HEXA_SENSOR_TIMEOUT")
	if value == "" {
		return 5 * time.Second
	}
	timeout, err := time.ParseDuration(value)
	if err != nil || timeout <= 0 {
		fmt.Fprintf(os.Stderr, "invalid HEXA_SENSOR_TIMEOUT %q; using 5s\n", value)
		return 5 * time.Second
	}
	return timeout
}
