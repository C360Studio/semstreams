package component

import (
	"testing"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/natsclient"
)

// TestCapabilitiesStream_SatisfiesTheBoundsRequirement is the framework holding
// itself to its own contract.
//
// COMPONENT_CAPABILITIES was the live in-repo violation: created through
// EnsureStream with a one-hour MaxAge, no MaxBytes and no discard policy, so a
// running server reported `max_bytes=-1 discard=old` — neither of which anyone
// chose. A framework that exempts itself from the requirement it asks sister repos
// to honor cannot ask them, and a boot-path literal is exactly the kind of
// declaration that drifts back without a test on it.
func TestCapabilitiesStream_SatisfiesTheBoundsRequirement(t *testing.T) {
	cfg := capabilitiesStreamConfig()

	require.NoError(t, natsclient.CheckStreamBounds(cfg, "component.Registry.InitNATS"),
		"the framework's own stream must satisfy the requirement EnsureStream enforces")

	assert.Positive(t, cfg.MaxAge, "a finite age, not the server's choice")
	assert.Positive(t, cfg.MaxBytes, "a finite size: the previous value was the server's -1")

	// Discard cannot be REQUIRED at the EnsureStream seam, because DiscardOld is
	// the zero value of the field and a deliberate choice is byte-identical to an
	// absent one. It is asserted here, where the declaration lives and the intent
	// is knowable: an announcement is a fact about current capabilities, so the
	// NEWEST is the one worth keeping, and DiscardNew would refuse it at the
	// ceiling and pin discovery to stale capabilities.
	assert.Equal(t, jetstream.DiscardOld, cfg.Discard)

	// Still an ordinary stream: were this ever renamed under a reserved prefix,
	// bounds would become the wrong requirement entirely and CheckStreamBounds
	// would pass by exemption rather than by declaration.
	kind, _ := natsclient.ClassifyBackingStream(cfg.Name)
	require.Equal(t, natsclient.ResourceOrdinaryStream, kind,
		"the assertions above only mean something for an ordinary stream")
}
