package agenticmemory

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	operatingmodel "github.com/c360studio/semstreams/agentic/operating-model"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
)

// newHandlerTestComponent builds a teams-memory Component suitable for driving
// handleLayerApproved directly. NATSClient is nil so publishGraphMutations is a
// no-op but still returns nil — that mirrors the production path's contract
// that missing NATS is tolerated (tests/units configs).
func newHandlerTestComponent(t *testing.T, org, platform string) *Component {
	t.Helper()
	cfg := DefaultConfig()
	rawCfg, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal default config: %v", err)
	}
	deps := component.Dependencies{
		NATSClient: nil,
		Platform:   component.PlatformMeta{Org: org, Platform: platform},
	}
	comp, err := NewComponent(rawCfg, deps)
	if err != nil {
		t.Fatalf("NewComponent failed: %v", err)
	}
	c, ok := comp.(*Component)
	if !ok {
		t.Fatalf("NewComponent returned %T, want *Component", comp)
	}
	return c
}

func wrapAsBaseMessage(t *testing.T, p message.Payload) []byte {
	t.Helper()
	msg := message.NewBaseMessage(p.Schema(), p, "test")
	data, err := msg.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal BaseMessage: %v", err)
	}
	return data
}

func validLayerApproved() *operatingmodel.LayerApproved {
	return &operatingmodel.LayerApproved{
		UserID:            "coby",
		LoopID:            "loop-abc",
		Layer:             operatingmodel.LayerOperatingRhythms,
		ProfileVersion:    1,
		CheckpointSummary: "Weekly rhythms established",
		Entries: []operatingmodel.Entry{
			{EntryID: "e-1", Title: "Weekly planning", Summary: "Mondays 9-10am"},
		},
		ApprovedAt: time.Date(2026, 4, 15, 10, 0, 0, 0, time.UTC),
	}
}

func TestHandleLayerApproved_Success(t *testing.T) {
	c := newHandlerTestComponent(t, "c360", "ops")
	data := wrapAsBaseMessage(t, validLayerApproved())

	before := atomic.LoadInt64(&c.eventsProcessed)
	beforeErrs := atomic.LoadInt64(&c.errors)

	beforeActivity := time.Now().Add(-time.Millisecond)
	c.handleLayerApproved(context.Background(), data)

	if got := atomic.LoadInt64(&c.eventsProcessed); got != before+1 {
		t.Errorf("eventsProcessed = %d, want %d", got, before+1)
	}
	if got := atomic.LoadInt64(&c.errors); got != beforeErrs {
		t.Errorf("errors = %d, want unchanged %d", got, beforeErrs)
	}
	c.mu.RLock()
	gotActivity := c.lastActivity
	c.mu.RUnlock()
	if !gotActivity.After(beforeActivity) {
		t.Errorf("lastActivity = %v, want after %v", gotActivity, beforeActivity)
	}
}

func TestHandleLayerApproved_InvalidJSON(t *testing.T) {
	c := newHandlerTestComponent(t, "c360", "ops")
	before := atomic.LoadInt64(&c.errors)

	c.handleLayerApproved(context.Background(), []byte("{not json"))

	if got := atomic.LoadInt64(&c.errors); got != before+1 {
		t.Errorf("errors = %d, want %d", got, before+1)
	}
	if got := atomic.LoadInt64(&c.eventsProcessed); got != 0 {
		t.Errorf("eventsProcessed = %d, want 0 on invalid JSON", got)
	}
}

func TestHandleLayerApproved_WrongPayloadType(t *testing.T) {
	c := newHandlerTestComponent(t, "c360", "ops")
	// Wrap a different payload type — ProfileContext — in a BaseMessage.
	pc := &operatingmodel.ProfileContext{UserID: "u", LoopID: "l"}
	data := wrapAsBaseMessage(t, pc)

	before := atomic.LoadInt64(&c.errors)
	c.handleLayerApproved(context.Background(), data)
	if got := atomic.LoadInt64(&c.errors); got != before+1 {
		t.Errorf("errors = %d, want %d (wrong type should count as error)", got, before+1)
	}
}

func TestHandleLayerApproved_InvalidPayload(t *testing.T) {
	c := newHandlerTestComponent(t, "c360", "ops")
	p := validLayerApproved()
	p.UserID = "" // violates Validate
	// Marshal the LayerApproved directly (skip Validate that MarshalJSON would enforce)
	// BaseMessage.MarshalJSON calls Validate; we want to ship invalid bytes. Build the
	// wire envelope by hand.
	data := buildBaseMessageBytes(t, p.Schema(), rawPayloadJSON(t, p))

	before := atomic.LoadInt64(&c.errors)
	c.handleLayerApproved(context.Background(), data)
	if got := atomic.LoadInt64(&c.errors); got != before+1 {
		t.Errorf("errors = %d, want %d", got, before+1)
	}
}

func TestHandleLayerApproved_MissingPlatform(t *testing.T) {
	c := newHandlerTestComponent(t, "", "") // deliberate empty platform
	data := wrapAsBaseMessage(t, validLayerApproved())

	before := atomic.LoadInt64(&c.errors)
	c.handleLayerApproved(context.Background(), data)
	if got := atomic.LoadInt64(&c.errors); got != before+1 {
		t.Errorf("errors = %d, want %d (empty platform should warn+count)", got, before+1)
	}
	if got := atomic.LoadInt64(&c.eventsProcessed); got != 0 {
		t.Errorf("eventsProcessed = %d, want 0 with missing platform", got)
	}
}

func TestHandleLayerApproved_ContextCancelled(t *testing.T) {
	c := newHandlerTestComponent(t, "c360", "ops")
	data := wrapAsBaseMessage(t, validLayerApproved())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c.handleLayerApproved(ctx, data)

	if got := atomic.LoadInt64(&c.eventsProcessed); got != 0 {
		t.Errorf("eventsProcessed = %d, want 0 when context cancelled", got)
	}
	// Cancellation is not a failure; errors must not tick.
	if got := atomic.LoadInt64(&c.errors); got != 0 {
		t.Errorf("errors = %d, want 0 on ctx cancellation", got)
	}
}

func TestHandlerForPort_RoutesAllExpectedPorts(t *testing.T) {
	// Locks the contract that DefaultConfig port names match the routing table.
	// Renaming one without the other is the exact regression this catches.
	c := newHandlerTestComponent(t, "c360", "ops")
	for _, name := range []string{"compaction_events", "hydrate_requests", "layer_approved_events"} {
		h, ok := c.handlerForPort(name)
		if !ok {
			t.Errorf("handlerForPort(%q) not routed", name)
			continue
		}
		if h == nil {
			t.Errorf("handlerForPort(%q) returned nil handler", name)
		}
	}
	if _, ok := c.handlerForPort("entity_states"); ok {
		// entity_states is declared as a port but has no handler — must not route.
		t.Errorf("handlerForPort(entity_states) routed; expected skip")
	}
	if _, ok := c.handlerForPort("nonexistent_port"); ok {
		t.Errorf("handlerForPort(nonexistent_port) routed; expected skip")
	}
}

func TestHandleLayerApproved_UnregisteredType(t *testing.T) {
	// A BaseMessage wire envelope referencing a message.Type that no package has
	// registered: BaseMessage.UnmarshalJSON fails at the registry lookup step.
	// Distinct code path from bad JSON.
	c := newHandlerTestComponent(t, "c360", "ops")
	payloadJSON, err := json.Marshal(map[string]string{"anything": "ok"})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	data := buildBaseMessageBytes(t, message.Type{
		Domain: "not_a_real_domain", Category: "nope", Version: "v1",
	}, payloadJSON)

	before := atomic.LoadInt64(&c.errors)
	c.handleLayerApproved(context.Background(), data)
	if got := atomic.LoadInt64(&c.errors); got != before+1 {
		t.Errorf("errors = %d, want %d (unregistered type should count as error)",
			got, before+1)
	}
}

// --- helpers ---

// buildBaseMessageBytes constructs a BaseMessage wire envelope bypassing
// BaseMessage.Validate so invalid payloads can be fed into the handler for
// error-path coverage.
func buildBaseMessageBytes(t *testing.T, msgType message.Type, payloadJSON json.RawMessage) []byte {
	t.Helper()
	wire := struct {
		ID      string          `json:"id"`
		Type    message.Type    `json:"type"`
		Payload json.RawMessage `json:"payload"`
		Meta    map[string]any  `json:"meta"`
	}{
		ID:      "test-id",
		Type:    msgType,
		Payload: payloadJSON,
		Meta: map[string]any{
			"created_at":  time.Now().UnixMilli(),
			"received_at": time.Now().UnixMilli(),
			"source":      "test",
		},
	}
	data, err := json.Marshal(wire)
	if err != nil {
		t.Fatalf("marshal wire: %v", err)
	}
	return data
}

// rawPayloadJSON json-encodes a payload via the Alias path (payload MarshalJSON)
// without running Validate.
func rawPayloadJSON(t *testing.T, p message.Payload) json.RawMessage {
	t.Helper()
	data, err := p.MarshalJSON()
	if err != nil {
		t.Fatalf("payload MarshalJSON: %v", err)
	}
	return data
}
