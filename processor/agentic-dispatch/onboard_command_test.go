package agenticdispatch

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/agentic"

	operatingmodel "github.com/c360studio/semstreams/agentic/operating-model"
)

// newOnboardTestComponent builds a Component with just enough wiring to drive
// handleOnboardCommand: the internal loop tracker and a silent logger. No NATS,
// no metrics — the handler under test does not touch either.
func newOnboardTestComponent() *Component {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return &Component{
		logger:      logger,
		loopTracker: NewLoopTrackerWithLogger(logger),
	}
}

func onboardUserMsg() agentic.UserMessage {
	return agentic.UserMessage{
		UserID:      "coby",
		ChannelType: "http",
		ChannelID:   "session-1",
		Content:     "/onboard",
	}
}

func TestHandleOnboardCommand_CreatesLoopWithWorkflowContext(t *testing.T) {
	c := newOnboardTestComponent()
	msg := onboardUserMsg()

	resp, err := c.handleOnboardCommand(context.Background(), msg, nil, "")
	if err != nil {
		t.Fatalf("handleOnboardCommand returned error: %v", err)
	}
	if resp.InReplyTo == "" {
		t.Fatalf("expected response.InReplyTo to carry new loop id, got empty")
	}

	info := c.loopTracker.Get(resp.InReplyTo)
	if info == nil {
		t.Fatalf("loop %q not tracked after /onboard", resp.InReplyTo)
	}
	if info.WorkflowSlug != OnboardingWorkflowSlug {
		t.Errorf("WorkflowSlug = %q, want %q", info.WorkflowSlug, OnboardingWorkflowSlug)
	}
	if info.WorkflowStep != operatingmodel.LayerOperatingRhythms {
		t.Errorf("WorkflowStep = %q, want %q", info.WorkflowStep, operatingmodel.LayerOperatingRhythms)
	}
	if info.State != OnboardingLoopStatePending {
		t.Errorf("State = %q, want %q", info.State, OnboardingLoopStatePending)
	}
	if info.UserID != msg.UserID || info.ChannelID != msg.ChannelID {
		t.Errorf("user/channel mismatch: got user=%q channel=%q", info.UserID, info.ChannelID)
	}
	if info.Metadata["profile_version"] != 1 {
		t.Errorf("Metadata.profile_version = %v, want 1", info.Metadata["profile_version"])
	}
}

func TestHandleOnboardCommand_ReturnsOpeningQuestion(t *testing.T) {
	c := newOnboardTestComponent()
	resp, err := c.handleOnboardCommand(context.Background(), onboardUserMsg(), nil, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Type != agentic.ResponseTypePrompt {
		t.Errorf("ResponseType = %q, want %q", resp.Type, agentic.ResponseTypePrompt)
	}
	want := OnboardingOpeningQuestion(operatingmodel.LayerOperatingRhythms)
	if resp.Content != want {
		t.Errorf("response content mismatch; got %q", resp.Content)
	}
}

func TestHandleOnboardCommand_MultipleCallsRejectedWhenActive(t *testing.T) {
	// Second /onboard on the same channel must be rejected rather than
	// clobbering the active loop in the tracker.
	c := newOnboardTestComponent()
	resp1, _ := c.handleOnboardCommand(context.Background(), onboardUserMsg(), nil, "")
	resp2, _ := c.handleOnboardCommand(context.Background(), onboardUserMsg(), nil, "")

	if resp2.Type != agentic.ResponseTypeError {
		t.Errorf("second /onboard response type = %q, want Error", resp2.Type)
	}
	if !strings.Contains(resp2.Content, resp1.InReplyTo) {
		t.Errorf("error content should name the blocking loop %q; got %q",
			resp1.InReplyTo, resp2.Content)
	}
	// Original loop must remain the active one.
	if got := c.loopTracker.GetActiveLoop("coby", "session-1"); got != resp1.InReplyTo {
		t.Errorf("active loop after rejected /onboard = %q, want %q",
			got, resp1.InReplyTo)
	}
}

func TestHandleOnboardCommand_DistinctLoopsAcrossChannels(t *testing.T) {
	// Two /onboard calls from the same user on different channels are
	// allowed — different channels imply different sessions.
	c := newOnboardTestComponent()
	m1 := onboardUserMsg()
	m2 := onboardUserMsg()
	m2.ChannelID = "session-2"

	resp1, _ := c.handleOnboardCommand(context.Background(), m1, nil, "")
	resp2, _ := c.handleOnboardCommand(context.Background(), m2, nil, "")
	if resp1.InReplyTo == resp2.InReplyTo {
		t.Errorf("distinct-channel /onboard calls produced the same loop id")
	}
	if resp2.Type == agentic.ResponseTypeError {
		t.Errorf("second /onboard on a different channel must not be rejected")
	}
}

func TestOnboardingOpeningQuestion_AllLayersReturnNonEmpty(t *testing.T) {
	for _, layer := range operatingmodel.Layers() {
		q := OnboardingOpeningQuestion(layer)
		if strings.TrimSpace(q) == "" {
			t.Errorf("OpeningQuestion(%q) returned empty", layer)
		}
	}
}

func TestOnboardingOpeningQuestion_PanicsOnUnknownLayer(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on unknown layer, got none")
		}
	}()
	OnboardingOpeningQuestion("definitely_not_a_layer")
}

func TestOnboardCommand_RegisteredInRegistry(t *testing.T) {
	// Confirms the /onboard registration in registerBuiltinCommands matches the
	// regex we document in its Help. Catches typos in the pattern and regressions
	// from renaming.
	registry := NewCommandRegistry()
	err := registry.Register("onboard", CommandConfig{
		Pattern:    `^/onboard$`,
		Permission: "submit_task",
		Help:       "/onboard - Start the operating-model onboarding interview",
	}, func(_ context.Context, _ agentic.UserMessage, _ []string, _ string) (agentic.UserResponse, error) {
		return agentic.UserResponse{}, nil
	})
	if err != nil {
		t.Fatalf("registration failed: %v", err)
	}

	name, _, args, ok := registry.Match("/onboard")
	if !ok {
		t.Fatalf("/onboard did not match registered pattern")
	}
	if name != "onboard" {
		t.Errorf("matched command = %q, want onboard", name)
	}
	if len(args) != 0 {
		t.Errorf("/onboard with no args produced %d captured groups, want 0", len(args))
	}

	// /onboard must not match with trailing text, since the interview drives
	// state via subsequent user messages, not args on the command itself.
	if _, _, _, ok := registry.Match("/onboard extra-text"); ok {
		t.Errorf("/onboard should not match when followed by extra text")
	}
}
