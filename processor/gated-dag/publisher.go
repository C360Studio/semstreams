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

// Dispatch publishes the registry-wrapped DispatchMessage reference.
func (p *natsPublisher) Dispatch(ctx context.Context, unitID string) error {
	msg := &DispatchMessage{UnitEntityID: unitID, FanOutWorkflow: p.fanOutWorkflow}
	base := message.NewBaseMessage(msg.Schema(), msg, "gateddag-executor")
	data, err := json.Marshal(base)
	if err != nil {
		return fmt.Errorf("marshal dispatch message for %s: %w", unitID, err)
	}
	if err := p.nc.Publish(ctx, p.subject, data); err != nil {
		return fmt.Errorf("publish dispatch for %s to %s: %w", unitID, p.subject, err)
	}
	return nil
}
