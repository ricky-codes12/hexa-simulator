package httpapi

import (
	"context"
	"errors"
	"fmt"

	"hexa-simulator/internal/fleet"
	"hexa-simulator/internal/mqttout"
	"hexa-simulator/internal/sensorpush"
	"hexa-simulator/internal/telemetry"
	"hexa-simulator/internal/teltonika"
)

// Delivery is one device's records for its output: usually one record, several after a burst.
type Delivery struct {
	DeviceID int64
	IMEI     string
	Kind     string
	Estate   string
	Records  []telemetry.Record
}

// Output is one configured transport. Deliver sends deliveries and returns one error per
// delivery, nil for an accepted one.
type Output interface {
	Name() string
	Deliver(ctx context.Context, ds []Delivery) []error
}

// Rejection is a record the receiving side refused, for a reason such as unknown_device: the
// transport works, the device is not registered yet.
type Rejection struct{ Reason string }

func (r Rejection) Error() string { return "rejected: " + r.Reason }

// outputPolicy is how a dispatcher batches for a transport.
type outputPolicy struct {
	workers int // concurrent Deliver calls
	batch   int // deliveries per call
}

func policyFor(name string) outputPolicy {
	switch name {
	case fleet.OutputHTTPPush:
		return outputPolicy{workers: 4, batch: 100} // one request carries many devices
	case fleet.OutputMQTT:
		return outputPolicy{workers: 1, batch: 32} // one broker connection
	}
	return outputPolicy{workers: 16, batch: 1} // one TCP session per tracker
}

// MQTTOutput publishes each device's records to its own topic.
type MQTTOutput struct{ Client *mqttout.Client }

func (MQTTOutput) Name() string { return fleet.OutputMQTT }

func (o MQTTOutput) Deliver(ctx context.Context, ds []Delivery) []error {
	errs := make([]error, len(ds))
	for i, d := range ds {
		errs[i] = o.Client.PublishRecords(ctx, d.Records, mqttout.Topics{Kind: d.Kind, Estate: d.Estate})
	}
	return errs
}

// TeltonikaOutput sends each device's records over its own tracker session.
type TeltonikaOutput struct{ Client *teltonika.Client }

func (TeltonikaOutput) Name() string { return fleet.OutputTeltonika }

func (o TeltonikaOutput) Deliver(ctx context.Context, ds []Delivery) []error {
	errs := make([]error, len(ds))
	for i, d := range ds {
		records := make([]teltonika.Record, len(d.Records))
		for j, r := range d.Records {
			records[j] = teltonika.FromTelemetry(r)
		}
		err := o.Client.Send(ctx, d.IMEI, records)
		if errors.Is(err, teltonika.ErrRefused) {
			err = Rejection{"IMEI refused at the handshake (not registered?)"}
		}
		errs[i] = err
	}
	return errs
}

// HTTPPushOutput posts the records of many devices in one request, as a vendor cloud does, and
// hands Sensor's per-record rejections back to their devices.
type HTTPPushOutput struct{ Client sensorpush.Client }

func (HTTPPushOutput) Name() string { return fleet.OutputHTTPPush }

func (o HTTPPushOutput) Deliver(ctx context.Context, ds []Delivery) []error {
	errs := make([]error, len(ds))
	var records []telemetry.Record
	var owner []int
	flush := func() {
		if len(records) == 0 {
			return
		}
		res, err := o.Client.Send(ctx, records)
		if err != nil {
			for _, i := range owner {
				errs[i] = err
			}
		} else {
			for _, e := range res.Errors {
				if e.Index >= 0 && e.Index < len(owner) && errs[owner[e.Index]] == nil {
					errs[owner[e.Index]] = Rejection{e.Reason}
				}
			}
		}
		records, owner = records[:0], owner[:0]
	}
	for i, d := range ds {
		if len(records)+len(d.Records) > sensorpush.MaxBatch {
			flush()
		}
		for _, r := range d.Records {
			records = append(records, r)
			owner = append(owner, i)
		}
	}
	flush()
	return errs
}

// describe turns an output error into what the console shows.
func describe(err error) (status, text string) {
	var rej Rejection
	if errors.As(err, &rej) {
		return "rejected", rej.Reason
	}
	return "error", fmt.Sprint(err)
}
