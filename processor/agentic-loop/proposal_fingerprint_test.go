package agenticloop

import (
	"bytes"
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
// agentic-loop decodes it into VerdictPayload (component.go:2391), where
// audit mode logs it as context on the observed-verdict line
// (governance_dispatcher.go:342). Three consumers read the field, so it is
// not dead surface — and the decoded half has an observer, so "decodes it as
// audit context" is a claim the code backs rather than a sentence about it.
//
// Not verified: nothing compares the verdict's fingerprint against the
// proposal's, so a verdict that disagrees is still delivered to its waiter.
// Enforcing the comparison needs the proposal's fingerprint to outlive the
// process that registered the waiter — durable per-call governance state this
// change does not own. NO LAYER OF THIS STACK VERIFIES IT: L4 (#1330) carries
// durable loop state, not durable per-call proposal state, so absent a new
// issue the fingerprint is an audit token only. The last subtest is the
// observer that will fail the day someone implements a comparison, forcing
// the spec to move with the code.
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

	t.Run("audit mode reads the decoded fingerprint onto its verdict line", func(t *testing.T) {
		// The decode at component.go:2392 had no reader: the field was
		// assigned and dropped. "Audit context" is only true if something
		// audits it, so the observed-verdict line carries it and this
		// asserts the emitted record rather than the struct field.
		var logs bytes.Buffer
		dispatcher := NewGovernanceDispatcher(
			ToolCallGovernanceConfig{Mode: ToolCallGovernanceModeAudit},
			&mockVerdictPublisher{},
			slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo})),
			nil,
		)

		wire, err := json.Marshal(map[string]any{
			"decision":             "rejected",
			"execution_id":         "execution-fp-audit",
			"rule_id":              "rule-fp-audit",
			"reason":               "denied by policy",
			"proposal_fingerprint": "sha256:audited-digest",
		})
		require.NoError(t, err)

		decision, err := dispatcher.HandleVerdict("rejected", "execution-fp-audit", wire)
		require.NoError(t, err)
		require.Equal(t, natsclient.DeliveryDecisionAck, decision)

		record := auditVerdictRecord(t, logs.Bytes())
		require.Equal(t, "sha256:audited-digest", record["proposal_fingerprint"],
			"the decoded fingerprint must reach the audit line, or nothing reads it")
		require.Equal(t, "execution-fp-audit", record["execution_id"],
			"the audit line still identifies the call by execution identity")
	})
}

// auditVerdictRecord returns the one "Audit-mode verdict observed" record in a
// JSON log stream, failing if there is not exactly one.
func auditVerdictRecord(t *testing.T, logs []byte) map[string]any {
	t.Helper()

	var found []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(logs)), "\n") {
		if line == "" {
			continue
		}
		var record map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &record), "log line %q is not JSON", line)
		if record["msg"] == "Audit-mode verdict observed" {
			found = append(found, record)
		}
	}
	require.Len(t, found, 1, "want exactly one audit verdict record, got %d", len(found))
	return found[0]
}
