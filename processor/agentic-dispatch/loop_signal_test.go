package agenticdispatch

import (
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// spec: agentic-dispatch / One control-signal payload travels the loop signal subject
// The dispatch-local control-signal category is gone from the composed
// registry, and the user control signal is the only payload registered for the
// loop signal subject.
//
// It builds the registry the way a binary does — payloadbuiltins.Register, the
// one composition root that ran the retired dispatch registration — rather than
// asserting over a hand-listed set, so a composition root that still called a
// dispatch registration would fail here. That call cannot come back silently
// either: agenticdispatch.RegisterPayloads no longer exists, so restoring it is
// a compile error at the caller, not a quiet re-registration.
func TestRetiredSignalMessageCategoryIsUnregistered(t *testing.T) {
	reg := payloadbuiltins.NewTestRegistry(t)

	assert.Nil(t, reg.Create(agentic.Domain, "signal_message", agentic.SchemaVersion),
		"the retired dispatch-local control-signal category must not resolve to a payload")

	require.NotNil(t, reg.Create(agentic.Domain, agentic.CategorySignal, agentic.SchemaVersion),
		"the user control signal stays registered — it is the one type on this subject")
	_, isUserSignal := reg.Create(agentic.Domain, agentic.CategorySignal, agentic.SchemaVersion).(*agentic.UserSignal)
	assert.True(t, isUserSignal, "the signal category resolves to agentic.UserSignal")

	for _, registration := range reg.ListByDomain(agentic.Domain) {
		assert.NotEqual(t, "signal_message", registration.Category,
			"no registration in the agentic domain still carries the retired category")
	}
}
