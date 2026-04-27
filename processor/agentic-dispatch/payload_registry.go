package agenticdispatch

import (
	"fmt"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/payloadregistry"
)

func buildSignalMessage(fields map[string]any) (any, error) {
	msg := &SignalMessage{}

	if v, ok := fields["loop_id"].(string); ok {
		msg.LoopID = v
	}
	if v, ok := fields["type"].(string); ok {
		msg.Type = v
	}
	if v, ok := fields["reason"].(string); ok {
		msg.Reason = v
	}

	// Handle timestamp
	if v, ok := fields["timestamp"].(string); ok {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			msg.Timestamp = t
		}
	}

	if err := msg.Validate(); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	return msg, nil
}

// init registers dispatch payload types with the global PayloadRegistry.
func init() {
	// Register SignalMessage payload factory
	err := payloadregistry.Register(&payloadregistry.Registration{
		Domain:      agentic.Domain,
		Category:    agentic.CategorySignalMessage,
		Version:     agentic.SchemaVersion,
		Description: "Control signal sent to a loop",
		Factory:     func() any { return &SignalMessage{} },
		Builder:     buildSignalMessage,
	})
	if err != nil {
		panic("failed to register SignalMessage payload: " + err.Error())
	}
}
