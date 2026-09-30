package httpapi

import (
	"context"
	"errors"
	"sync"
	"time"
)

// memStore is an in-memory FleetStore for tests.
type memStore struct {
	mu         sync.Mutex
	items      []Device
	behaviours []Behaviour
	fleet      FleetState
	nextB      int64
	pingErr    error
	persisted  int
}

func (f *memStore) Ping(context.Context) error { return f.pingErr }

func (f *memStore) ListDevices(context.Context) ([]Device, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Device(nil), f.items...), nil
}

func (f *memStore) GetDevice(_ context.Context, id int64) (Device, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, d := range f.items {
		if d.ID == id {
			return d, nil
		}
	}
	return Device{}, errors.New("not found")
}

func (f *memStore) CreateDevice(_ context.Context, in DeviceInput) (Device, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	d := Device{ID: int64(len(f.items) + 1), Name: in.Name, IMEI: in.IMEI, Model: in.Model, Kind: in.Kind, Output: in.Output, Estate: in.Estate,
		Status: "offline", Attributes: map[string]any{}, CreatedAt: time.Unix(1, 0).UTC(), UpdatedAt: time.Unix(1, 0).UTC()}
	f.items = append(f.items, d)
	return d, nil
}

func (f *memStore) UpdateDevice(_ context.Context, id int64, p DevicePatch) (Device, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.items {
		if f.items[i].ID == id {
			if p.Name != nil {
				f.items[i].Name = *p.Name
			}
			if p.Output != nil {
				f.items[i].Output = *p.Output
			}
			if p.Kind != nil {
				f.items[i].Kind = *p.Kind
			}
			if p.Estate != nil {
				f.items[i].Estate = *p.Estate
			}
			return f.items[i], nil
		}
	}
	return Device{}, errors.New("not found")
}

func (f *memStore) SetOutput(_ context.Context, ids []int64, output string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, id := range ids {
		for i := range f.items {
			if f.items[i].ID == id {
				f.items[i].Output = output
				n++
			}
		}
	}
	return n, nil
}

func (f *memStore) DeleteDevice(_ context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.items {
		if f.items[i].ID == id {
			f.items = append(f.items[:i], f.items[i+1:]...)
			return nil
		}
	}
	return errors.New("not found")
}

func (f *memStore) UpdateTelemetry(_ context.Context, id int64, in TelemetryInput) (Device, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.items {
		if f.items[i].ID == id {
			d := &f.items[i]
			d.Status, d.Latitude, d.Longitude, d.Speed, d.Heading, d.Ignition = in.Status, in.Latitude, in.Longitude, in.Speed, in.Heading, in.Ignition
			d.UpdatedAt = time.Now()
			return *d, nil
		}
	}
	return Device{}, errors.New("not found")
}

func (f *memStore) PersistTelemetry(_ context.Context, us []TelemetryUpdate) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, u := range us {
		for i := range f.items {
			if f.items[i].ID == u.ID {
				d := &f.items[i]
				d.Status, d.Latitude, d.Longitude, d.Speed, d.Heading, d.Ignition, d.Attributes, d.PlanClock, d.UpdatedAt =
					u.Status, u.Latitude, u.Longitude, u.Speed, u.Heading, u.Ignition, u.Attributes, u.PlanClock, u.At
			}
		}
	}
	f.persisted += len(us)
	return nil
}

func (f *memStore) CreateBehaviours(_ context.Context, bs []Behaviour) ([]Behaviour, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []Behaviour
	for _, b := range bs {
		f.nextB++
		b.ID = f.nextB
		f.behaviours = append(f.behaviours, b)
		out = append(out, b)
	}
	return out, nil
}

func (f *memStore) ActiveBehaviours(_ context.Context, at time.Time) ([]Behaviour, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []Behaviour
	for _, b := range f.behaviours {
		if b.EndsAt.After(at) {
			out = append(out, b)
		}
	}
	return out, nil
}

func (f *memStore) DeleteBehaviours(_ context.Context, filter BehaviourFilter) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ids := map[int64]bool{}
	for _, id := range filter.DeviceIDs {
		ids[id] = true
	}
	kept := f.behaviours[:0]
	n := 0
	for _, b := range f.behaviours {
		if filter.All || (filter.Source != "" && b.Source == filter.Source) || ids[b.DeviceID] {
			n++
			continue
		}
		kept = append(kept, b)
	}
	f.behaviours = kept
	return n, nil
}

func (f *memStore) FleetState(context.Context) (FleetState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.fleet, nil
}

func (f *memStore) SaveFleetState(_ context.Context, st FleetState) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fleet = st
	return nil
}

func (f *memStore) ResetFleet(_ context.Context, epoch time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.behaviours = nil
	f.fleet = FleetState{Epoch: epoch}
	return nil
}

// recordingOutput captures deliveries; err, when set, answers every delivery.
type recordingOutput struct {
	name  string
	mu    sync.Mutex
	got   []Delivery
	err   error
	block chan struct{}
}

func (o *recordingOutput) Name() string { return o.name }

func (o *recordingOutput) Deliver(ctx context.Context, ds []Delivery) []error {
	if o.block != nil {
		select {
		case <-o.block:
		case <-ctx.Done():
		}
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.got = append(o.got, ds...)
	errs := make([]error, len(ds))
	for i := range errs {
		errs[i] = o.err
	}
	return errs
}

func (o *recordingOutput) deliveries() []Delivery {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]Delivery(nil), o.got...)
}
