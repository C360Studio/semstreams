package agenticloop

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type retainedVerdictEvidence struct {
	settlementEvidence
	readVerdict func(context.Context, string, string) (retainedLoopMessage, bool, error)
}

func (e *retainedVerdictEvidence) ReadGovernanceVerdict(ctx context.Context, stream, subject string) (retainedLoopMessage, bool, error) {
	return e.readVerdict(ctx, stream, subject)
}

type retainedVerdictPolicy struct {
	propose func(context.Context, string, string, []agentic.ToolCall) (DispatcherResult, error)
}

func (d retainedVerdictPolicy) Propose(ctx context.Context, loopID, parentID string, calls []agentic.ToolCall) (DispatcherResult, error) {
	return d.propose(ctx, loopID, parentID, calls)
}

func (retainedVerdictPolicy) Mode() string { return ToolCallGovernanceModeEnforce }
func (retainedVerdictPolicy) HandleVerdict(VerdictPayload) (natsclient.DeliveryDecision, error) {
	return natsclient.DeliveryDecisionRetry, errors.New("retained lookup must not simulate live delivery")
}

type retainedVerdictFixture struct {
	c        *Component
	entity   agentic.LoopEntity
	response agentic.AgentResponse
	calls    []agentic.ToolCall
	evidence *retainedVerdictEvidence
	bucket   *settlementBucket
}

func newRetainedVerdictFixture(t *testing.T, count int) retainedVerdictFixture {
	t.Helper()
	c, _ := verdictWireComponent(t)
	c.natsClient = nil // Unit proof inspects the response owner's decision; native tests prove publication.
	loopID := uuid.NewString()
	requestID := loopID + ":req:" + uuid.NewString()
	entity := agentic.NewLoopEntity(loopID, "task-retained", "general", "model", 5)
	entity.ParentLoopID = uuid.NewString()
	request := retainedRequest(t, loopID, requestID)
	evidence := &retainedVerdictEvidence{settlementEvidence: settlementEvidence{request: request, requestFound: true}}
	evidence.readVerdict = func(context.Context, string, string) (retainedLoopMessage, bool, error) {
		return retainedLoopMessage{}, false, nil
	}
	c.settlementEvidence = evidence
	bucket := &settlementBucket{values: map[string][]byte{loopID: settlementLoopRecord(t, entity)}}
	c.loopsBucket = bucket
	for i := range c.config.Ports.Inputs {
		port := &c.config.Ports.Inputs[i]
		if port.Name == "agent.toolcall.approved" || port.Name == "agent.toolcall.rejected" {
			port.Config = component.JetStreamPort{Subjects: []string{port.Name + ".>"}, StreamName: port.Name + "-stream"}
		}
	}
	calls := make([]agentic.ToolCall, count)
	for i := range calls {
		calls[i] = agentic.ToolCall{ID: fmt.Sprintf("call-%d", i), Name: "lookup", Arguments: map[string]any{"index": float64(i)}}
	}
	response := agentic.AgentResponse{RequestID: requestID, Status: agentic.StatusToolCall,
		Message: agentic.ChatMessage{Role: "assistant", ToolCalls: append([]agentic.ToolCall(nil), calls...)}}
	require.NoError(t, stampToolExecutionCorrelation(requestID, calls))
	return retainedVerdictFixture{c: c, entity: entity, response: response, calls: calls, evidence: evidence, bucket: bucket}
}

func (f retainedVerdictFixture) verdict(t *testing.T, index int, decision string) VerdictPayload {
	t.Helper()
	proposal, err := prepareProposedToolCall(f.entity.ID, f.entity.ParentLoopID, f.calls[index])
	require.NoError(t, err)
	verdict := verdictForPublishedProposal(proposal, decision)
	verdict.Reason, verdict.RuleID = "retained policy", "retained-rule"
	return verdict
}

// spec: agentic-governance / Governance verdict correlation survives process replacement
// spec: agentic-governance / Governance verdicts use one registered wire and typed handoff
func TestComponentResponseRetainedVerdicts(t *testing.T) {
	for _, row := range []struct {
		name               string
		approved, rejected bool
		readError          bool
		conflict, carrier  string
		want               natsclient.DeliveryDecision
	}{
		{name: "approved", approved: true, want: natsclient.DeliveryDecisionAck},
		{name: "rejected", rejected: true, want: natsclient.DeliveryDecisionAck},
		{name: "absent", want: natsclient.DeliveryDecisionRetry},
		{name: "opposite-read-fails", approved: true, readError: true, want: natsclient.DeliveryDecisionRetry},
		{name: "both-decisions", approved: true, rejected: true, want: natsclient.DeliveryDecisionQuarantine},
		{name: "loop-conflict", approved: true, conflict: "loop_id", want: natsclient.DeliveryDecisionQuarantine},
		{name: "request-conflict", approved: true, conflict: "request_id", want: natsclient.DeliveryDecisionQuarantine},
		{name: "execution-conflict", approved: true, conflict: "execution_id", want: natsclient.DeliveryDecisionQuarantine},
		{name: "fingerprint-conflict", approved: true, conflict: "proposal_fingerprint", want: natsclient.DeliveryDecisionQuarantine},
		{name: "call-conflict", approved: true, conflict: "call_id", want: natsclient.DeliveryDecisionQuarantine},
		{name: "subject-conflict", approved: true, conflict: "subject", want: natsclient.DeliveryDecisionQuarantine},
		{name: "raw-carrier", approved: true, carrier: "raw", want: natsclient.DeliveryDecisionTerminate},
		{name: "unsupported-carrier", approved: true, carrier: "unsupported", want: natsclient.DeliveryDecisionTerminate},
	} {
		t.Run(row.name, func(t *testing.T) {
			f := newRetainedVerdictFixture(t, 1)
			ctx := t.Context()
			var reads []string
			f.evidence.readVerdict = func(gotCtx context.Context, stream, subject string) (retainedLoopMessage, bool, error) {
				require.Equal(t, ctx, gotCtx)
				reads = append(reads, subject)
				decision := "approved"
				if subject == "agent.toolcall.rejected."+f.calls[0].ExecutionID {
					decision = "rejected"
				}
				require.Equal(t, "agent.toolcall."+decision+"-stream", stream)
				require.Equal(t, "agent.toolcall."+decision+"."+f.calls[0].ExecutionID, subject)
				if decision == "rejected" && row.readError {
					return retainedLoopMessage{}, false, errors.New("verdict read unavailable")
				}
				if (decision == "approved" && !row.approved) || (decision == "rejected" && !row.rejected) {
					return retainedLoopMessage{}, false, nil
				}
				verdict := f.verdict(t, 0, decision)
				switch row.conflict {
				case "loop_id":
					verdict.LoopID = "other-loop"
				case "request_id":
					verdict.RequestID = "other-request"
				case "execution_id":
					verdict.ExecutionID = "other-execution"
				case "proposal_fingerprint":
					verdict.ProposalFingerprint = "other-fingerprint"
				case "call_id":
					verdict.CallID = "other-call"
				case "subject":
					subject = "agent.toolcall.rejected." + verdict.ExecutionID
				}
				data := registeredProposalVerdict(t, verdict, true)
				if row.carrier == "raw" {
					data = []byte(`{"decision":"approved"}`)
				}
				if row.carrier == "unsupported" {
					data = settlementEnvelope(t, &agentic.UserSignal{SignalID: "signal", Type: agentic.SignalCancel, LoopID: f.entity.ID, UserID: "user"})
				}
				return retainedLoopMessage{subject: subject, data: data}, true, nil
			}
			var policyCalls int
			policyErr := errors.New("policy is only permitted after exact absence")
			f.c.handler.SetGovernanceDispatcher(retainedVerdictPolicy{propose: func(context.Context, string, string, []agentic.ToolCall) (DispatcherResult, error) {
				policyCalls++
				return DispatcherResult{}, policyErr
			}})
			before := append([]byte(nil), f.bucket.values[f.entity.ID]...)
			decision, err := f.c.handleResponseMessage(ctx, settlementEnvelope(t, &f.response))
			require.Equal(t, row.want, decision, "response source disposition")
			if row.want == natsclient.DeliveryDecisionAck {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.Equal(t, before, f.bucket.values[f.entity.ID], "failed governance changed durable authority")
				require.Len(t, f.bucket.values, 1, "failed governance created a terminal record")
				_, lookupErr := f.c.handler.GetLoop(f.entity.ID)
				require.Error(t, lookupErr, "failed governance retained speculative process state")
			}
			wantPolicy := 0
			if row.name == "absent" {
				wantPolicy = 1
				require.ErrorIs(t, err, policyErr)
			}
			require.Equal(t, wantPolicy, policyCalls, "retained observation must precede even a custom policy dispatcher")
			require.NotEmpty(t, reads, "Component did not read retained verdict evidence")
			if row.conflict == "" && row.carrier == "" {
				require.Len(t, reads, 2, "both decision subjects are required")
			}
		})
	}
}

// spec: agentic-governance / Governance publications are durably at-least-once
func TestEnforceProposalFailureReturnsError(t *testing.T) {
	for _, row := range []string{"publication", "preparation", "cancellation"} {
		t.Run(row, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calls := []agentic.ToolCall{governanceTestCall("call", "lookup")}
			publisher := proposalTestPublisher(func(context.Context, string, []byte) error {
				if row == "cancellation" {
					cancel()
					return nil
				}
				return errors.New("proposal PubAck unavailable")
			})
			if row == "preparation" {
				calls[0].Arguments = map[string]any{"invalid": make(chan int)}
			}
			d := NewGovernanceDispatcher(ToolCallGovernanceConfig{Mode: ToolCallGovernanceModeEnforce, Timeout: "1s"}, publisher, nil, nil)
			result, err := d.Propose(ctx, "loop-1", "", calls)
			require.Error(t, err, "dependency failure cannot become a policy rejection")
			require.Empty(t, result.Approved)
			require.Empty(t, result.Rejected)
			if row == "cancellation" {
				require.ErrorIs(t, err, context.Canceled)
			}
			require.Empty(t, d.(*enforceDispatcher).waiters)
		})
	}
}

// spec: agentic-governance / Governance publications are durably at-least-once
// spec: agentic-governance / Governance verdict correlation survives process replacement
func TestComponentResponseGovernanceFailureCannotSettleAsRejection(t *testing.T) {
	for _, row := range []string{"publication", "cancellation", "missing-reader"} {
		t.Run(row, func(t *testing.T) {
			f := newRetainedVerdictFixture(t, 1)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var publications int
			publicationErr := errors.New("required proposal PubAck unavailable")
			publisher := proposalTestPublisher(func(context.Context, string, []byte) error {
				publications++
				if row == "cancellation" {
					cancel()
					return nil
				}
				return publicationErr
			})
			f.c.handler.SetGovernanceDispatcher(NewGovernanceDispatcher(ToolCallGovernanceConfig{
				Mode: ToolCallGovernanceModeEnforce, Timeout: "1s",
			}, publisher, nil, nil))
			if row == "missing-reader" {
				f.c.settlementEvidence = &approvalClosureEvidence{loopSettlementEvidenceReader: f.evidence,
					afterRequest: func(context.Context) { f.c.settlementEvidence = nil }}
			}
			before := append([]byte(nil), f.bucket.values[f.entity.ID]...)
			decision, err := f.c.handleResponseMessage(ctx, settlementEnvelope(t, &f.response))
			require.Equal(t, natsclient.DeliveryDecisionRetry, decision)
			require.Error(t, err, "source dependency failure was swallowed as a completed policy rejection")
			if row == "publication" {
				require.ErrorIs(t, err, publicationErr)
			}
			if row == "cancellation" {
				require.ErrorIs(t, err, context.Canceled)
			}
			if row == "missing-reader" {
				require.Zero(t, publications)
			}
			require.Equal(t, before, f.bucket.values[f.entity.ID])
			require.Len(t, f.bucket.values, 1, "failed governance selected a terminal outcome")
			_, lookupErr := f.c.handler.GetLoop(f.entity.ID)
			require.Error(t, lookupErr, "failed governance kept speculative state")
		})
	}
}

// spec: agentic-governance / Governance verdict correlation survives process replacement
func TestComponentResponseRetainedMixedBatch(t *testing.T) {
	f := newRetainedVerdictFixture(t, 4)
	var reads, policyCalls int
	f.evidence.readVerdict = func(_ context.Context, _, subject string) (retainedLoopMessage, bool, error) {
		reads++
		for _, row := range []struct {
			index    int
			decision string
		}{{0, "approved"}, {2, "rejected"}} {
			if subject == "agent.toolcall."+row.decision+"."+f.calls[row.index].ExecutionID {
				verdict := f.verdict(t, row.index, row.decision)
				verdict.CallID = "" // Optional even on the retained route.
				return retainedLoopMessage{subject: subject, data: registeredProposalVerdict(t, verdict, false)}, true, nil
			}
		}
		return retainedLoopMessage{}, false, nil
	}
	f.c.handler.SetGovernanceDispatcher(retainedVerdictPolicy{propose: func(ctx context.Context, loopID, parentID string, calls []agentic.ToolCall) (DispatcherResult, error) {
		policyCalls++
		require.Equal(t, t.Context(), ctx)
		require.Equal(t, f.entity.ID, loopID)
		require.Equal(t, f.entity.ParentLoopID, parentID)
		require.Equal(t, 8, reads, "all required reads must complete before policy")
		require.Equal(t, []agentic.ToolCall{f.calls[1], f.calls[3]}, calls)
		return DispatcherResult{Approved: []agentic.ToolCall{calls[1]}, Rejected: []ToolCallRejection{{Call: calls[0], Reason: "current policy"}}}, nil
	}})
	decision, err := f.c.handleResponseMessage(t.Context(), settlementEnvelope(t, &f.response))
	require.NoError(t, err)
	require.Equal(t, natsclient.DeliveryDecisionAck, decision)
	require.Equal(t, 1, policyCalls)
	require.Equal(t, []string{f.calls[0].ID}, f.c.handler.loopManager.GetPendingTools(f.entity.ID))
	next, ok := f.c.handler.loopManager.DequeueToolCall(f.entity.ID)
	require.True(t, ok)
	require.Equal(t, f.calls[3].ExecutionID, next.ExecutionID)
	require.Equal(t, f.calls[3].RequestID, next.RequestID)
	require.Equal(t, f.calls[3].CallOrdinal, next.CallOrdinal)
	results := f.c.handler.loopManager.GetAndClearToolResults(f.entity.ID)
	require.Len(t, results, 2)
	for _, result := range results {
		if result.ExecutionID == f.calls[1].ExecutionID {
			require.Contains(t, result.Error, "current policy")
		} else {
			require.Equal(t, f.calls[2].ExecutionID, result.ExecutionID)
			require.Contains(t, result.Error, "retained policy (rule_id=retained-rule)")
		}
	}
}

// spec: agentic-governance / Governance verdict correlation survives process replacement
func TestComponentResponseGovernanceReadFailurePrecedesPolicyForWholeBatch(t *testing.T) {
	f := newRetainedVerdictFixture(t, 2)
	readErr := errors.New("second call verdict read failed")
	f.evidence.readVerdict = func(_ context.Context, _, subject string) (retainedLoopMessage, bool, error) {
		if subject == "agent.toolcall.rejected."+f.calls[1].ExecutionID {
			return retainedLoopMessage{}, false, readErr
		}
		return retainedLoopMessage{}, false, nil
	}
	policyCalls := 0
	f.c.handler.SetGovernanceDispatcher(retainedVerdictPolicy{propose: func(context.Context, string, string, []agentic.ToolCall) (DispatcherResult, error) {
		policyCalls++
		return DispatcherResult{}, nil
	}})
	decision, err := f.c.handleResponseMessage(t.Context(), settlementEnvelope(t, &f.response))
	require.ErrorIs(t, err, readErr)
	require.Equal(t, natsclient.DeliveryDecisionRetry, decision)
	require.Zero(t, policyCalls, "the first absent call must not reach policy before the second read fails")
}

// spec: agentic-governance / Governance verdict correlation survives process replacement
func TestComponentResponseGovernanceUnresolvedAddressRetries(t *testing.T) {
	for _, shape := range []string{"missing", "core", "wrong-subject", "empty-stream"} {
		t.Run(shape, func(t *testing.T) {
			f := newRetainedVerdictFixture(t, 1)
			f.c.config.ToolCallGovernance.Mode = ToolCallGovernanceModeEnforce
			// Replacing the configured enforce dispatcher with a pass-through must
			// not bypass its response owner's required retained observation.
			f.c.handler.SetGovernanceDispatcher(NewGovernanceDispatcher(ToolCallGovernanceConfig{Mode: ToolCallGovernanceModeDisabled}, nil, nil, nil))
			for i := range f.c.config.Ports.Inputs {
				port := &f.c.config.Ports.Inputs[i]
				if port.Name != "agent.toolcall.approved" {
					continue
				}
				switch shape {
				case "missing":
					port.Name = "other"
				case "core":
					port.Config = component.NATSPort{Subject: "agent.toolcall.approved.>"}
				case "wrong-subject":
					port.Config = component.JetStreamPort{Subjects: []string{"other.>"}, StreamName: "OTHER"}
				case "empty-stream":
					port.Config = component.JetStreamPort{Subjects: []string{"agent.toolcall.approved.>"}}
				}
			}
			decision, err := f.c.handleResponseMessage(t.Context(), settlementEnvelope(t, &f.response))
			require.Error(t, err)
			require.Equal(t, natsclient.DeliveryDecisionRetry, decision)
			require.Len(t, f.bucket.values, 1)
		})
	}
}
