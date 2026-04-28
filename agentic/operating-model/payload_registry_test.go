package operatingmodel

import (
	"testing"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadregistry"
)

func TestRegistry_LayerApprovedTypeExists(t *testing.T) {
	p := payloadregistry.Create(Domain, CategoryLayerApproved, SchemaVersion)
	if p == nil {
		t.Fatalf("CreatePayload returned nil; LayerApproved not registered")
	}
	if _, ok := p.(*LayerApproved); !ok {
		t.Fatalf("CreatePayload returned %T, want *LayerApproved", p)
	}
}

func TestRegistry_ProfileContextTypeExists(t *testing.T) {
	p := payloadregistry.Create(Domain, CategoryProfileContext, SchemaVersion)
	if p == nil {
		t.Fatalf("CreatePayload returned nil; ProfileContext not registered")
	}
	if _, ok := p.(*ProfileContext); !ok {
		t.Fatalf("CreatePayload returned %T, want *ProfileContext", p)
	}
}

func TestRegistry_BaseMessageRoundTripLayerApproved(t *testing.T) {
	original := validLayerApproved()
	msg := message.NewBaseMessage(
		message.Type{Domain: Domain, Category: CategoryLayerApproved, Version: SchemaVersion},
		original,
		"test-source",
	)

	data, err := msg.MarshalJSON()
	if err != nil {
		t.Fatalf("BaseMessage.MarshalJSON failed: %v", err)
	}

	reg := payloadregistry.NewWithSubset(t, RegisterPayloads)
	roundTripped, err := message.NewDecoder(reg).Decode(data)
	if err != nil {
		t.Fatalf("Decoder.Decode failed: %v", err)
	}

	payload := roundTripped.Payload()
	got, ok := payload.(*LayerApproved)
	if !ok {
		t.Fatalf("round-tripped payload = %T, want *LayerApproved", payload)
	}
	if got.UserID != original.UserID || got.Layer != original.Layer ||
		len(got.Entries) != len(original.Entries) {
		t.Errorf("round-trip payload mismatch: got=%+v", got)
	}
}

func TestRegistry_DuplicateRegistrationErrors(t *testing.T) {
	// Documents the contract: attempting to re-register an already-registered
	// message type returns a validation error rather than succeeding silently.
	// init() panics on this error by design (see registerOrPanic).
	err := payloadregistry.Register(&payloadregistry.Registration{
		Domain:      Domain,
		Category:    CategoryLayerApproved,
		Version:     SchemaVersion,
		Description: "duplicate",
		Factory:     func() any { return &LayerApproved{} },
	})
	if err == nil {
		t.Fatalf("second RegisterPayload returned nil; expected duplicate error")
	}
}

func TestRegistry_BaseMessageRoundTripProfileContext(t *testing.T) {
	original := validProfileContext()
	msg := message.NewBaseMessage(
		message.Type{Domain: Domain, Category: CategoryProfileContext, Version: SchemaVersion},
		original,
		"test-source",
	)

	data, err := msg.MarshalJSON()
	if err != nil {
		t.Fatalf("BaseMessage.MarshalJSON failed: %v", err)
	}

	reg := payloadregistry.NewWithSubset(t, RegisterPayloads)
	roundTripped, err := message.NewDecoder(reg).Decode(data)
	if err != nil {
		t.Fatalf("Decoder.Decode failed: %v", err)
	}

	payload := roundTripped.Payload()
	got, ok := payload.(*ProfileContext)
	if !ok {
		t.Fatalf("round-tripped payload = %T, want *ProfileContext", payload)
	}
	if got.OperatingModel.Content != original.OperatingModel.Content {
		t.Errorf("round-trip content mismatch")
	}
}
