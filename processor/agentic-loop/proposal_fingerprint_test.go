package agenticloop

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/stretchr/testify/require"
)

// ProposalFingerprint is a CARRIED correlation token, not a verified one, and
// this pins both halves of that so neither can change silently.
//
// Carried: agentic-loop mints it onto every proposed call, the rule engine
// echoes it back on the verdict (processor/rule/actions.go:2218) and
// agentic-loop decodes it into VerdictPayload (component.go:2391). Two
// consumers already read the field off the wire, so it is not dead surface.
//
// Not verified: nothing compares the verdict's fingerprint against the
// proposal's, so a verdict that disagrees is still delivered to its waiter.
// Enforcing the comparison needs the proposal's fingerprint to outlive the
// process that registered the waiter, which is durable-loop work this change
// does not own. The last subtest is the observer that will fail the day
// someone implements it, forcing the spec to move with the code.
//
// spec: agentic-governance / Governance publications are durably at-least-once
func TestProposalFingerprintIsCarriedAndNotVerified(t *testing.T) {
	t.Parallel()

	t.Run("the proposal carries a deterministic fingerprint", func(t *testing.T) {
		pub := &mockVerdictPublisher{}
		dispatcher := NewGovernanceDispatcher(
			ToolCallGovernanceConfig{Mode: ToolCallGovernanceModeAudit}, pub, slog.Default(), nil)

		call := governanceTestCall("fp-1", "bash")
		call.Arguments = map[string]any{"command": "ls /tmp"}
		_, err := dispatcher.Propose(context.Background(), "loop-fp", "", []agentic.ToolCall{call})
		require.NoError(t, err)

		published := pub.Published()
		require.Len(t, published, 1)
		payload := unwrapProposedFromBaseMessage(t, published[0].data)
		require.True(t, strings.HasPrefix(payload.ProposalFingerprint, "sha256:"),
			"proposal_fingerprint = %q, want a sha256: digest", payload.ProposalFingerprint)

		// Same logical proposal, same digest; a different argument, a
		// different digest. Without both halves the field could be a
		// constant and every assertion above would still pass.
		again, err := fingerprintProposedToolCall(payload)
		require.NoError(t, err)
		require.Equal(t, payload.ProposalFingerprint, again,
			"the digest must exclude the field it is stored in, or it can never be recomputed")

		divergent := payload
		divergent.Arguments = map[string]any{"command": "rm -rf /"}
		other, err := fingerprintProposedToolCall(divergent)
		require.NoError(t, err)
		require.NotEqual(t, payload.ProposalFingerprint, other,
			"a different proposed call must not share a fingerprint")
	})

	t.Run("the verdict round-trips the echoed fingerprint", func(t *testing.T) {
		wire, err := json.Marshal(map[string]any{
			"decision":             "approved",
			"execution_id":         "execution-fp-1",
			"proposal_fingerprint": "sha256:echoed-digest",
		})
		require.NoError(t, err)

		var payload VerdictPayload
		require.NoError(t, json.Unmarshal(wire, &payload))
		require.Equal(t, "sha256:echoed-digest", payload.ProposalFingerprint,
			"the token the rule echoed must survive the decode agentic-loop performs")
	})

	t.Run("a disagreeing fingerprint does not refuse the verdict today", func(t *testing.T) {
		dispatcher := NewGovernanceDispatcher(
			ToolCallGovernanceConfig{Mode: ToolCallGovernanceModeEnforce, Timeout: "1s"},
			nil, slog.Default(), nil,
		).(*enforceDispatcher)

		waiter := dispatcher.registerWaiter("execution-fp-1")
		defer dispatcher.releaseWaiter("execution-fp-1")

		wire, err := json.Marshal(map[string]any{
			"decision":             "approved",
			"execution_id":         "execution-fp-1",
			"proposal_fingerprint": "sha256:not-the-proposals-digest",
		})
		require.NoError(t, err)

		decision, err := dispatcher.HandleVerdict("approved", "execution-fp-1", wire)
		require.NoError(t, err)
		require.Equal(t, natsclient.DeliveryDecisionAck, decision,
			"routing is by execution identity alone; the fingerprint is audit context")
		require.Equal(t, "approved", (<-waiter).decision)
	})
}
