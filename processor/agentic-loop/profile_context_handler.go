package agenticloop

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"

	operatingmodel "github.com/c360studio/semstreams/agentic/operating-model"
)

// handleProfileContextMessage consumes operating_model.profile_context.v1
// events published by teams-memory on agent.context.profile.{loop_id}. The
// rendered preamble is added to RegionHydratedContext so the model sees a
// "How this user works" section ahead of recent conversation history.
//
// Injection is gated by Config.Context.InjectProfileContext — deployments that
// don't run teams-memory's profile-context publisher can leave it off without
// side-effect.
//
// Known limitations (v1):
//
//   - **First-iteration race.** The agent.created → profile_context round-trip
//     is asynchronous, so the first iteration's LLM request may go out before
//     the preamble lands. Subsequent iterations pick it up via
//     RegionHydratedContext. Single-shot tasks that finish in one LLM call
//     may miss the injection.
//   - **Re-delivery is not deduplicated.** If JetStream re-delivers a
//     profile_context after a loop restart, it will be injected again —
//     inflating RegionHydratedContext until the next compaction reclaims it.
//     A future phase can dedupe by ProfileVersion on the context-manager side.
//   - **Unknown-loop is expected** for either a GC'd loop or a profile_context
//     that arrives after loop completion; both paths log Warn and return.
func (c *Component) handleProfileContextMessage(ctx context.Context, data []byte) {
	if !c.config.Context.InjectProfileContext {
		c.logger.Debug("profile_context received but inject_profile_context disabled")
		return
	}

	payload, ok := c.unmarshalProfileContext(data)
	if !ok {
		return
	}

	if !payload.HasOperatingModel() {
		// Empty profile (user hasn't onboarded yet) — valid publish, nothing to inject.
		c.logger.DebugContext(ctx, "profile_context has empty operating model; skipping",
			slog.String("loop_id", payload.LoopID),
			slog.String("user_id", payload.UserID))
		return
	}

	// Handler is wired during Start(). Guard against the (narrow) shutdown-race
	// window where a subscription delivers mid-teardown.
	if c.handler == nil {
		c.logger.WarnContext(ctx, "profile_context received before handler initialized",
			slog.String("loop_id", payload.LoopID))
		return
	}
	cm := c.handler.GetContextManager(payload.LoopID)
	if cm == nil {
		// Either the loop was GC'd or the event arrived after completion —
		// both expected paths, not errors.
		c.logger.WarnContext(ctx, "profile_context arrived for unknown loop",
			slog.String("loop_id", payload.LoopID))
		return
	}

	preamble := payload.SystemPromptPreamble()
	// AddMessage's error is checked (unlike the fire-and-forget convention in
	// handlers.go) because a failed inject silently drops the onboarding
	// preamble — worth surfacing when the region is misrouted or out of budget.
	if err := cm.AddMessage(RegionHydratedContext, agentic.ChatMessage{
		Role:    "system",
		Content: preamble,
	}); err != nil {
		c.logger.ErrorContext(ctx, "failed to inject profile_context",
			slog.String("loop_id", payload.LoopID),
			slog.Any("error", err))
		return
	}

	c.logger.DebugContext(ctx, "profile_context injected",
		slog.String("loop_id", payload.LoopID),
		slog.String("user_id", payload.UserID),
		slog.Int("entries", payload.OperatingModel.EntryCount),
		slog.Int("tokens", payload.OperatingModel.TokenCount))
}

// unmarshalProfileContext decodes a BaseMessage envelope and type-asserts the
// ProfileContext payload. Returns (nil, false) after logging on any failure.
func (c *Component) unmarshalProfileContext(data []byte) (*operatingmodel.ProfileContext, bool) {
	var baseMsg message.BaseMessage
	if err := json.Unmarshal(data, &baseMsg); err != nil {
		c.logger.Error("Failed to unmarshal profile_context BaseMessage", "error", err)
		return nil, false
	}
	raw := baseMsg.Payload()
	if raw == nil {
		c.logger.Error("profile_context BaseMessage has nil payload",
			"type", baseMsg.Type().String())
		return nil, false
	}
	payload, ok := raw.(*operatingmodel.ProfileContext)
	if !ok {
		c.logger.Error("Unexpected profile_context payload type",
			"type", baseMsg.Type().String())
		return nil, false
	}
	return payload, true
}
