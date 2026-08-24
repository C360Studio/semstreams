package rule

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadregistry"
	"github.com/c360studio/semstreams/processor/rule/expression"
)

// identityProbePayload stands in for an ADOPTER typed payload whose author
// exposes `entity_id` in its projection for matching. Deliberately not a
// framework type: no registered framework payload exposes a bare `entity_id`
// today, and the guarantee under test is the CONTRACT — any RuleReadable
// author may legally expose one tomorrow, and doing so must not let the
// projection capture durable state identity.
type identityProbePayload struct {
	EntityID string `json:"entity_id"`
	Marker   string `json:"marker"`
}

func (p *identityProbePayload) Schema() message.Type {
	return message.Type{Domain: "test", Category: "identity_probe", Version: "v1"}
}

func (p *identityProbePayload) Validate() error { return nil }

func (p *identityProbePayload) MarshalJSON() ([]byte, error) {
	type alias identityProbePayload
	return json.Marshal((*alias)(p))
}

func (p *identityProbePayload) UnmarshalJSON(data []byte) error {
	type alias identityProbePayload
	return json.Unmarshal(data, (*alias)(p))
}

// RuleFields exposes entity_id for CONDITION matching and substitution —
// the projection contract — which is exactly what must NOT leak into the
// RULE_STATE key.
func (p *identityProbePayload) RuleFields() map[string]any {
	return map[string]any{"entity_id": p.EntityID, "marker": p.Marker}
}

// identityProbeDecoder returns a production Decoder that can decode the
// probe payload from wire bytes.
func identityProbeDecoder(t *testing.T) *message.Decoder {
	t.Helper()
	reg := payloadregistry.New()
	if err := reg.Register(&payloadregistry.Registration{
		Domain:      "test",
		Category:    "identity_probe",
		Version:     "v1",
		Description: "state identity probe",
		Factory:     func() any { return &identityProbePayload{} },
	}); err != nil {
		t.Fatalf("register identity probe payload: %v", err)
	}
	if err := message.RegisterPayloads(reg); err != nil {
		t.Fatalf("register core payloads: %v", err)
	}
	return message.NewDecoder(reg)
}

// newIdentityProcessor assembles the message-path evaluation seam the same
// way payload_projection_test.go does: production decoder, production
// StatefulEvaluator over a StateTracker, mock KV and publisher.
func newIdentityProcessor(t *testing.T, def Definition) (*Processor, *StateTracker, *mockPublisher) {
	t.Helper()
	r, err := NewExpressionRule("test-pack", def)
	if err != nil {
		t.Fatalf("NewExpressionRule: %v", err)
	}
	publisher := &mockPublisher{}
	tracker := NewStateTracker(newMockKVBucket(), nil)
	processor := &Processor{
		logger:            slog.Default(),
		decoder:           identityProbeDecoder(t),
		rules:             map[string]Rule{def.ID: r},
		ruleDefinitions:   map[string]Definition{def.ID: def},
		matchCounters:     map[string]*atomic.Int64{def.ID: {}},
		stateTracker:      tracker,
		statefulEvaluator: NewStatefulEvaluator(tracker, NewActionExecutorFull(slog.Default(), nil, publisher), nil),
	}
	return processor, tracker, publisher
}

func identityRuleDefinition(id, entityID string) Definition {
	return Definition{
		ID:      id,
		Type:    "expression",
		Name:    id,
		Enabled: true,
		Logic:   "and",
		Conditions: []expression.ConditionExpression{
			{Field: "$message.entity_id", Operator: "eq", Value: entityID},
		},
		OnEnter: []Action{{Type: ActionTypePublish, Subject: "test.identity.fired"}},
	}
}

// TestTypedPayloadProjectionEntityIDDoesNotControlStateIdentity is the
// acceptance test for the PR #1052 round-5 blocking finding: the projection
// contract (condition visibility + $message.* substitution) must not also
// control durable state identity.
//
// Two DISTINCT wire messages of a typed payload both expose the SAME
// `entity_id` in RuleFields, and the rule's condition matches on that very
// field — proving the projection still serves matching. State identity must
// come from each message's wire ID (the pre-projection baseline for
// non-generic payloads, 774c85dc), so BOTH messages transition false→true on
// their own RULE_STATE record and OnEnter fires twice.
//
// Under the defective coupling (extractEntityID reading ruleFields), both
// messages collapse onto one record keyed by the projected value: the second
// evaluation is TransitionNone, OnEnter is suppressed, and only one publish
// occurs.
func TestTypedPayloadProjectionEntityIDDoesNotControlStateIdentity(t *testing.T) {
	const projected = "acme.prod.robotics.gcs.drone.d007"
	processor, tracker, publisher := newIdentityProcessor(t, identityRuleDefinition("typed-identity-probe", projected))

	first := message.NewBaseMessage(
		(&identityProbePayload{}).Schema(), &identityProbePayload{EntityID: projected, Marker: "first"}, "test")
	second := message.NewBaseMessage(
		(&identityProbePayload{}).Schema(), &identityProbePayload{EntityID: projected, Marker: "second"}, "test")
	if first.ID() == second.ID() {
		t.Fatal("test invariant: distinct messages must carry distinct wire IDs")
	}

	for _, msg := range []message.Message{first, second} {
		wire, err := json.Marshal(msg)
		if err != nil {
			t.Fatalf("marshal wire: %v", err)
		}
		processor.handleSemanticMessage(context.Background(), "test.identity.in", wire)
	}

	if got := len(publisher.published); got != 2 {
		t.Fatalf("OnEnter fired %d times, want 2 — distinct messages sharing a projected entity_id collapsed onto one durable state record", got)
	}

	ctx := context.Background()
	for _, msg := range []message.Message{first, second} {
		state, err := tracker.Get(ctx, "typed-identity-probe", msg.ID())
		if err != nil {
			t.Fatalf("RULE_STATE record for message %s: %v — typed-payload state identity must be the wire message ID", msg.ID(), err)
		}
		if !state.IsMatching {
			t.Errorf("state for message %s IsMatching = false, want true", msg.ID())
		}
	}
	if _, err := tracker.Get(ctx, "typed-identity-probe", projected); !errors.Is(err, ErrStateNotFound) {
		t.Errorf("a RULE_STATE record exists under the PROJECTED entity_id %q (err=%v) — the projection captured durable state identity", projected, err)
	}
}

// TestGenericPayloadEntityIDStateIdentityUnchanged pins the legacy lane the
// fix must NOT disturb: core.json.v1 payloads have carried their state
// identity in Data["entity_id"] since before RuleReadable existed, so two
// distinct generic messages with the same entity_id SHARE one RULE_STATE
// record by design — the first enters, the second is steady-state.
func TestGenericPayloadEntityIDStateIdentityUnchanged(t *testing.T) {
	const entityID = "acme.prod.robotics.gcs.drone.d007"
	processor, tracker, publisher := newIdentityProcessor(t, identityRuleDefinition("generic-identity", entityID))

	for _, marker := range []string{"first", "second"} {
		payload := message.NewGenericJSON(map[string]any{"entity_id": entityID, "marker": marker})
		wire, err := json.Marshal(message.NewBaseMessage(payload.Schema(), payload, "test"))
		if err != nil {
			t.Fatalf("marshal wire: %v", err)
		}
		processor.handleSemanticMessage(context.Background(), "test.identity.in", wire)
	}

	if got := len(publisher.published); got != 1 {
		t.Fatalf("OnEnter fired %d times, want exactly 1 — generic payloads sharing entity_id must keep collapsing onto one state record", got)
	}
	state, err := tracker.Get(context.Background(), "generic-identity", entityID)
	if err != nil {
		t.Fatalf("RULE_STATE record keyed by generic Data[entity_id]: %v", err)
	}
	if !state.IsMatching {
		t.Error("generic entity_id state record IsMatching = false, want true")
	}
}

// TestExtractEntityIDSeam pins the seam function directly: the generic
// payload's legacy triple (string / absent / non-string) and the typed-payload
// exclusion.
func TestExtractEntityIDSeam(t *testing.T) {
	cases := []struct {
		name    string
		payload message.Payload
		// wantProjected is what extractEntityID must NOT return for typed
		// payloads; want=="" means "expect the wire message ID".
		want string
	}{
		{
			name:    "generic string entity_id wins",
			payload: message.NewGenericJSON(map[string]any{"entity_id": "acme.prod.robotics.gcs.drone.d007"}),
			want:    "acme.prod.robotics.gcs.drone.d007",
		},
		{
			name:    "generic without entity_id falls back to message ID",
			payload: message.NewGenericJSON(map[string]any{"role": "architect"}),
		},
		{
			name:    "generic non-string entity_id falls back to message ID",
			payload: message.NewGenericJSON(map[string]any{"entity_id": 42}),
		},
		{
			name:    "typed projection entity_id is excluded from identity",
			payload: &identityProbePayload{EntityID: "acme.prod.robotics.gcs.drone.d007"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := message.NewBaseMessage(tc.payload.Schema(), tc.payload, "test")
			want := tc.want
			if want == "" {
				want = msg.ID()
			}
			if got := extractEntityID(msg); got != want {
				t.Errorf("extractEntityID = %q, want %q", got, want)
			}
		})
	}
}
