package gateddagexec

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/message"
)

// TestDecodeDispatch_RoundTrip proves the shared helper (ADR-070 B5) decodes a
// registry-wrapped dispatch envelope — the exact shape the durable stream
// delivers — back to the typed payload, so consumers don't reinvent the unwrap.
func TestDecodeDispatch_RoundTrip(t *testing.T) {
	orig := &DispatchMessage{UnitEntityID: "acme.ops.plan.fanout.unit.a", FanOutWorkflow: "gateddag-fanout"}
	data, err := json.Marshal(message.NewBaseMessage(orig.Schema(), orig, "gateddag-executor"))
	if err != nil {
		t.Fatalf("marshal base message: %v", err)
	}

	got, err := DecodeDispatch(data)
	if err != nil {
		t.Fatalf("DecodeDispatch: %v", err)
	}
	if got.UnitEntityID != orig.UnitEntityID {
		t.Errorf("UnitEntityID = %q, want %q", got.UnitEntityID, orig.UnitEntityID)
	}
	if got.FanOutWorkflow != orig.FanOutWorkflow {
		t.Errorf("FanOutWorkflow = %q, want %q", got.FanOutWorkflow, orig.FanOutWorkflow)
	}
}

// TestDecodeDispatch_Garbage returns an error, not a panic, on non-envelope bytes.
func TestDecodeDispatch_Garbage(t *testing.T) {
	if _, err := DecodeDispatch([]byte("not a base message")); err == nil {
		t.Fatal("want error on garbage input, got nil")
	}
}
