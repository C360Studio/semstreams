package agenticloop

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/stretchr/testify/require"
)

type proposalTestPublisher func(context.Context, string, []byte) error

func (p proposalTestPublisher) PublishToStream(ctx context.Context, subject string, data []byte) error {
	return p(ctx, subject, data)
}

func publishedGovernanceProposal(t *testing.T, data []byte) ProposedToolCallPayload {
	t.Helper()
	decoded, err := message.NewDecoder(payloadbuiltins.NewTestRegistry(t)).Decode(data)
	require.NoError(t, err)
	require.NoError(t, decoded.Validate())
	payload, ok := decoded.Payload().(*message.GenericJSONPayload)
	require.True(t, ok)
	fields, err := json.Marshal(payload.Data)
	require.NoError(t, err)
	var proposal ProposedToolCallPayload
	require.NoError(t, json.Unmarshal(fields, &proposal))
	return proposal
}

func verdictForPublishedProposal(proposal ProposedToolCallPayload, decision string) VerdictPayload {
	return VerdictPayload{Decision: decision, LoopID: proposal.LoopID, RequestID: proposal.RequestID,
		ExecutionID: proposal.ExecutionID, CallID: proposal.CallID, ProposalFingerprint: proposal.ProposalFingerprint}
}

func registeredProposalVerdict(t *testing.T, verdict VerdictPayload, nested bool) []byte {
	t.Helper()
	data, err := json.Marshal(verdict)
	require.NoError(t, err)
	var fields map[string]any
	require.NoError(t, json.Unmarshal(data, &fields))
	if nested {
		fields = map[string]any{"properties": fields}
	}
	return settlementEnvelope(t, message.NewGenericJSON(fields))
}

// spec: agentic-governance / Governance publications are durably at-least-once
// spec: agentic-governance / Governance verdicts use one registered wire and typed handoff
func TestGovernanceLiveProposalMismatchCannotConsumeWaiter(t *testing.T) {
	for _, route := range []string{"direct", "wire-flat", "wire-nested"} {
		for _, field := range []string{"loop_id", "request_id", "proposal_fingerprint", "call_id"} {
			t.Run(route+"/"+field, func(t *testing.T) {
				c, _ := verdictWireComponent(t)
				var dispatcher GovernanceDispatcher
				publisher := proposalTestPublisher(func(_ context.Context, _ string, data []byte) error {
					proposal := publishedGovernanceProposal(t, data)
					matching := verdictForPublishedProposal(proposal, "approved")
					conflict := matching
					switch field {
					case "loop_id":
						conflict.LoopID = "other-loop"
					case "request_id":
						conflict.RequestID = "other-request"
					case "proposal_fingerprint":
						conflict.ProposalFingerprint = "other-fingerprint"
					case "call_id":
						conflict.CallID = "other-call"
					}
					var decision natsclient.DeliveryDecision
					var err error
					if route == "direct" {
						decision, err = dispatcher.HandleVerdict(conflict)
					} else {
						decision, err = c.handleToolCallVerdictMessage(t.Context(), "agent.toolcall.approved."+proposal.ExecutionID,
							registeredProposalVerdict(t, conflict, route == "wire-nested"))
					}
					require.Equal(t, natsclient.DeliveryDecisionQuarantine, decision, "wrong originating proposal must not be admitted")
					require.ErrorContains(t, err, field)
					// Propose has not begun waiting yet. A matching verdict must still fit
					// in its single buffered slot after the conflicting verdict is refused.
					decision, err = dispatcher.HandleVerdict(matching)
					require.NoError(t, err)
					require.Equal(t, natsclient.DeliveryDecisionAck, decision)
					return nil
				})
				dispatcher = NewGovernanceDispatcher(ToolCallGovernanceConfig{Mode: ToolCallGovernanceModeEnforce, Timeout: "1s"}, publisher, nil, nil)
				c.handler.SetGovernanceDispatcher(dispatcher)
				calls := []agentic.ToolCall{governanceTestCall("provider-call", "lookup")}
				result, err := dispatcher.Propose(t.Context(), "loop-1", "parent", calls)
				require.NoError(t, err)
				require.Equal(t, calls, result.Approved)
				require.Empty(t, result.Rejected)
			})
		}
	}
}

// spec: agentic-governance / Governance verdicts use one registered wire and typed handoff
func TestGovernanceLiveProposalOptionalCallAndDiagnostics(t *testing.T) {
	for _, decision := range []string{"approved", "rejected"} {
		for _, omitCall := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/omit-call-%v", decision, omitCall), func(t *testing.T) {
				c, _ := verdictWireComponent(t)
				var dispatcher GovernanceDispatcher
				publisher := proposalTestPublisher(func(_ context.Context, _ string, data []byte) error {
					verdict := verdictForPublishedProposal(publishedGovernanceProposal(t, data), decision)
					if omitCall {
						verdict.CallID = ""
					}
					verdict.Reason, verdict.RuleID = "top reason", "top rule"
					verdict.Properties = map[string]any{"reason": "nested reason", "rule_id": "nested rule"}
					outcome, err := c.handleToolCallVerdictMessage(t.Context(), "agent.toolcall."+decision+"."+verdict.ExecutionID,
						registeredProposalVerdict(t, verdict, false))
					require.NoError(t, err)
					require.Equal(t, natsclient.DeliveryDecisionAck, outcome)
					return nil
				})
				dispatcher = NewGovernanceDispatcher(ToolCallGovernanceConfig{Mode: ToolCallGovernanceModeEnforce, Timeout: "1s"}, publisher, nil, nil)
				c.handler.SetGovernanceDispatcher(dispatcher)
				calls := []agentic.ToolCall{governanceTestCall("provider-call", "lookup")}
				result, err := dispatcher.Propose(t.Context(), "loop-1", "", calls)
				require.NoError(t, err)
				if decision == "approved" {
					require.Equal(t, calls, result.Approved)
					require.Empty(t, result.Rejected)
				} else {
					require.Empty(t, result.Approved)
					require.Equal(t, []ToolCallRejection{{Call: calls[0], Reason: "top reason (rule_id=top rule)"}}, result.Rejected)
				}
			})
		}
	}
}

// spec: agentic-governance / Governance publications are durably at-least-once
// spec: agentic-loop / Tool execution has stable framework correlation
func TestGovernanceLiveProposalRepeatedProviderCallsStaySeparate(t *testing.T) {
	calls := []agentic.ToolCall{{ID: "repeat", Name: "lookup"}, {ID: "repeat", Name: "lookup"}}
	require.NoError(t, stampToolExecutionCorrelation("request-a", calls))
	other := []agentic.ToolCall{{ID: "repeat", Name: "lookup"}}
	require.NoError(t, stampToolExecutionCorrelation("request-b", other))
	calls = append(calls, other...)
	var proposals []ProposedToolCallPayload
	var dispatcher GovernanceDispatcher
	publisher := proposalTestPublisher(func(_ context.Context, _ string, data []byte) error {
		proposals = append(proposals, publishedGovernanceProposal(t, data))
		if len(proposals) != len(calls) {
			return nil
		}
		conflict := verdictForPublishedProposal(proposals[0], "approved")
		conflict.ExecutionID = proposals[1].ExecutionID
		decision, err := dispatcher.HandleVerdict(conflict)
		require.Equal(t, natsclient.DeliveryDecisionQuarantine, decision)
		require.ErrorContains(t, err, "proposal_fingerprint")
		for i := len(proposals) - 1; i >= 0; i-- {
			verdictDecision := "approved"
			if i == 1 {
				verdictDecision = "rejected"
			}
			decision, err := dispatcher.HandleVerdict(verdictForPublishedProposal(proposals[i], verdictDecision))
			require.NoError(t, err)
			require.Equal(t, natsclient.DeliveryDecisionAck, decision)
		}
		return nil
	})
	dispatcher = NewGovernanceDispatcher(ToolCallGovernanceConfig{Mode: ToolCallGovernanceModeEnforce, Timeout: "1s"}, publisher, nil, nil)
	result, err := dispatcher.Propose(t.Context(), "loop-1", "", calls)
	require.NoError(t, err)
	require.Equal(t, []agentic.ToolCall{calls[0], calls[2]}, result.Approved)
	require.Len(t, result.Rejected, 1)
	require.Equal(t, calls[1], result.Rejected[0].Call)
}

// spec: agentic-governance / Governance verdict correlation survives process replacement
func TestGovernanceLiveProposalPreservesPublishedFingerprint(t *testing.T) {
	call := agentic.ToolCall{ID: "provider-call", Name: "http_request", RequestID: "request-a", ExecutionID: "execution-a",
		CallOrdinal: 2, Arguments: map[string]any{"command": "echo ready", "url": "https://example.test/reference", "depth": float64(2)}}
	var dispatcher GovernanceDispatcher
	publisher := proposalTestPublisher(func(_ context.Context, subject string, data []byte) error {
		proposal := publishedGovernanceProposal(t, data)
		require.Equal(t, "agent.toolcall.proposed.loop-a", subject)
		expected := ProposedToolCallPayload{LoopID: "loop-a", ParentLoopID: "parent-a", RequestID: "request-a",
			ExecutionID: "execution-a", CallID: "provider-call", CallOrdinal: 2, ToolName: "http_request",
			Command: "echo ready", URL: "https://example.test/reference", Arguments: call.Arguments}
		// Fixed pre-extraction serialized contract, independent of the private builder.
		fingerprintInput := `{"loop_id":"loop-a","parent_loop_id":"parent-a","request_id":"request-a","execution_id":"execution-a","call_id":"provider-call","call_ordinal":2,"proposal_fingerprint":"","tool_name":"http_request","command":"echo ready","url":"https://example.test/reference","arguments":{"command":"echo ready","depth":2,"url":"https://example.test/reference"}}`
		expected.ProposalFingerprint = fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(fingerprintInput)))
		require.Equal(t, expected, proposal)
		// Changes to the original top-level arguments after publication do not rewrite
		// the proposal that already established this waiter's identity.
		call.Arguments["command"] = "changed after publication"
		decision, err := dispatcher.HandleVerdict(verdictForPublishedProposal(proposal, "approved"))
		require.NoError(t, err)
		require.Equal(t, natsclient.DeliveryDecisionAck, decision)
		return nil
	})
	dispatcher = NewGovernanceDispatcher(ToolCallGovernanceConfig{Mode: ToolCallGovernanceModeEnforce, Timeout: "1s"}, publisher, nil, nil)
	result, err := dispatcher.Propose(t.Context(), "loop-a", "parent-a", []agentic.ToolCall{call})
	require.NoError(t, err)
	require.Len(t, result.Approved, 1)
	require.Empty(t, result.Rejected)
}
