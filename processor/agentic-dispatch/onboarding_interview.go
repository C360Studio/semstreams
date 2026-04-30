package agenticdispatch

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/google/uuid"

	operatingmodel "github.com/c360studio/semstreams/agentic/operating-model"
)

// Onboarding sub-states carried on LoopInfo.Metadata. Sub-state is orthogonal to
// WorkflowStep: WorkflowStep tracks which layer the user is on, sub-state
// tracks where we are within a single layer's Q → approve → save cycle.
const (
	// OnboardMetaSubState is the Metadata key under which the interview
	// sub-state lives (see SubState* constants).
	OnboardMetaSubState = "onboard_substate"

	// OnboardMetaDraftEntries is the Metadata key under which the most-recent
	// normalized draft entries live while awaiting user approval.
	OnboardMetaDraftEntries = "onboard_draft_entries"

	// OnboardMetaDraftSummary is the Metadata key under which the most-recent
	// checkpoint summary lives while awaiting user approval.
	OnboardMetaDraftSummary = "onboard_draft_summary"
)

// Onboarding sub-state values.
const (
	// SubStateAwaitingAnswer means the handler is waiting for the user's
	// freeform response to the current layer's opening question.
	SubStateAwaitingAnswer = "awaiting_answer"

	// SubStateAwaitingApproval means the handler has normalized the user's
	// answer into draft entries and is waiting for "approve" to save them.
	SubStateAwaitingApproval = "awaiting_approval"
)

// onboardingLayerApprovedSubject returns the NATS subject used to publish
// operating_model.layer_approved events. The user_id is the routing key so
// agentic-memory can filter per-user later without changing the subject shape.
func onboardingLayerApprovedSubject(userID string) string {
	return fmt.Sprintf("teams.operating_model.layer_approved.%s", userID)
}

// isOnboardingInFlight reports whether the user's active loop on this channel
// is an in-flight onboarding interview.
func (c *Component) isOnboardingInFlight(userID, channelID string) bool {
	loopID := c.loopTracker.GetActiveLoop(userID, channelID)
	if loopID == "" {
		return false
	}
	info := c.loopTracker.Get(loopID)
	if info == nil {
		return false
	}
	return info.WorkflowSlug == OnboardingWorkflowSlug && !isTerminalState(info.State)
}

// handleOnboardingTurn routes a non-slash user message inside an active
// onboarding loop. Dispatched from handleUserMessage ahead of the intent
// classifier or task submission.
//
// Contract: the caller has already verified isOnboardingInFlight(msg.UserID,
// msg.ChannelID) is true. handleOnboardingTurn re-reads the tracker so the
// caller never touches LoopInfo directly.
func (c *Component) handleOnboardingTurn(ctx context.Context, msg agentic.UserMessage) {
	loopID := c.loopTracker.GetActiveLoop(msg.UserID, msg.ChannelID)
	// snapshotLoop returns a deep copy so downstream reads never race with a
	// sibling goroutine mutating the tracker (e.g. /cancel, a parallel turn).
	info := snapshotLoop(c.loopTracker, loopID)
	if info == nil || info.WorkflowSlug != OnboardingWorkflowSlug {
		c.logger.WarnContext(ctx, "onboarding turn: loop disappeared between intercept and handle",
			slog.String("user_id", msg.UserID),
			slog.String("channel_id", msg.ChannelID))
		return
	}

	sub := onboardSubState(info)
	switch sub {
	case SubStateAwaitingAnswer:
		c.onboardingRecordAnswer(ctx, msg, info)
	case SubStateAwaitingApproval:
		if isApprovalText(msg.Content) {
			c.onboardingApproveLayer(ctx, msg, info)
		} else {
			// Treat as a replacement answer.
			c.onboardingRecordAnswer(ctx, msg, info)
		}
	default:
		c.logger.WarnContext(ctx, "onboarding turn: unknown sub-state",
			slog.String("loop_id", info.LoopID),
			slog.String("substate", sub))
	}
}

// onboardingRecordAnswer captures the user's freeform answer for the current
// layer, runs it through the stub normalizer, stashes the draft on the
// tracker, and sends a checkpoint response asking for approval.
//
// Empty answers are rejected without advancing sub-state — the user stays in
// awaiting_answer and is asked to try again.
func (c *Component) onboardingRecordAnswer(ctx context.Context, msg agentic.UserMessage, info *LoopInfo) {
	entries := c.normalizeLayerAnswer(ctx, info.WorkflowStep, msg.Content)
	if len(entries) == 0 {
		c.sendResponse(ctx, agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			InReplyTo:   info.LoopID,
			Type:        agentic.ResponseTypePrompt,
			Content:     "I didn't catch an answer. Try again — describe what matters for this layer.",
			Timestamp:   time.Now(),
		})
		return
	}
	summary := layerCheckpointSummary(info.WorkflowStep, entries)

	updateMetadata(c.loopTracker, info.LoopID, map[string]any{
		OnboardMetaSubState:     SubStateAwaitingApproval,
		OnboardMetaDraftEntries: entries,
		OnboardMetaDraftSummary: summary,
	})

	c.logger.DebugContext(ctx, "onboarding answer recorded",
		slog.String("loop_id", info.LoopID),
		slog.String("layer", info.WorkflowStep),
		slog.Int("draft_entry_count", len(entries)))

	c.sendResponse(ctx, agentic.UserResponse{
		ResponseID:  uuid.New().String(),
		ChannelType: msg.ChannelType,
		ChannelID:   msg.ChannelID,
		UserID:      msg.UserID,
		InReplyTo:   info.LoopID,
		Type:        agentic.ResponseTypePrompt,
		Content:     renderCheckpointForApproval(info.WorkflowStep, summary, entries),
		Timestamp:   time.Now(),
	})
}

// onboardingApproveLayer publishes the LayerApproved payload, advances the
// WorkflowStep to the next layer, and sends either the next layer's opening
// question or a completion message.
func (c *Component) onboardingApproveLayer(ctx context.Context, msg agentic.UserMessage, info *LoopInfo) {
	entries, ok := draftEntriesFromMetadata(info.Metadata)
	if !ok || len(entries) == 0 {
		c.logger.ErrorContext(ctx, "onboarding approve: no draft entries to save",
			slog.String("loop_id", info.LoopID),
			slog.String("layer", info.WorkflowStep))
		c.sendResponse(ctx, agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			InReplyTo:   info.LoopID,
			Type:        agentic.ResponseTypeError,
			Content:     "Nothing to approve yet — tell me about this layer first.",
			Timestamp:   time.Now(),
		})
		return
	}

	summary := metaString(info.Metadata, OnboardMetaDraftSummary)
	profileVersion := metaInt(info.Metadata, OnboardMetaProfileVersion, 1)

	payload := &operatingmodel.LayerApproved{
		UserID:            msg.UserID,
		LoopID:            info.LoopID,
		Layer:             info.WorkflowStep,
		ProfileVersion:    profileVersion,
		CheckpointSummary: summary,
		Entries:           entries,
		ApprovedAt:        time.Now().UTC(),
	}
	if err := payload.Validate(); err != nil {
		c.logger.ErrorContext(ctx, "onboarding approve: built payload failed validation",
			slog.String("loop_id", info.LoopID),
			slog.String("layer", info.WorkflowStep),
			slog.Any("error", err))
		c.sendResponse(ctx, agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			InReplyTo:   info.LoopID,
			Type:        agentic.ResponseTypeError,
			Content:     "Couldn't save this layer — its contents were invalid. Please re-answer.",
			Timestamp:   time.Now(),
		})
		return
	}

	if err := c.publishLayerApproved(ctx, payload); err != nil {
		c.logger.ErrorContext(ctx, "onboarding approve: publish failed",
			slog.String("loop_id", info.LoopID),
			slog.String("layer", info.WorkflowStep),
			slog.Any("error", err))
		c.sendResponse(ctx, agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			InReplyTo:   info.LoopID,
			Type:        agentic.ResponseTypeError,
			Content:     "Couldn't save this layer — try again in a moment.",
			Timestamp:   time.Now(),
		})
		return
	}

	// At-least-once semantics: the LayerApproved event is now on the wire.
	// If a crash or concurrent cancellation intervenes before we advance the
	// tracker, the guarded advanceToNextLayer / finalizeOnboardingLoop
	// checks below prevent skipping ahead on a stale pointer, and agentic-memory
	// must tolerate a duplicate write on restart replay.
	recordLayerApproved(c.loopTracker, info.LoopID)
	c.advanceAfterApproval(ctx, msg, info)
}

// advanceAfterApproval moves the loop to the next layer (or marks it complete)
// and sends the appropriate response. Split from onboardingApproveLayer to
// keep each function under the revive length cap.
//
// Re-reads a fresh snapshot so all state decisions use post-publish values,
// not the stale `info` captured before publishLayerApproved ran.
func (c *Component) advanceAfterApproval(ctx context.Context, msg agentic.UserMessage, info *LoopInfo) {
	current := snapshotLoop(c.loopTracker, info.LoopID)
	if current == nil {
		// Loop was cancelled between publish and advance; the LayerApproved
		// event still went out, but we have nothing to advance. Silent.
		c.logger.WarnContext(ctx, "onboarding advance: loop disappeared post-publish",
			slog.String("loop_id", info.LoopID))
		return
	}

	nextLayer, ok := nextLayerAfter(current.WorkflowStep)
	if !ok {
		if finalizeOnboardingLoop(c.loopTracker, current.LoopID, current.WorkflowStep) {
			c.sendResponse(ctx, agentic.UserResponse{
				ResponseID:  uuid.New().String(),
				ChannelType: msg.ChannelType,
				ChannelID:   msg.ChannelID,
				UserID:      msg.UserID,
				InReplyTo:   current.LoopID,
				Type:        agentic.ResponseTypeStatus,
				Content: "All five layers saved. Your operating model is now live and will be " +
					"injected into future agent conversations. You can re-run /onboard to " +
					"refresh it any time.",
				Timestamp: time.Now(),
			})
		}
		return
	}

	layerOrder := metaInt(current.Metadata, OnboardMetaLayerOrder, 1) + 1
	if !advanceToNextLayer(c.loopTracker, current.LoopID, current.WorkflowStep, nextLayer, layerOrder) {
		c.logger.WarnContext(ctx, "onboarding advance: tracker mutated between snapshot and advance",
			slog.String("loop_id", current.LoopID),
			slog.String("from", current.WorkflowStep),
			slog.String("to", nextLayer))
		return
	}

	c.sendResponse(ctx, agentic.UserResponse{
		ResponseID:  uuid.New().String(),
		ChannelType: msg.ChannelType,
		ChannelID:   msg.ChannelID,
		UserID:      msg.UserID,
		InReplyTo:   current.LoopID,
		Type:        agentic.ResponseTypePrompt,
		Content:     OnboardingOpeningQuestion(nextLayer),
		Timestamp:   time.Now(),
	})
}

// publishLayerApproved wraps the payload in a BaseMessage envelope and
// publishes it to the JetStream AGENT stream.
func (c *Component) publishLayerApproved(ctx context.Context, payload *operatingmodel.LayerApproved) error {
	baseMsg := message.NewBaseMessage(payload.Schema(), payload, "agentic-dispatch")
	data, err := json.Marshal(baseMsg)
	if err != nil {
		return fmt.Errorf("marshal layer_approved: %w", err)
	}
	subject := onboardingLayerApprovedSubject(payload.UserID)
	if c.natsClient == nil {
		// Unit-test harness may leave NATS unwired. Logged at Info so a
		// production miswire is diagnostic-worthy rather than hidden in Debug.
		c.logger.InfoContext(ctx, "layer_approved publish skipped (no NATS client)",
			slog.String("subject", subject))
		return nil
	}
	return c.natsClient.PublishToStream(ctx, subject, data)
}

// NormalizeLayerAnswer converts a user's freeform answer for a layer into one
// or more structured entries. This v1 implementation is a deterministic stub:
// it produces a single entry whose title is the answer's first line and whose
// summary is the full answer.
//
// A future phase will replace this with an LLM-assisted extraction that emits
// multiple fine-grained entries with cadence/trigger/inputs/etc. populated.
// The stub exists so the full onboarding pipeline is end-to-end testable
// before the LLM integration lands.
func NormalizeLayerAnswer(layer, answer string) []operatingmodel.Entry {
	trimmed := strings.TrimSpace(answer)
	if trimmed == "" {
		return nil
	}
	title := firstLine(trimmed)
	if len(title) > 80 {
		title = title[:77] + "..."
	}
	return []operatingmodel.Entry{{
		EntryID:          entryID(layer),
		Title:            title,
		Summary:          trimmed,
		SourceConfidence: operatingmodel.ConfidenceConfirmed,
		Status:           operatingmodel.StatusActive,
	}}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i > 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

func entryID(layer string) string {
	return fmt.Sprintf("om-%s-%s", layer, uuid.New().String()[:8])
}

// layerCheckpointSummary builds a terse human-readable summary describing how
// many entries were captured. Used in both the approval prompt and the
// ultimate LayerApproved payload.
func layerCheckpointSummary(layer string, entries []operatingmodel.Entry) string {
	if len(entries) == 0 {
		return fmt.Sprintf("No %s entries captured.", humanLayerName(layer))
	}
	if len(entries) == 1 {
		return fmt.Sprintf("Captured 1 %s entry.", humanLayerName(layer))
	}
	return fmt.Sprintf("Captured %d %s entries.", len(entries), humanLayerName(layer))
}

// humanLayerName turns a canonical layer name into a short human phrase for
// status messages.
func humanLayerName(layer string) string {
	return strings.ReplaceAll(layer, "_", " ")
}

// renderCheckpointForApproval produces the UserResponse body shown to the user
// before they approve a layer's draft entries.
func renderCheckpointForApproval(layer, summary string, entries []operatingmodel.Entry) string {
	var b strings.Builder
	fmt.Fprintf(&b, "**%s checkpoint — reply `approve` to save, or retype to redo.**\n\n",
		humanLayerName(layer))
	b.WriteString(summary)
	b.WriteString("\n\n")
	for i, e := range entries {
		fmt.Fprintf(&b, "%d. %s\n   %s\n", i+1, e.Title, e.Summary)
	}
	return b.String()
}

// isApprovalText reports whether the user's message is intended as approval
// for a layer checkpoint. Case-insensitive match against the standard terms.
func isApprovalText(content string) bool {
	switch strings.ToLower(strings.TrimSpace(content)) {
	case "approve", "yes", "y", "ok", "okay", "accept", "save":
		return true
	}
	return false
}

// nextLayerAfter returns the layer that follows current in the canonical
// interview order, plus true if one exists.
func nextLayerAfter(current string) (string, bool) {
	layers := operatingmodel.Layers()
	for i, l := range layers {
		if l == current && i+1 < len(layers) {
			return layers[i+1], true
		}
	}
	return "", false
}
