package agenticmemory

import (
	"context"
	"sync/atomic"
	"time"

	operatingmodel "github.com/c360studio/semstreams/agentic/operating-model"
)

// handleLayerApproved processes operating-model layer-approved events published by
// /onboard on agent.operating_model.layer_approved.*. It unwraps the BaseMessage
// envelope, validates the payload, converts it to graph triples, and publishes a
// graph mutation on graph.mutation.{loopID}.
//
// Errors are logged and counted; they never fail the upstream publisher, in keeping
// with teams-memory's "memory is supplementary" error policy.
func (c *Component) handleLayerApproved(ctx context.Context, data []byte) {
	payload, ok := c.unmarshalLayerApproved(data)
	if !ok {
		return
	}

	// Validate before ctx check to mirror sibling handlers (handleCompactionEvent
	// at handlers.go:37-48) — a cancelled-but-invalid payload still logs the
	// validation failure so operators see the diagnostic.
	if err := payload.Validate(); err != nil {
		c.logger.Error("Invalid layer_approved payload",
			"loop_id", payload.LoopID,
			"user_id", payload.UserID,
			"layer", payload.Layer,
			"error", err)
		atomic.AddInt64(&c.errors, 1)
		return
	}

	if ctx.Err() != nil {
		c.logger.Debug("Context cancelled before processing layer_approved",
			"loop_id", payload.LoopID)
		return
	}

	if c.platform.Org == "" || c.platform.Platform == "" {
		c.logger.Warn("Cannot write layer_approved triples: platform identity missing",
			"loop_id", payload.LoopID,
			"user_id", payload.UserID,
			"layer", payload.Layer,
			"org", c.platform.Org,
			"platform", c.platform.Platform)
		atomic.AddInt64(&c.errors, 1)
		return
	}

	triples := payload.Triples(c.platform.Org, c.platform.Platform)
	if len(triples) == 0 {
		c.logger.Debug("layer_approved produced no triples",
			"loop_id", payload.LoopID, "layer", payload.Layer)
		return
	}

	if err := c.publishGraphMutations(ctx, payload.LoopID, "add_triples", triples); err != nil {
		c.logger.Error("Failed to publish layer_approved graph mutations",
			"loop_id", payload.LoopID, "layer", payload.Layer, "error", err)
		atomic.AddInt64(&c.errors, 1)
		return
	}

	c.recordLayerApprovedSuccess(payload, len(triples))
}

// recordLayerApprovedSuccess emits the success log and updates metrics.
// Kept separate so handleLayerApproved stays under revive's function-length cap.
func (c *Component) recordLayerApprovedSuccess(payload *operatingmodel.LayerApproved, tripleCount int) {
	c.logger.Debug("Published layer_approved triples",
		"loop_id", payload.LoopID,
		"user_id", payload.UserID,
		"layer", payload.Layer,
		"profile_version", payload.ProfileVersion,
		"triple_count", tripleCount)
	atomic.AddInt64(&c.eventsProcessed, 1)
	c.mu.Lock()
	c.lastActivity = time.Now()
	c.mu.Unlock()
}

// unmarshalLayerApproved decodes a BaseMessage envelope and asserts the payload type.
// Returns (payload, true) on success and (nil, false) on any failure after logging.
func (c *Component) unmarshalLayerApproved(data []byte) (*operatingmodel.LayerApproved, bool) {
	baseMsg, err := c.decoder.Decode(data)
	if err != nil {
		c.logger.Error("Failed to unmarshal layer_approved BaseMessage", "error", err)
		atomic.AddInt64(&c.errors, 1)
		return nil, false
	}

	raw := baseMsg.Payload()
	if raw == nil {
		c.logger.Error("layer_approved BaseMessage has nil payload",
			"type", baseMsg.Type().String())
		atomic.AddInt64(&c.errors, 1)
		return nil, false
	}
	payload, ok := raw.(*operatingmodel.LayerApproved)
	if !ok {
		c.logger.Error("Unexpected layer_approved payload type",
			"type", baseMsg.Type().String())
		atomic.AddInt64(&c.errors, 1)
		return nil, false
	}
	return payload, true
}
