package graph

import (
	"testing"
	"time"

	"github.com/c360studio/semstreams/message"
)

// trustedDecodeValidState builds a canonical EntityState and returns it with
// its MarshalEntityState bytes (the exact bytes the ENTITY_STATES owner
// persists).
func trustedDecodeValidState(t *testing.T) (*EntityState, []byte) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	state := &EntityState{
		ID: "c360.test.graph.decode.entity.001",
		Triples: []message.Triple{
			{
				Subject:    "c360.test.graph.decode.entity.001",
				Predicate:  "test.decode.kind",
				Object:     "alpha",
				Timestamp:  now,
				Confidence: 1.0,
			},
			{
				Subject:    "c360.test.graph.decode.entity.001",
				Predicate:  "test.decode.status",
				Object:     "active",
				Timestamp:  now,
				Confidence: 1.0,
			},
		},
		MessageType: message.Type{Domain: "test", Category: "decode", Version: "v1"},
		Version:     3,
		UpdatedAt:   now,
	}
	data, err := MarshalEntityState(state)
	if err != nil {
		t.Fatalf("MarshalEntityState on canonical fixture: %v", err)
	}
	return state, data
}

func TestUnmarshalEntityStateTrusted_DecodesValidState(t *testing.T) {
	t.Parallel()

	want, data := trustedDecodeValidState(t)

	var got EntityState
	if err := UnmarshalEntityStateTrusted(data, &got); err != nil {
		t.Fatalf("UnmarshalEntityStateTrusted on canonical bytes: %v", err)
	}
	if got.ID != want.ID {
		t.Errorf("ID = %q, want %q", got.ID, want.ID)
	}
	if len(got.Triples) != len(want.Triples) {
		t.Fatalf("len(Triples) = %d, want %d", len(got.Triples), len(want.Triples))
	}
	if got.Triples[0].Predicate != want.Triples[0].Predicate {
		t.Errorf("Triples[0].Predicate = %q, want %q", got.Triples[0].Predicate, want.Triples[0].Predicate)
	}
	if got.Version != want.Version {
		t.Errorf("Version = %d, want %d", got.Version, want.Version)
	}
}

func TestUnmarshalEntityStateTrusted_MalformedJSONReturnsPlainError(t *testing.T) {
	t.Parallel()

	var got EntityState
	err := UnmarshalEntityStateTrusted([]byte(`{"id": "truncated`), &got)
	if err == nil {
		t.Fatal("UnmarshalEntityStateTrusted on malformed JSON returned nil error")
	}
	// The trusted decoder is a plain decode: it must NOT classify malformed
	// bytes as the graph-state-reset contract — that classification belongs to
	// the validating decoder and the poison-detection paths that keep using it.
	if IsStateContractError(err) {
		t.Errorf("trusted decoder classified malformed JSON as state-contract error: %v", err)
	}
}

// TestUnmarshalEntityStateTrusted_AdmitsNoncanonicalState pins the trusted
// decoder's contract (gh#562): it performs NO canonical-contract validation.
// Noncanonical predicates, subjects, and root IDs decode without error — the
// MarshalEntityState write gate, not the owner's own RMW read, is the
// enforcement boundary. The validating decoder must keep rejecting the same
// bytes.
func TestUnmarshalEntityStateTrusted_AdmitsNoncanonicalState(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		data []byte
	}{
		{
			name: "noncanonical predicate",
			data: []byte(`{"id":"c360.test.graph.decode.entity.002",` +
				`"triples":[{"subject":"c360.test.graph.decode.entity.002",` +
				`"predicate":"Test.Poison.Predicate","object":"x"}],` + // predicate-audit:invalid {"kind":"stored-predicate","value":"Test.Poison.Predicate","reason":"segment_start"}
				`"message_type":{"domain":"test","category":"decode","version":"v1"},` +
				`"version":1,"updated_at":"2026-01-01T00:00:00Z"}`),
		},
		{
			name: "noncanonical triple subject",
			data: []byte(`{"id":"c360.test.graph.decode.entity.003",` +
				`"triples":[{"subject":"bad.subject",` +
				`"predicate":"test.poison.kind","object":"x"}],` +
				`"message_type":{"domain":"test","category":"decode","version":"v1"},` +
				`"version":1,"updated_at":"2026-01-01T00:00:00Z"}`),
		},
		{
			name: "noncanonical root id",
			data: []byte(`{"id":"only.two","triples":[],` +
				`"message_type":{"domain":"test","category":"decode","version":"v1"},` +
				`"version":1,"updated_at":"2026-01-01T00:00:00Z"}`),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var trusted EntityState
			if err := UnmarshalEntityStateTrusted(tc.data, &trusted); err != nil {
				t.Fatalf("trusted decoder rejected noncanonical state (that IS its contract): %v", err)
			}

			// Contrast pin: the validating decoder still refuses the bytes.
			var validated EntityState
			if err := UnmarshalEntityState(tc.data, &validated); err == nil {
				t.Fatal("validating decoder accepted noncanonical state; fixture is not poison")
			}
		})
	}
}
