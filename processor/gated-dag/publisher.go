package gateddagexec

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
)

// publisher publishes the dispatch reference for a dispatchable unit. It is an
// interface so unit tests record dispatch ordering against a fake.
type publisher interface {
	// Dispatch publishes the unit's reference envelope to the dispatch subject.
	Dispatch(ctx context.Context, unitID string) error
}

// natsPublisher wraps the reference in a BaseMessage envelope (payload-registry
// contract) and publishes it to the configured subject.
type natsPublisher struct {
	nc             *natsclient.Client
	subject        string
	fanOutWorkflow string
}

// Dispatch publishes the registry-wrapped DispatchMessage reference to the
// dispatch stream via an ack-confirmed JetStream publish (ADR-070). A returned
// error means the ack did NOT come back — the message was not persisted and will
// not be delivered — so the caller can safely roll the claim back (B1).
func (p *natsPublisher) Dispatch(ctx context.Context, unitID string) error {
	msg := &DispatchMessage{UnitEntityID: unitID, FanOutWorkflow: p.fanOutWorkflow}
	base := message.NewBaseMessage(msg.Schema(), msg, "gateddag-executor")
	data, err := json.Marshal(base)
	if err != nil {
		return fmt.Errorf("marshal dispatch message for %s: %w", unitID, err)
	}
	if _, err := p.nc.PublishToStreamWithAck(ctx, p.subject, data); err != nil {
		return fmt.Errorf("publish dispatch for %s to %s: %w", unitID, p.subject, err)
	}
	return nil
}

// stallPublisher publishes the edge-triggered stall event (#365.3). An interface
// so the executor can hold nil (no stall_subject) and unit tests can fake it.
type stallPublisher interface {
	// PublishStall emits the wedge event for the given held-ready unit IDs.
	PublishStall(ctx context.Context, stalledUnits []string) error
}

// natsStallPublisher wraps the StallEvent in a BaseMessage and publishes it.
type natsStallPublisher struct {
	nc               *natsclient.Client
	subject          string
	fanOutWorkflow   string
	fanOutInstanceID string
}

// PublishStall publishes the registry-wrapped StallEvent.
func (p *natsStallPublisher) PublishStall(ctx context.Context, stalledUnits []string) error {
	msg := &StallEvent{StalledUnits: stalledUnits, FanOutWorkflow: p.fanOutWorkflow, FanOutInstanceID: p.fanOutInstanceID}
	base := message.NewBaseMessage(msg.Schema(), msg, "gateddag-executor")
	data, err := json.Marshal(base)
	if err != nil {
		return fmt.Errorf("marshal stall event: %w", err)
	}
	if err := p.nc.Publish(ctx, p.subject, data); err != nil {
		return fmt.Errorf("publish stall event to %s: %w", p.subject, err)
	}
	return nil
}
