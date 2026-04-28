package agenticloop

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadbuiltins"

	operatingmodel "github.com/c360studio/semstreams/agentic/operating-model"
)

// newProfileContextTestComponent constructs a Component with default config
// plus the optional inject_profile_context override. NATS is nil because the
// handler under test only reads from the context manager; no outbound
// publishes happen in this path.
func newProfileContextTestComponent(t *testing.T, inject bool) *Component {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Context.InjectProfileContext = inject
	rawCfg, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	deps := component.Dependencies{
		NATSClient:      nil,
		Logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		PayloadRegistry: payloadbuiltins.NewTestRegistry(t),
	}
	comp, err := NewComponent(rawCfg, deps)
	if err != nil {
		t.Fatalf("NewComponent: %v", err)
	}
	return comp.(*Component)
}

// seedLoop creates a loop in the component's loop manager so that the
// handler under test can find a context manager for it.
func seedLoop(t *testing.T, c *Component, loopID string) {
	t.Helper()
	if _, err := c.handler.loopManager.CreateLoopWithID(loopID, "task-"+loopID, "general", "test-model", 20); err != nil {
		t.Fatalf("CreateLoopWithID: %v", err)
	}
}

func validProfileContextPayload() *operatingmodel.ProfileContext {
	return &operatingmodel.ProfileContext{
		UserID:         "coby",
		LoopID:         "loop-abc",
		ProfileVersion: 1,
		OperatingModel: operatingmodel.ProfileContextSlice{
			Content:    "- Weekly planning block: Mondays 9-10am",
			TokenCount: 12,
			EntryCount: 1,
		},
		TokenBudget: 800,
		AssembledAt: time.Date(2026, 4, 15, 10, 0, 0, 0, time.UTC),
	}
}

func wrapProfileContextBaseMsg(t *testing.T, p *operatingmodel.ProfileContext) []byte {
	t.Helper()
	baseMsg := message.NewBaseMessage(p.Schema(), p, "test")
	data, err := baseMsg.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal BaseMessage: %v", err)
	}
	return data
}

func TestHandleProfileContextMessage_InjectsIntoHydratedRegion(t *testing.T) {
	c := newProfileContextTestComponent(t, true)
	seedLoop(t, c, "loop-abc")

	payload := validProfileContextPayload()
	data := wrapProfileContextBaseMsg(t, payload)
	c.handleProfileContextMessage(context.Background(), data)

	cm := c.handler.GetContextManager("loop-abc")
	if cm == nil {
		t.Fatal("context manager missing for seeded loop")
	}
	if got := cm.GetRegionTokens(RegionHydratedContext); got == 0 {
		t.Errorf("RegionHydratedContext tokens = 0, want non-zero after injection")
	}

	// Assert that the payload's rendered preamble is present verbatim rather
	// than against a specific header string — the header text is owned by the
	// operating-model package and its tests.
	preamble := payload.SystemPromptPreamble()
	if preamble == "" {
		t.Fatal("test fixture produced an empty preamble; fix fixture")
	}
	joined := ""
	for _, m := range cm.GetContext() {
		joined += m.Content + "\n"
	}
	if !strings.Contains(joined, preamble) {
		t.Errorf("injected context missing rendered preamble; got:\n%s", joined)
	}
}

func TestHandleProfileContextMessage_SkipsWhenDisabled(t *testing.T) {
	c := newProfileContextTestComponent(t, false)
	seedLoop(t, c, "loop-abc")

	data := wrapProfileContextBaseMsg(t, validProfileContextPayload())
	c.handleProfileContextMessage(context.Background(), data)

	cm := c.handler.GetContextManager("loop-abc")
	if got := cm.GetRegionTokens(RegionHydratedContext); got != 0 {
		t.Errorf("RegionHydratedContext tokens = %d; should be 0 with inject disabled", got)
	}
}

func TestHandleProfileContextMessage_EmptyProfileSkipsInjection(t *testing.T) {
	c := newProfileContextTestComponent(t, true)
	seedLoop(t, c, "loop-abc")

	payload := validProfileContextPayload()
	payload.OperatingModel = operatingmodel.ProfileContextSlice{} // empty
	data := wrapProfileContextBaseMsg(t, payload)

	c.handleProfileContextMessage(context.Background(), data)

	cm := c.handler.GetContextManager("loop-abc")
	if got := cm.GetRegionTokens(RegionHydratedContext); got != 0 {
		t.Errorf("empty profile should not inject anything; got %d tokens", got)
	}
}

func TestHandleProfileContextMessage_UnknownLoopIsNoop(t *testing.T) {
	// No seeded loop — GetContextManager returns nil. Handler must not panic.
	c := newProfileContextTestComponent(t, true)
	data := wrapProfileContextBaseMsg(t, validProfileContextPayload())

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("handler panicked on unknown loop: %v", r)
		}
	}()
	c.handleProfileContextMessage(context.Background(), data)
}

func TestHandleProfileContextMessage_InvalidJSON(t *testing.T) {
	c := newProfileContextTestComponent(t, true)
	seedLoop(t, c, "loop-abc")

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("handler panicked on invalid JSON: %v", r)
		}
	}()
	c.handleProfileContextMessage(context.Background(), []byte("{not json"))

	cm := c.handler.GetContextManager("loop-abc")
	if got := cm.GetRegionTokens(RegionHydratedContext); got != 0 {
		t.Errorf("invalid JSON should not mutate hydrated region; got %d tokens", got)
	}
}

func TestHandleProfileContextMessage_WrongPayloadType(t *testing.T) {
	c := newProfileContextTestComponent(t, true)
	seedLoop(t, c, "loop-abc")

	// Wrap a LayerApproved payload to simulate a mis-routed publish.
	la := &operatingmodel.LayerApproved{
		UserID: "u", LoopID: "loop-abc", Layer: operatingmodel.LayerFriction,
		ProfileVersion: 1, CheckpointSummary: "s",
		Entries:    []operatingmodel.Entry{{EntryID: "e1", Title: "t", Summary: "s"}},
		ApprovedAt: time.Now(),
	}
	baseMsg := message.NewBaseMessage(la.Schema(), la, "test")
	data, err := baseMsg.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal LayerApproved BaseMessage: %v", err)
	}

	c.handleProfileContextMessage(context.Background(), data)

	cm := c.handler.GetContextManager("loop-abc")
	if got := cm.GetRegionTokens(RegionHydratedContext); got != 0 {
		t.Errorf("wrong payload should not inject; got %d tokens", got)
	}
}

func TestInjectProfileContext_DefaultsOff(t *testing.T) {
	// Backward-compat check: deployments that don't set the flag get no
	// subscription and no behavior change.
	cfg := DefaultConfig()
	if cfg.Context.InjectProfileContext {
		t.Errorf("InjectProfileContext default = true, want false")
	}
}
