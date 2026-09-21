package deliverylane

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"pgregory.net/rapid"

	"github.com/c360studio/semstreams/natsclient"
)

// The latch is a stateful history: its outcome depends on the ORDER of the
// results a lane observes, which is the third shape the PBT decision names
// (docs/contributing/01-testing.md, "When to Use Property-Based Testing"). The
// examples in deliverylane_test.go fix one order each; this property searches
// arbitrary orders for a history where the first owner-stop result is not the
// one that closes the lane and reaches the owner.
//
// The expectation is derived from the drawn history alone — firstFatal is
// computed from the draw, never read back from the Admission — so the property
// cannot reconstruct the implementation's answer.
//
// Boundary coverage is by construction, not by striding: the length generator
// includes 0 (a lane that never latches) and the element generator is a plain
// bool, so "the very first result is fatal" and "no result is fatal" are
// ordinary draws rather than lucky ones.

// propResult builds one result through the production settlement path: a
// Quarantine with a cause is the owner-stop case, a clean Ack is not.
func propResult(fatal bool, cause string) natsclient.DeliveryResult {
	msg := &fakeMsg{
		data:     []byte(`{}`),
		subject:  "prop.lane",
		metadata: &jetstream.MsgMetadata{NumDelivered: 1},
	}
	if fatal {
		return natsclient.SettleDeliveryWithRetry(msg, natsclient.ImmediateDeliveryRetry(),
			natsclient.DeliveryDecisionQuarantine, errors.New(cause))
	}
	return natsclient.SettleDelivery(msg, natsclient.DeliveryDecisionAck, nil)
}

// TestPropFirstOwnerStopResultWins: over any history of results, the lane
// admits exactly until its first owner-stop result, the health writer sees
// that result once and only once, and the observer drains the exact handle on
// that same first cause.
//
// spec: jetstream-consumer-policy / control loss shuts down through the existing exact owner
func TestPropFirstOwnerStopResultWins(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		history := rapid.SliceOfN(rapid.Bool(), 0, 6).Draw(rt, "ownerStopAtStep")

		var recorded []error
		admission := NewAdmission(func(result natsclient.DeliveryResult) {
			recorded = append(recorded, result.Err())
		}, nil)

		firstFatal := -1
		for step, fatal := range history {
			if open := admission.Admit(); open != (firstFatal == -1) {
				rt.Fatalf("step %d: Admit()=%v with %d prior owner-stop results", step, open, firstFatal+1)
			}
			admission.Latch(propResult(fatal, fmt.Sprintf("cause-%d", step)))
			if fatal && firstFatal == -1 {
				firstFatal = step
			}
		}

		if open := admission.Admit(); open != (firstFatal == -1) {
			rt.Fatalf("after %d results Admit()=%v, first owner-stop result at %d", len(history), open, firstFatal)
		}
		if firstFatal == -1 {
			if len(recorded) != 0 {
				rt.Fatalf("a history with no owner-stop result recorded %d fatals", len(recorded))
			}
		} else {
			if len(recorded) != 1 {
				rt.Fatalf("history %v recorded %d fatals, want exactly the first", history, len(recorded))
			}
			want := fmt.Sprintf("cause-%d", firstFatal)
			if recorded[0] == nil || !strings.Contains(recorded[0].Error(), want) {
				rt.Fatalf("health writer saw %v, want the first cause %q", recorded[0], want)
			}
		}

		handle := newFakeHandle()
		binding := NewBinding(handle)
		observed := make(chan natsclient.DeliveryResult, 1)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		Observe(ctx, binding, admission, func(result natsclient.DeliveryResult) { observed <- result })

		if firstFatal == -1 {
			cancel()
			<-binding.Done()
			if drains := handle.drains.Load(); drains != 0 {
				rt.Fatalf("a lane with no owner-stop result drained %d times", drains)
			}
			return
		}

		select {
		case result := <-observed:
			want := fmt.Sprintf("cause-%d", firstFatal)
			if result.Err() == nil || !strings.Contains(result.Err().Error(), want) {
				rt.Fatalf("observer saw %v, want the first cause %q", result.Err(), want)
			}
		case <-time.After(5 * time.Second):
			rt.Fatalf("history %v buffered no fatal for the observer", history)
		}
		<-binding.Done()
		if drains := handle.drains.Load(); drains != 1 {
			rt.Fatalf("the exact handle drained %d times, want exactly once", drains)
		}
	})
}
