package agenticmemory

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/errs"
)

// Publisher defines the interface for publishing messages
type Publisher interface {
	Publish(ctx context.Context, subject string, data []byte) error
}

// InjectedContextMessage represents context injected back to the agent loop
type InjectedContextMessage struct {
	LoopID     string `json:"loop_id"`
	Context    string `json:"context"`
	TokenCount int    `json:"token_count"`
	Source     string `json:"source"`    // "post_compaction", "pre_task"
	Timestamp  int64  `json:"timestamp"` // Unix timestamp
}

// GraphMutationMessage represents a graph mutation command
type GraphMutationMessage struct {
	Operation string           `json:"operation"` // "add_triples", "delete_triples"
	LoopID    string           `json:"loop_id"`
	Triples   []message.Triple `json:"triples"`
	Timestamp int64            `json:"timestamp"` // Unix timestamp
}

// CheckpointEventMessage represents a checkpoint creation event
type CheckpointEventMessage struct {
	LoopID       string `json:"loop_id"`
	CheckpointID string `json:"checkpoint_id"`
	Bucket       string `json:"bucket"`
	Timestamp    int64  `json:"timestamp"` // Unix timestamp
}

// publishInjectedContext publishes hydrated context back to the agent loop
func (c *Component) publishInjectedContext(ctx context.Context, loopID, source string, hydrated *HydratedContext) error {
	// Validate inputs
	if loopID == "" {
		return errs.WrapInvalid(
			fmt.Errorf("loopID cannot be empty"),
			"Component",
			"publishInjectedContext",
			"validate loopID",
		)
	}
	if hydrated == nil {
		return errs.WrapInvalid(
			fmt.Errorf("hydrated context cannot be nil"),
			"Component",
			"publishInjectedContext",
			"validate hydrated context",
		)
	}

	// Check context cancellation
	if err := ctx.Err(); err != nil {
		return err
	}

	// Build message
	msg := InjectedContextMessage{
		LoopID:     loopID,
		Context:    hydrated.Context,
		TokenCount: hydrated.TokenCount,
		Source:     source,
		Timestamp:  time.Now().Unix(),
	}

	// Marshal to JSON
	data, err := json.Marshal(msg)
	if err != nil {
		return errs.Wrap(err, "Component", "publishInjectedContext", "marshal injected context message")
	}

	// Build subject from output port config, falling back to default pattern
	subject := component.ResolveSubject(c.outputPortDefs(), "injected_context", loopID)

	// Publish if NATS client available
	if c.natsClient != nil {
		if err := c.natsClient.Publish(ctx, subject, data); err != nil {
			return errs.WrapTransient(err, "Component", "publishInjectedContext", "publish injected context")
		}
		c.logger.Debug("Published injected context",
			"loop_id", loopID,
			"source", source,
			"subject", subject)
	}

	return nil
}

// publishGraphMutations publishes graph mutation commands
func (c *Component) publishGraphMutations(ctx context.Context, loopID, operation string, triples []message.Triple) error {
	// Validate inputs
	if loopID == "" {
		return errs.WrapInvalid(
			fmt.Errorf("loopID cannot be empty"),
			"Component",
			"publishGraphMutations",
			"validate loopID",
		)
	}

	// Check context cancellation
	if err := ctx.Err(); err != nil {
		return err
	}

	// Build message
	msg := GraphMutationMessage{
		Operation: operation,
		LoopID:    loopID,
		Triples:   triples,
		Timestamp: time.Now().Unix(),
	}

	// Marshal to JSON
	data, err := json.Marshal(msg)
	if err != nil {
		return errs.Wrap(err, "Component", "publishGraphMutations", "marshal graph mutation message")
	}

	// Subject resolved via output port — defaults to graph.mutation.{loopID}
	// when no port override is configured.
	subject := component.ResolveSubject(c.outputPortDefs(), "graph_mutations", loopID)

	// Publish if NATS client available
	if c.natsClient != nil {
		if err := c.natsClient.Publish(ctx, subject, data); err != nil {
			return errs.WrapTransient(err, "Component", "publishGraphMutations", "publish graph mutation")
		}
		c.logger.Debug("Published graph mutation",
			"loop_id", loopID,
			"operation", operation,
			"triple_count", len(triples),
			"subject", subject)
	}

	return nil
}

// publishCheckpointEvent publishes a checkpoint creation event
func (c *Component) publishCheckpointEvent(ctx context.Context, loopID, checkpointID string) error {
	// Validate inputs
	if loopID == "" {
		return errs.WrapInvalid(
			fmt.Errorf("loopID cannot be empty"),
			"Component",
			"publishCheckpointEvent",
			"validate loopID",
		)
	}
	if checkpointID == "" {
		return errs.WrapInvalid(
			fmt.Errorf("checkpointID cannot be empty"),
			"Component",
			"publishCheckpointEvent",
			"validate checkpointID",
		)
	}

	// Check context cancellation
	if err := ctx.Err(); err != nil {
		return err
	}

	// Build message
	msg := CheckpointEventMessage{
		LoopID:       loopID,
		CheckpointID: checkpointID,
		Bucket:       c.config.Checkpoint.StorageBucket,
		Timestamp:    time.Now().Unix(),
	}

	// Marshal to JSON
	data, err := json.Marshal(msg)
	if err != nil {
		return errs.Wrap(err, "Component", "publishCheckpointEvent", "marshal checkpoint event message")
	}

	// Subject resolved via output port — defaults to memory.checkpoint.created.{loopID}
	// when no port override is configured.
	subject := component.ResolveSubject(c.outputPortDefs(), "checkpoint_events", loopID)

	// Publish if NATS client available
	if c.natsClient != nil {
		if err := c.natsClient.Publish(ctx, subject, data); err != nil {
			return errs.WrapTransient(err, "Component", "publishCheckpointEvent", "publish checkpoint event")
		}
		c.logger.Debug("Published checkpoint event",
			"loop_id", loopID,
			"checkpoint_id", checkpointID,
			"subject", subject)
	}

	return nil
}
