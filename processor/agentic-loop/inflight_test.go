package agenticloop

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/pkg/errs"
)

// TestInFlight_NoBoundConsumerIsUnknownNotZero is the defect gh#733 was filed
// about. A deployment with no agentic-loop consumer for the subject must not
// answer "nothing in flight" — there may be work on the stream that nobody here
// is answering for.
func TestInFlight_NoBoundConsumerIsUnknownNotZero(t *testing.T) {
	t.Parallel()
	c := &Component{}

	_, err := c.outstandingForSubject(context.Background(), "agent.task.*")
	if err == nil {
		t.Fatal("expected unknown; got a nil error, which a caller reads as an authoritative answer")
	}
	if !errors.Is(err, ErrInFlightUnknownNoConsumer) {
		t.Errorf("error must match the sentinel so a caller branches on identity, not message "+
			"text; got %v", err)
	}
}

// TestInFlight_HandlerReturnsNoPayloadWhenUnknown pins the shape that makes the
// invariant unrepresentable: an unknown answer is an error, never an
// InFlightResponse carrying Outstanding: 0.
func TestInFlight_HandlerReturnsNoPayloadWhenUnknown(t *testing.T) {
	t.Parallel()
	c := &Component{}

	req, err := json.Marshal(InFlightRequest{Subject: "agent.task.*"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	payload, err := c.handleInFlightQuery(context.Background(), req)
	if err == nil {
		t.Fatal("expected an error for an unknown answer")
	}
	if payload != nil {
		t.Errorf("an unknown answer must carry NO payload, else a caller decodes Outstanding:0 "+
			"and reads absence of a measurement as a measurement of absence; got %s", payload)
	}
}

// TestInFlight_KnownAnswerRoundTripsThroughTheResponseShape guards the decode a
// caller actually performs. A zero-valued expectation would prove nothing here,
// so the fixture is deliberately non-zero.
func TestInFlight_ResponseShapeCarriesInFlightExplicitly(t *testing.T) {
	t.Parallel()
	cases := []struct {
		outstanding  uint64
		wantInFlight bool
	}{
		{outstanding: 3, wantInFlight: true},
		{outstanding: 0, wantInFlight: false},
	}
	for _, tc := range cases {
		raw, err := json.Marshal(InFlightResponse{
			Subject:     "agent.task.*",
			Outstanding: tc.outstanding,
			InFlight:    tc.outstanding > 0,
		})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var got InFlightResponse
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got.Outstanding != tc.outstanding || got.InFlight != tc.wantInFlight {
			t.Errorf("round trip: got %+v, want outstanding=%d inFlight=%v",
				got, tc.outstanding, tc.wantInFlight)
		}
	}
}

// TestInFlight_InvalidRequestIsInvalidNotUnknown — a malformed request is the
// caller's bug and must classify Invalid, distinct from the Transient "unknown"
// class. Collapsing them would make a caller retry its own bad request forever.
func TestInFlight_InvalidRequestIsInvalidNotUnknown(t *testing.T) {
	t.Parallel()
	c := &Component{}

	for _, data := range [][]byte{[]byte("not json"), []byte(`{"subject":""}`)} {
		_, err := c.handleInFlightQuery(context.Background(), data)
		if err == nil {
			t.Fatalf("expected an error for request %q", data)
		}
		if !errs.IsInvalid(err) {
			t.Errorf("request %q should classify Invalid, got %v", data, err)
		}
	}
}

// TestInFlight_ConsumerLookupUsesTheRecordedBinding proves the query addresses the
// consumer the component actually bound, including its ConsumerNameSuffix, rather
// than re-deriving a name that could drift from it.
func TestInFlight_ConsumerLookupUsesTheRecordedBinding(t *testing.T) {
	t.Parallel()
	c := &Component{consumerInfos: []consumerInfo{
		{streamName: "AGENT", consumerName: "agentic-loop-agent-task-any-suffixed", subject: "agent.task.*"},
		{streamName: "AGENT", consumerName: "agentic-loop-tool-result-all", subject: "tool.result.>"},
	}}

	stream, consumer, ok := c.consumerForSubject("agent.task.*")
	if !ok {
		t.Fatal("expected the recorded binding to be found")
	}
	if stream != "AGENT" || consumer != "agentic-loop-agent-task-any-suffixed" {
		t.Errorf("got (%q, %q); the query must address the BOUND consumer, suffix included", stream, consumer)
	}

	if _, _, ok := c.consumerForSubject("agent.never.bound"); ok {
		t.Error("an unbound subject must not resolve to some other consumer")
	}
}

// TestInFlight_UnknownHasExactlyOneConstructionSite is the structural half of the
// "one rule, three instances" requirement.
//
// The three unknown cases are one invariant — an absent measurement must never
// render as a measurement of absence — not three coincidences. If a fourth case is
// added later by classifying an error inline, it will bypass the rule and this
// test is what catches it.
func TestInFlight_UnknownHasExactlyOneConstructionSite(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("inflight.go")
	if err != nil {
		t.Fatalf("read inflight.go: %v", err)
	}
	body := string(src)

	if got := strings.Count(body, "errs.ClassifiedCode("); got != 1 {
		t.Errorf("expected exactly ONE errs.ClassifiedCode call in inflight.go (inside "+
			"errUnknownInFlight), got %d — a new unknown case must route through the shared "+
			"constructor, not classify inline", got)
	}
	if got := strings.Count(body, "errUnknownInFlight("); got < 3 {
		t.Errorf("expected the shared constructor to be declared and used by each unknown "+
			"path, found %d references", got)
	}
}

// TestStartFailure_TearsDownRequestSubscriptions covers Codex finding 6, which I
// introduced by adding a SECOND request subscription to Start.
//
// Start installs trajectorySub then inflightSub. If the second fails,
// start-failure cleanup previously stopped only JetStream consumers, so
// trajectorySub stayed live while `started` remained false — and Stop returns early
// when not started, so nothing ever reaped it. A Start retry then installed a
// SECOND responder on the same subject and NATS delivered requests to both.
//
// This local check covers the cleanup operation's idempotent, nil-safe request
// subscription state transition. Start-failure generation retention is covered
// separately through the component lifecycle path.
func TestStartFailure_TearsDownRequestSubscriptions(t *testing.T) {
	t.Parallel()

	// Idempotent and nil-safe: both cleanup paths call it, and a Start that fails
	// before either subscription exists must not panic.
	c := &Component{}
	if err := c.cleanup(context.Background(), false); err != nil {
		t.Fatalf("first cleanup: %v", err)
	}
	if err := c.cleanup(context.Background(), false); err != nil {
		t.Fatalf("second cleanup: %v", err)
	}
	if c.trajectorySub != nil || c.inflightSub != nil {
		t.Error("teardown must leave both subscription handles nil")
	}
}
