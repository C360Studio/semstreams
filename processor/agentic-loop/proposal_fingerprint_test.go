package agenticloop

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/stretchr/testify/require"
)

// ProposalFingerprint is a CARRIED correlation token, not a verified one, and
// this pins both halves of that so neither can change silently.
//
// Carried: agentic-loop mints it onto every proposed call, the rule engine
// echoes it back on the verdict (processor/rule/actions.go:2218) and
// agentic-loop decodes it into VerdictPayload (component.go:2744, and
// governance_dispatcher.go:273 for the shape that nests it under
// `properties`), where audit mode logs it as context on the observed-verdict
// line (governance_dispatcher.go:437). Three consumers read the field, so it
// is not dead surface — and the decoded half has an observer, so "decodes it
// as audit context" is a claim the code backs rather than a sentence about it.
//
// Not verified: nothing compares the verdict's fingerprint against the
// proposal's, so a verdict that disagrees is still delivered to its waiter.
// Enforcing the comparison needs the proposal's fingerprint to outlive the
// process that registered the waiter — durable per-call governance state this
// change does not own. NO LAYER OF THIS STACK VERIFIES IT: L4 (#1330) carries
// durable loop state, not durable per-call proposal state, so absent a new
// issue the fingerprint is an audit token only. The "disagreeing fingerprint"
// subtest is the observer that will fail the day someone implements a
// comparison, forcing the spec to move with the code.
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

		decision, err := dispatcher.HandleVerdict("approved", "execution-fp-1", VerdictPayload{
			Decision:            "approved",
			ExecutionID:         "execution-fp-1",
			ProposalFingerprint: "sha256:not-the-proposals-digest",
		})
		require.NoError(t, err)
		require.Equal(t, natsclient.DeliveryDecisionAck, decision,
			"routing is by execution identity alone; the fingerprint is audit context")
		require.Equal(t, "approved", (<-waiter).decision)
	})

	// The audit line is asserted over the two shapes a rule actually
	// publishes, not over a flat map nothing emits. The flat map was the
	// defect: the dispatcher unmarshalled the raw bytes itself, which reads
	// every field of a BaseMessage envelope and of a publish-action verdict
	// as the empty string, so the line that exists to prove the fingerprint
	// has a reader logged "" for every production verdict and passed.
	t.Run("both production wire shapes carry their fingerprint to the audit line", func(t *testing.T) {
		for _, shape := range []struct {
			name     string
			wire     func(t *testing.T) []byte
			decision string
			ruleID   string
		}{
			{
				name:     "approve action, BaseMessage envelope, fields at the top level",
				wire:     approveActionVerdictWire,
				decision: "approved",
				ruleID:   "rule-fp-audit",
			},
			{
				name: "publish action, raw map, fields under properties",
				wire: publishActionVerdictWire,
				// The canonical reject rules echo no rule_id, so the empty
				// string here is the shape's truth, not a lost field.
				decision: "rejected",
				ruleID:   "",
			},
		} {
			t.Run(shape.name, func(t *testing.T) {
				var logs bytes.Buffer
				c := verdictTestComponent(t)
				c.handler.SetGovernanceDispatcher(NewGovernanceDispatcher(
					ToolCallGovernanceConfig{Mode: ToolCallGovernanceModeAudit},
					&mockVerdictPublisher{},
					slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo})),
					nil,
				))

				decision, err := c.handleToolCallVerdictMessage(t.Context(), shape.wire(t))
				require.NoError(t, err)
				require.Equal(t, natsclient.DeliveryDecisionAck, decision)

				record := auditVerdictRecord(t, logs.Bytes())
				require.Equal(t, auditedFingerprint, record["proposal_fingerprint"],
					"the fingerprint the rule echoed did not reach the audit line for this shape")
				require.Equal(t, auditedExecutionID, record["execution_id"])
				require.Equal(t, shape.decision, record["decision"])
				require.Equal(t, auditedReason, record["reason"])
				require.Equal(t, shape.ruleID, record["rule_id"])
			})
		}
	})

	// Enforce mode reads the same fields, and there the loss is not audit:
	// the reason travels to the waiting Propose and becomes the text the
	// model is told its call was refused with.
	t.Run("a publish-action rejection reaches its waiter with the reason on it", func(t *testing.T) {
		c := verdictTestComponent(t)
		dispatcher := NewGovernanceDispatcher(
			ToolCallGovernanceConfig{Mode: ToolCallGovernanceModeEnforce, Timeout: "1s"},
			&mockVerdictPublisher{}, slog.Default(), nil,
		).(*enforceDispatcher)
		c.handler.SetGovernanceDispatcher(dispatcher)

		waiter := dispatcher.registerWaiter(auditedExecutionID)
		defer dispatcher.releaseWaiter(auditedExecutionID)

		decision, err := c.handleToolCallVerdictMessage(t.Context(), publishActionVerdictWire(t))
		require.NoError(t, err)
		require.Equal(t, natsclient.DeliveryDecisionAck, decision)

		arrival := <-waiter
		require.Equal(t, "rejected", arrival.decision)
		require.Equal(t, auditedReason, arrival.reason,
			"a rejection with no reason on it is what the model would have been told")
	})
}

// The one verdict the two shape builders below describe, so an assertion
// names the same value whichever shape carried it.
const (
	auditedExecutionID = "tool-exec-v1-fp-audit"
	auditedFingerprint = "sha256:audited-digest"
	auditedReason      = "bash disallowed by policy"
)

// verdictTestComponent builds the production verdict path: the real decoder
// over the real payload registry, feeding the real handleToolCallVerdictMessage.
func verdictTestComponent(t *testing.T) *Component {
	t.Helper()
	discoverable, err := NewComponent([]byte(`{}`), component.Dependencies{
		NATSClient: &natsclient.Client{}, PayloadRegistry: payloadbuiltins.NewTestRegistry(t),
	})
	require.NoError(t, err)
	return discoverable.(*Component)
}

// approveActionVerdictWire reproduces what the rule engine's `approve` action
// puts on agent.toolcall.approved.<execution_id>: the verdict fields in a
// GenericJSON payload inside a core.json.v1 BaseMessage
// (processor/rule/actions.go:2197-2231).
func approveActionVerdictWire(t *testing.T) []byte {
	t.Helper()
	generic := message.NewGenericJSON(map[string]any{
		"decision":             "approved",
		"rule_id":              "rule-fp-audit",
		"reason":               auditedReason,
		"entity_id":            "acme.ops.semstreams.agentic.toolcall.fp-audit",
		"timestamp":            time.Now().Format(time.RFC3339Nano),
		"call_id":              "call-001",
		"loop_id":              "7c9e6679-7425-40de-944b-e07fc1f90ae7",
		"request_id":           "7c9e6679-7425-40de-944b-e07fc1f90ae7:req:2:0",
		"execution_id":         auditedExecutionID,
		"proposal_fingerprint": auditedFingerprint,
	})
	data, err := json.Marshal(message.NewBaseMessage(generic.Schema(), generic, "rule_engine"))
	require.NoError(t, err)
	return data
}

// publishActionVerdictWire reproduces what the canonical ADR-039 reject
// pattern puts on agent.toolcall.rejected.<execution_id>: a raw map whose
// verdict fields all live under `properties`
// (processor/rule/actions.go:1172-1179, and the rule set in
// docs/operations/17-tool-call-governance.md:117-128).
func publishActionVerdictWire(t *testing.T) []byte {
	t.Helper()
	data, err := json.Marshal(map[string]any{
		"entity_id": "acme.ops.semstreams.agentic.toolcall.fp-audit",
		"subject":   "agent.toolcall.rejected." + auditedExecutionID,
		"timestamp": time.Now().Format(time.RFC3339Nano),
		"source":    "rule_engine",
		"properties": map[string]any{
			"decision":             "rejected",
			"request_id":           "7c9e6679-7425-40de-944b-e07fc1f90ae7:req:2:0",
			"execution_id":         auditedExecutionID,
			"call_id":              "call-001",
			"proposal_fingerprint": auditedFingerprint,
			"reason":               auditedReason,
		},
	})
	require.NoError(t, err)
	return data
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
