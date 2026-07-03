package graph

import (
	"testing"

	"github.com/c360studio/semstreams/message"
	"github.com/stretchr/testify/assert"
)

func TestEntityState_IsStub(t *testing.T) {
	tests := []struct {
		name string
		mt   message.Type
		want bool
	}{
		{"stub envelope is a stub", StubMessageType, true},
		{"real envelope is not a stub", message.Type{Domain: "workflow", Category: "task-unit", Version: "v1"}, false},
		{"zero envelope is not a stub", message.Type{}, false},
		{"same-domain different-category is not a stub", message.Type{Domain: "core", Category: "identity", Version: "v1"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			es := EntityState{ID: "acme.ops.robotics.gcs.drone.001", MessageType: tc.mt}
			assert.Equal(t, tc.want, es.IsStub())
		})
	}
}

// IsStub keys on the ENVELOPE, not the PredStubMarker triple: the stub triple
// persists after real birth (nothing removes it), so a triple-based check would
// wrongly classify an upgraded real unit as a stub forever. This locks the
// envelope-not-triple discriminator (gh#429).
func TestEntityState_IsStub_KeysOnEnvelopeNotTriple(t *testing.T) {
	es := EntityState{
		ID:          "acme.ops.robotics.gcs.drone.001",
		MessageType: message.Type{Domain: "workflow", Category: "task-unit", Version: "v1"},
		Triples:     []message.Triple{{Subject: "acme.ops.robotics.gcs.drone.001", Predicate: PredStubMarker, Object: true}},
	}
	assert.False(t, es.IsStub(), "a real-enveloped entity still carrying a persisted stub marker triple is NOT a stub")
}
