package httpapi

import (
	"context"
	"time"
)

// Device is a virtual device as stored.
type Device struct {
	ID         int64          `json:"id"`
	Name       string         `json:"name"`
	IMEI       string         `json:"imei"`
	Model      string         `json:"model"`
	Kind       string         `json:"kind"`
	Output     string         `json:"output"`
	Estate     string         `json:"estate"`
	Seeded     bool           `json:"seeded"`
	Status     string         `json:"status"`
	Latitude   float64        `json:"latitude"`
	Longitude  float64        `json:"longitude"`
	Speed      float64        `json:"speed"`
	Heading    float64        `json:"heading"`
	Ignition   bool           `json:"ignition"`
	Attributes map[string]any `json:"attributes"`
	// PlanClock is the unit's itinerary clock when it last reported; nil starts it at its phase.
	PlanClock *float64  `json:"-"`
	UpdatedAt time.Time `json:"updated_at"`
	CreatedAt time.Time `json:"created_at"`
}

// DeviceInput creates a device. Kind, output and estate are optional: the default kind is a haul
// truck, and a device created without an output sends to every configured output, as devices
// did before per-device routing (plan S1 keeps that as the explicit "all").
type DeviceInput struct {
	Name   string `json:"name"`
	IMEI   string `json:"imei"`
	Model  string `json:"model"`
	Kind   string `json:"kind"`
	Output string `json:"output"`
	Estate string `json:"estate"`
}

// DevicePatch changes a device; nil fields stay.
type DevicePatch struct {
	Name   *string `json:"name"`
	Output *string `json:"output"`
	Kind   *string `json:"kind"`
	Estate *string `json:"estate"`
}

// TelemetryInput is a hand-made telemetry sample (POST /api/devices/{id}/telemetry).
type TelemetryInput struct {
	Status    string  `json:"status"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Speed     float64 `json:"speed"`
	Heading   float64 `json:"heading"`
	Ignition  bool    `json:"ignition"`
}

// TelemetryUpdate is the latest state of one unit, written in batches by the runtime.
type TelemetryUpdate struct {
	ID         int64
	Status     string
	Latitude   float64
	Longitude  float64
	Speed      float64
	Heading    float64
	Ignition   bool
	Attributes map[string]any
	PlanClock  *float64
	At         time.Time
}

// SeedDevice is one unit of the seeded fleet.
type SeedDevice struct{ Name, IMEI, Model, Kind, Output, Estate string }

// Behaviour is a misbehaviour given to one unit from StartsAt until EndsAt.
type Behaviour struct {
	ID       int64          `json:"id"`
	DeviceID int64          `json:"device_id"`
	Type     string         `json:"type"`
	Params   map[string]any `json:"params"`
	StartsAt time.Time      `json:"starts_at"`
	EndsAt   time.Time      `json:"ends_at"`
	Source   string         `json:"source"`
}

// BehaviourFilter selects behaviours to remove: those of some devices, of a source, or all.
type BehaviourFilter struct {
	DeviceIDs []int64
	Source    string
	All       bool
}

// FleetState is the fleet's T0 and the timeline that runs.
type FleetState struct {
	Epoch             time.Time
	Scenario          string
	ScenarioStartedAt *time.Time
}

// DeviceSearchStore pages through devices server-side.
type DeviceSearchStore interface {
	SearchDevices(context.Context, string, int, int) ([]Device, int, error)
}

// DeviceLookupStore reads one device.
type DeviceLookupStore interface {
	GetDevice(context.Context, int64) (Device, error)
}

// DeviceStore is the device registry.
type DeviceStore interface {
	Ping(context.Context) error
	ListDevices(context.Context) ([]Device, error)
	CreateDevice(context.Context, DeviceInput) (Device, error)
	UpdateTelemetry(context.Context, int64, TelemetryInput) (Device, error)
	DeleteDevice(context.Context, int64) error
}

// FleetStore is what the fleet runtime persists.
type FleetStore interface {
	DeviceStore
	DeviceLookupStore
	UpdateDevice(context.Context, int64, DevicePatch) (Device, error)
	SetOutput(context.Context, []int64, string) (int, error)
	PersistTelemetry(context.Context, []TelemetryUpdate) error
	CreateBehaviours(context.Context, []Behaviour) ([]Behaviour, error)
	ActiveBehaviours(context.Context, time.Time) ([]Behaviour, error)
	DeleteBehaviours(context.Context, BehaviourFilter) (int, error)
	FleetState(context.Context) (FleetState, error)
	SaveFleetState(context.Context, FleetState) error
	ResetFleet(context.Context, time.Time) error
}
