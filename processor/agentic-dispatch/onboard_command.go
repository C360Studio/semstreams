package agenticdispatch

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/google/uuid"

	operatingmodel "github.com/c360studio/semstreams/agentic/operating-model"
)

// OnboardingWorkflowSlug identifies a loop driving the operating-model
// interview. Shared with the interview message handler (next phase) so both
// sides agree on the slug a loop is tagged with.
const OnboardingWorkflowSlug = "onboarding"

// OnboardingLoopStatePending is the state used while an onboarding loop is
// waiting on the next user turn. We deliberately reuse "pending" so existing
// commands like /status and /cancel continue to work on onboarding loops
// without special-casing.
const OnboardingLoopStatePending = "pending"

// Metadata keys used by the onboarding loop and consumed by the interview
// handler in Phase 4. Constants ensure both writers and readers agree.
const (
	OnboardMetaProfileVersion = "profile_version"
	OnboardMetaLayerOrder     = "layer_order"
)

// OnboardingOpeningQuestion returns the user-facing prompt that starts a
// specific layer. Exported so the interview message handler can produce the
// same transition text when advancing between layers.
//
// Panics if layer is not one of the canonical five layer names; the caller is
// responsible for passing a validated layer name.
func OnboardingOpeningQuestion(layer string) string {
	switch layer {
	case operatingmodel.LayerOperatingRhythms:
		return "Let's start with your **operating rhythms**. " +
			"What recurring blocks on your calendar are load-bearing for how you work — " +
			"planning, stand-ups, deep-work, reviews? When do they happen and what makes them work?"
	case operatingmodel.LayerRecurringDecisions:
		return "Now **recurring decisions**. " +
			"What decisions do you make on a regular schedule — weekly prioritization, " +
			"go/no-go calls, quarterly planning? How do you make them and what inputs do you rely on?"
	case operatingmodel.LayerDependencies:
		return "**Dependencies** next. " +
			"Which people, systems, or inputs does your work depend on? " +
			"Where do you get blocked when one of them is unavailable?"
	case operatingmodel.LayerInstitutionalKnowledge:
		return "**Institutional knowledge** — the stuff that isn't written down. " +
			"What do you know about how your organization works that a newcomer would need a year to learn?"
	case operatingmodel.LayerFriction:
		return "Finally, **friction**. " +
			"Where does the work get stuck today? Name the specific places where your flow breaks down."
	default:
		panic(fmt.Sprintf("OnboardingOpeningQuestion: unknown layer %q", layer))
	}
}

// handleOnboardCommand creates a new onboarding-scoped loop for the user and
// returns the opening question for the first layer. Subsequent user messages
// on this loop are routed into the layer-interview handler (see Phase 4).
//
// The loop is tracked locally in agentic-dispatch and does not publish a
// TaskMessage to agentic-loop — the interview's layer-normalization LLM calls
// are one-off rather than a continuous agent loop.
//
// If the user already has a non-terminal loop on this channel, /onboard
// refuses with a friendly error so the tracker's "most recent loop wins"
// semantics don't silently hijack an in-flight task loop.
func (c *Component) handleOnboardCommand(ctx context.Context, msg agentic.UserMessage, _ []string, _ string) (agentic.UserResponse, error) {
	// Reject only when there's an active loop on THIS channel. The tracker's
	// GetActiveLoop falls back to the user's most-recent loop across channels,
	// so we verify the returned loop actually belongs to msg.ChannelID before
	// blocking — users running /onboard in a fresh session must not be
	// blocked by a task loop on an older session.
	if active := c.loopTracker.GetActiveLoop(msg.UserID, msg.ChannelID); active != "" {
		if info := c.loopTracker.Get(active); info != nil && info.ChannelID == msg.ChannelID {
			return agentic.UserResponse{
				ResponseID:  uuid.New().String(),
				ChannelType: msg.ChannelType,
				ChannelID:   msg.ChannelID,
				UserID:      msg.UserID,
				InReplyTo:   active,
				Type:        agentic.ResponseTypeError,
				Content: fmt.Sprintf(
					"Cannot start onboarding: loop %s is still active on this channel. "+
						"Use /cancel %s first, or wait for it to complete.",
					active, active),
				Timestamp: time.Now(),
			}, nil
		}
	}

	newLoopID := "loop_" + uuid.New().String()[:8]
	firstLayer := operatingmodel.LayerOperatingRhythms
	profileVersion := c.nextProfileVersion(ctx, msg.UserID)

	c.loopTracker.Track(&LoopInfo{
		LoopID:        newLoopID,
		Role:          c.config.DefaultRole,
		UserID:        msg.UserID,
		ChannelType:   msg.ChannelType,
		ChannelID:     msg.ChannelID,
		State:         OnboardingLoopStatePending,
		MaxIterations: len(operatingmodel.Layers()),
		WorkflowSlug:  OnboardingWorkflowSlug,
		WorkflowStep:  firstLayer,
		CreatedAt:     time.Now(),
		Metadata: map[string]any{
			OnboardMetaProfileVersion: profileVersion,
			OnboardMetaLayerOrder:     1,
		},
	})

	if c.metrics != nil {
		c.metrics.recordLoopStarted()
	}

	c.logger.DebugContext(ctx, "Onboarding started",
		slog.String("loop_id", newLoopID),
		slog.String("user_id", msg.UserID),
		slog.String("layer", firstLayer),
		slog.Int("profile_version", profileVersion))

	return agentic.UserResponse{
		ResponseID:  uuid.New().String(),
		ChannelType: msg.ChannelType,
		ChannelID:   msg.ChannelID,
		UserID:      msg.UserID,
		InReplyTo:   newLoopID,
		Type:        agentic.ResponseTypePrompt,
		Content:     OnboardingOpeningQuestion(firstLayer),
		Timestamp:   time.Now(),
	}, nil
}

// nextProfileVersion returns the ProfileVersion to stamp on a new /onboard
// loop's metadata. First-time onboarders get 1; re-runs get prior+1 so the
// `user.operating_model.version` triple records monotonically increasing
// versions across re-interviews.
//
// Falls back to 1 when no profile reader is wired (default
// EmptyProfileReader), when the user has no prior profile, or when the
// reader returns an error — the version-bump is best-effort and must not
// block /onboard from starting.
func (c *Component) nextProfileVersion(ctx context.Context, userID string) int {
	prior, err := c.getProfileReader().ReadProfileVersion(ctx, c.deps.Platform.Org, c.deps.Platform.Platform, userID)
	if err != nil {
		c.logger.WarnContext(ctx, "onboard: prior profile version read failed; defaulting to 1",
			slog.String("user_id", userID),
			slog.Any("error", err))
		return 1
	}
	if prior < 1 {
		return 1
	}
	return prior + 1
}
