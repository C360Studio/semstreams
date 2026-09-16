package agenticloop

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"testing"
	"unicode/utf8"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/c360studio/semstreams/payloadregistry"
	"github.com/stretchr/testify/require"
)

func verdictWireComponent(t *testing.T) (*Component, *enforceDispatcher) {
	t.Helper()
	discoverable, err := NewComponent([]byte(`{}`), component.Dependencies{
		NATSClient: &natsclient.Client{}, PayloadRegistry: payloadbuiltins.NewTestRegistry(t),
	})
	require.NoError(t, err)
	c := discoverable.(*Component)
	d := &enforceDispatcher{logger: slog.New(slog.NewTextHandler(io.Discard, nil)), waiters: make(map[string]verdictWaiter)}
	c.handler.SetGovernanceDispatcher(d)
	return c, d
}

// spec: agentic-governance / Governance verdicts use one registered wire and typed handoff
func TestGovernanceVerdictRequiredCorrelationRefusesBeforeWaiter(t *testing.T) {
	for _, name := range []string{"decision", "loop_id", "request_id", "execution_id", "proposal_fingerprint", "call_id"} {
		for _, location := range []string{"top", "properties"} {
			for _, bad := range []any{nil, true, float64(1), []any{}, map[string]any{}, ""} {
				if name == "call_id" && bad == "" {
					continue
				}
				t.Run(fmt.Sprintf("%s/%s/%T-%v", name, location, bad, bad), func(t *testing.T) {
					fields := validVerdictFields()
					if location == "properties" {
						fields = map[string]any{"properties": fields}
					}
					if location == "top" {
						fields[name] = bad
					} else {
						fields["properties"].(map[string]any)[name] = bad
					}
					c, d := verdictWireComponent(t)
					waiter := d.registerWaiter(governanceTestProposal("exec-1")).arrivals
					decision, err := c.handleToolCallVerdictMessage(t.Context(), "agent.toolcall.rejected.exec-1", settlementEnvelope(t, message.NewGenericJSON(fields)))
					require.Error(t, err)
					require.Equal(t, natsclient.DeliveryDecisionTerminate, decision)
					require.Empty(t, waiter)
					if direct, projectionErr := verdictPayloadFromMap(fields); projectionErr == nil {
						decision, err = d.HandleVerdict(direct)
						require.Error(t, err)
						require.Equal(t, natsclient.DeliveryDecisionTerminate, decision)
						require.Empty(t, waiter)
					}
				})
			}
		}
		if name != "call_id" {
			t.Run(name+"/missing", func(t *testing.T) {
				fields := validVerdictFields()
				delete(fields, name)
				c, d := verdictWireComponent(t)
				waiter := d.registerWaiter(governanceTestProposal("exec-1")).arrivals
				decision, err := c.handleToolCallVerdictMessage(t.Context(), "agent.toolcall.rejected.exec-1", settlementEnvelope(t, message.NewGenericJSON(fields)))
				require.Error(t, err)
				require.Equal(t, natsclient.DeliveryDecisionTerminate, decision)
				require.Empty(t, waiter)
			})
		}
	}
}

// spec: agentic-governance / Governance verdicts use one registered wire and typed handoff
func TestGovernanceVerdictCorrelationConflicts(t *testing.T) {
	for _, name := range []string{"decision", "loop_id", "request_id", "execution_id", "proposal_fingerprint", "call_id"} {
		t.Run(name, func(t *testing.T) {
			fields := validVerdictFields()
			fields["call_id"] = "call-1"
			fields["properties"] = map[string]any{name: "conflicting"}
			c, d := verdictWireComponent(t)
			waiter := d.registerWaiter(governanceTestProposal("exec-1")).arrivals
			decision, err := c.handleToolCallVerdictMessage(t.Context(), "agent.toolcall.rejected.exec-1", settlementEnvelope(t, message.NewGenericJSON(fields)))
			require.Error(t, err)
			require.Equal(t, natsclient.DeliveryDecisionQuarantine, decision)
			require.Empty(t, waiter)
			var direct VerdictPayload
			data, err := json.Marshal(fields)
			require.NoError(t, err)
			require.NoError(t, json.Unmarshal(data, &direct))
			decision, err = d.HandleVerdict(direct)
			require.Error(t, err)
			require.Equal(t, natsclient.DeliveryDecisionQuarantine, decision)
			require.Empty(t, waiter)
		})
	}
}

// spec: agentic-governance / Governance verdicts use one registered wire and typed handoff
func TestGovernanceVerdictDiagnosticsAndDirectParity(t *testing.T) {
	for _, row := range []struct {
		name        string
		top, nested any
		want        string
	}{
		{"top", "top", nil, "top"}, {"nested", nil, "nested", "nested"},
		{"empty-top", "", "nested", "nested"}, {"disagree", "top", "nested", "top"},
		{"omitted", nil, nil, ""}, {"malformed-optional", float64(4), true, ""},
	} {
		t.Run(row.name, func(t *testing.T) {
			for _, mode := range []string{ToolCallGovernanceModeDisabled, ToolCallGovernanceModeAudit, ToolCallGovernanceModeEnforce} {
				t.Run(mode, func(t *testing.T) {
					var log bytes.Buffer
					logger := slog.New(slog.NewJSONHandler(&log, nil))
					c, _ := verdictWireComponent(t)
					d := NewGovernanceDispatcher(ToolCallGovernanceConfig{Mode: mode}, nil, logger, nil)
					c.handler.SetGovernanceDispatcher(d)
					fields := validVerdictFields()
					delete(fields, "reason")
					delete(fields, "rule_id")
					props := map[string]any{}
					if row.top != nil {
						fields["reason"], fields["rule_id"] = row.top, row.top
					}
					if row.nested != nil {
						props["reason"], props["rule_id"] = row.nested, row.nested
					}
					fields["properties"] = props
					original := maps.Clone(props)
					var waiter chan verdictArrival
					if enforce, ok := d.(*enforceDispatcher); ok {
						waiter = enforce.registerWaiter(governanceTestProposal("exec-1")).arrivals
					}
					decision, err := c.handleToolCallVerdictMessage(t.Context(), "agent.toolcall.rejected.exec-1", settlementEnvelope(t, message.NewGenericJSON(fields)))
					require.NoError(t, err)
					require.Equal(t, natsclient.DeliveryDecisionAck, decision)
					if waiter != nil {
						require.Len(t, waiter, 1)
						require.Equal(t, verdictArrival{decision: "rejected", reason: row.want, ruleID: row.want}, <-waiter)
					}
					if mode == ToolCallGovernanceModeAudit {
						var observed map[string]any
						require.NoError(t, json.Unmarshal(log.Bytes(), &observed))
						require.Equal(t, row.want, observed["reason"])
						require.Equal(t, row.want, observed["rule_id"])
					}
					direct := governanceTestVerdict("rejected", "exec-1")
					direct.Reason, _ = row.top.(string)
					direct.RuleID, _ = row.top.(string)
					direct.Properties = props
					decision, err = d.HandleVerdict(direct)
					require.NoError(t, err)
					require.Equal(t, natsclient.DeliveryDecisionAck, decision)
					if waiter != nil {
						require.Len(t, waiter, 1)
						require.Equal(t, verdictArrival{decision: "rejected", reason: row.want, ruleID: row.want}, <-waiter)
					}
					require.Equal(t, original, props, "normalization modified caller map")
				})
			}
		})
	}
}

// spec: agentic-governance / Governance verdicts use one registered wire and typed handoff
func TestGovernanceVerdictSubjectAndEnvelopeRefusals(t *testing.T) {
	t.Run("unsupported-registered-payload", func(t *testing.T) {
		c, d := verdictWireComponent(t)
		waiter := d.registerWaiter(governanceTestProposal("exec-1")).arrivals
		data := settlementEnvelope(t, &agentic.UserSignal{SignalID: "signal", Type: agentic.SignalCancel,
			LoopID: "33333333-3333-4333-8333-333333333333", UserID: "user"})
		decision, err := c.handleToolCallVerdictMessage(t.Context(), "agent.toolcall.rejected.exec-1", data)
		require.ErrorContains(t, err, "GenericJSON")
		require.Equal(t, natsclient.DeliveryDecisionTerminate, decision)
		require.Empty(t, waiter)
	})
	for _, subject := range []string{"", "agent.task.exec-1", "agent.toolcall.observed.exec-1", "agent.toolcall.approved.exec-1",
		"agent.toolcall.rejected", "agent.toolcall.rejected.other", "agent.toolcall.rejected.exec-1.extra"} {
		t.Run("subject/"+subject, func(t *testing.T) {
			c, d := verdictWireComponent(t)
			fields := validVerdictFields()
			fields["subject"] = "agent.toolcall.rejected.exec-1" // Metadata cannot override actual transport identity.
			waiter := d.registerWaiter(governanceTestProposal("exec-1")).arrivals
			decision, err := c.handleToolCallVerdictMessage(t.Context(), subject, settlementEnvelope(t, message.NewGenericJSON(fields)))
			require.Error(t, err)
			require.Equal(t, natsclient.DeliveryDecisionQuarantine, decision)
			require.Empty(t, waiter)
		})
	}
	for _, execution := range []string{"a.b", "*", ">", "a*", "a>", "a b", "a\tb", "a\nb", "a\u00a0b"} {
		t.Run("token/"+execution, func(t *testing.T) {
			c, d := verdictWireComponent(t)
			fields := validVerdictFields()
			fields["execution_id"] = execution
			waiter := d.registerWaiter(governanceTestProposal(execution)).arrivals
			decision, err := c.handleToolCallVerdictMessage(t.Context(), "agent.toolcall.rejected."+execution, settlementEnvelope(t, message.NewGenericJSON(fields)))
			require.Error(t, err)
			require.Equal(t, natsclient.DeliveryDecisionTerminate, decision)
			require.Empty(t, waiter)
		})
	}
	for _, data := range []string{`{`, `{}`, `null`, `{"type":{"domain":"missing","category":"json","version":"v1"}}`,
		`{"type":{"domain":"core","category":"json","version":"v1"},"payload":{"data":null}}`} {
		t.Run("envelope/"+data, func(t *testing.T) {
			c, d := verdictWireComponent(t)
			waiter := d.registerWaiter(governanceTestProposal("exec-1")).arrivals
			decision, err := c.handleToolCallVerdictMessage(t.Context(), "agent.toolcall.rejected.exec-1", []byte(data))
			require.Error(t, err)
			require.Equal(t, natsclient.DeliveryDecisionTerminate, decision)
			require.Empty(t, waiter)
		})
	}
	for _, props := range []any{nil, true, "not-object", []any{}} {
		t.Run(fmt.Sprintf("container/%T", props), func(t *testing.T) {
			c, d := verdictWireComponent(t)
			fields := validVerdictFields()
			fields["properties"] = props
			waiter := d.registerWaiter(governanceTestProposal("exec-1")).arrivals
			decision, err := c.handleToolCallVerdictMessage(t.Context(), "agent.toolcall.rejected.exec-1", settlementEnvelope(t, message.NewGenericJSON(fields)))
			require.Error(t, err)
			require.Equal(t, natsclient.DeliveryDecisionTerminate, decision)
			require.Empty(t, waiter)
		})
	}
}

// spec: agentic-governance / Governance verdicts use one registered wire and typed handoff
// spec: agentic-governance / Governance publications are durably at-least-once
func FuzzGovernanceVerdictBoundary(f *testing.F) {
	decoder := message.NewDecoder(payloadregistry.NewWithSubset(f, message.RegisterPayloads))
	validGeneric := message.NewGenericJSON(validVerdictFields())
	validWire, err := json.Marshal(message.NewBaseMessage(validGeneric.Schema(), validGeneric, "rule_engine"))
	require.NoError(f, err)
	for _, seed := range []struct{ decision, execution, diagnostic, props string }{
		{"approved", "exec-1", "top", `{}`}, {"rejected", "exec-1", "", `{"reason":"nested","rule_id":"nested"}`},
		{"", "", "", `{"decision":"rejected","execution_id":"exec-1","reason":"nested","rule_id":"nested"}`},
		{"rejected", "exec-1", "", `{"decision":"approved"}`}, {"", "exec-1", "", `{}`},
		{"other", "exec-1", "", `{}`}, {"rejected", "a.b", "", `{}`}, {"rejected", "*", "", `{}`},
		{"rejected", "a b", "", `{}`}, {"rejected", "exec-1", "", `{"loop_id":1}`},
		{"rejected", "exec-1", "top", `{"reason":"different","rule_id":false}`}, {"rejected", "exec-1", "", `{`},
		{"approved", "exec-1", "", `{"loop_id":"other-loop"}`},
		{"rejected", "exec-1", "", `{"request_id":"other-request"}`},
		{"approved", "exec-1", "", `{"proposal_fingerprint":"other-fingerprint"}`},
		{"rejected", "exec-1", "", `{"call_id":"other-call"}`},
		{"approved", "exec-1", "top", `{"loop_id":"loop-1","request_id":"request-1","proposal_fingerprint":"fingerprint","call_id":"call-1","reason":"different"}`},
		{"approved", "another-execution", "", `{}`},
	} {
		f.Add("agent.toolcall."+seed.decision+"."+seed.execution, seed.decision, seed.execution, seed.diagnostic, seed.props, validWire)
	}
	f.Add("agent.toolcall.rejected.exec-1", "rejected", "exec-1", "", `{}`, []byte(`{`))
	f.Add("agent.toolcall.rejected.exec-1", "", "", "", `{"decision":"rejected","execution_id":"exec-1","loop_id":"loop-1","request_id":"request-1","proposal_fingerprint":"fingerprint"}`, validWire)
	f.Fuzz(func(t *testing.T, subject, decision, execution, diagnostic, propertiesJSON string, arbitraryWire []byte) {
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		arbitraryDispatcher := &enforceDispatcher{logger: logger, waiters: make(map[string]verdictWaiter)}
		arbitraryWaiter := arbitraryDispatcher.registerWaiter(governanceTestProposal("exec-1")).arrivals
		arbitraryComponent := &Component{decoder: decoder, handler: &MessageHandler{governanceDispatcher: arbitraryDispatcher}}
		arbitraryDecision, arbitraryErr := arbitraryComponent.handleToolCallVerdictMessage(t.Context(), subject, arbitraryWire)
		if arbitraryErr != nil {
			require.NotEqual(t, natsclient.DeliveryDecisionAck, arbitraryDecision)
			require.Empty(t, arbitraryWaiter)
		}
		var props map[string]any
		if err := json.Unmarshal([]byte(propertiesJSON), &props); err != nil {
			return
		}
		// JSON replaces invalid UTF-8. Those byte strings do not have an equivalent
		// wire representation; the arbitrary-envelope refusal property above still runs.
		if !utf8.ValidString(decision) || !utf8.ValidString(execution) || !utf8.ValidString(diagnostic) {
			return
		}
		direct := governanceTestVerdict(decision, execution)
		direct.Properties = props
		direct.Reason, direct.RuleID = diagnostic, diagnostic
		// Properties supplies arbitrary correlation without a fixed top-level value
		// masking it. The expected proposal remains fixed, independent of this input.
		if _, supplied := props["loop_id"]; supplied {
			direct.LoopID = ""
		}
		if _, supplied := props["request_id"]; supplied {
			direct.RequestID = ""
		}
		if _, supplied := props["proposal_fingerprint"]; supplied {
			direct.ProposalFingerprint = ""
		}
		before, err := json.Marshal(props)
		require.NoError(t, err)
		d := &enforceDispatcher{logger: logger, waiters: make(map[string]verdictWaiter)}
		waiter := d.registerWaiter(governanceTestProposal("exec-1")).arrivals
		directDecision, directErr := d.HandleVerdict(direct)
		var expected verdictArrival
		if directErr == nil {
			require.Equal(t, natsclient.DeliveryDecisionAck, directDecision)
			require.Len(t, waiter, 1)
			expected = <-waiter
		} else {
			require.Contains(t, []natsclient.DeliveryDecision{natsclient.DeliveryDecisionTerminate, natsclient.DeliveryDecisionQuarantine, natsclient.DeliveryDecisionRetry}, directDecision)
			require.Empty(t, waiter)
		}
		after, err := json.Marshal(props)
		require.NoError(t, err)
		require.Equal(t, before, after)
		// Standard JSON round-trip supplies an equivalent registered representation,
		// independently of the production map projector/normalizer.
		plain, err := json.Marshal(direct)
		require.NoError(t, err)
		var fields map[string]any
		require.NoError(t, json.Unmarshal(plain, &fields))
		wire := settlementEnvelope(t, message.NewGenericJSON(fields))
		normalizedDirect, directStatus, directValidation := normalizeVerdictPayload(direct)
		normalizedWire, wireStatus, wireValidation := decodeVerdictPayload(decoder, wire)
		require.Equal(t, directStatus, wireStatus)
		require.Equal(t, directValidation == nil, wireValidation == nil)
		if directValidation == nil {
			// Properties itself may encode an empty map as omission. Compare the
			// normalized decision/correlation/context, not that container spelling.
			normalizedDirect.Properties, normalizedWire.Properties = nil, nil
			require.Equal(t, normalizedDirect, normalizedWire, "wire projection changed normalized correlation/context")
			// The clause, not matchVerdictProposal, defines this fixed proposal oracle.
			want := natsclient.DeliveryDecisionAck
			if normalizedDirect.ExecutionID != "exec-1" {
				want = natsclient.DeliveryDecisionRetry
			} else if normalizedDirect.LoopID != "loop-1" || normalizedDirect.RequestID != "request-1" ||
				normalizedDirect.ProposalFingerprint != "fingerprint" ||
				(normalizedDirect.CallID != "" && normalizedDirect.CallID != "call-1") {
				want = natsclient.DeliveryDecisionQuarantine
			}
			require.Equal(t, want, directDecision, "proposal-conflicting identity must not enter the fixed waiter")
		}
		c := &Component{decoder: decoder, handler: &MessageHandler{governanceDispatcher: d}}
		wireDecision, wireErr := c.handleToolCallVerdictMessage(t.Context(), subject, wire)
		if directValidation != nil {
			require.Error(t, wireErr)
			require.Equal(t, directDecision, wireDecision)
			require.Empty(t, waiter)
		} else if subject != "agent.toolcall."+normalizedDirect.Decision+"."+normalizedDirect.ExecutionID {
			require.Error(t, wireErr)
			require.Equal(t, natsclient.DeliveryDecisionQuarantine, wireDecision)
			require.Empty(t, waiter)
		} else if directErr != nil {
			require.Error(t, wireErr)
			require.Equal(t, directDecision, wireDecision)
			require.Empty(t, waiter)
		} else {
			require.NoError(t, wireErr)
			require.Equal(t, natsclient.DeliveryDecisionAck, wireDecision)
			require.Len(t, waiter, 1)
			require.Equal(t, expected, <-waiter)
		}
	})
}

func validVerdictFields() map[string]any {
	return map[string]any{"decision": "rejected", "execution_id": "exec-1", "loop_id": "loop-1",
		"request_id": "request-1", "proposal_fingerprint": "fingerprint", "reason": "policy", "rule_id": "rule"}
}

// spec: agentic-governance / Governance verdicts use one registered wire and typed handoff
func TestGovernanceVerdictRegisteredContextSurvives(t *testing.T) {
	for _, nested := range []bool{false, true} {
		name := "approve-shape"
		if nested {
			name = "publish-shape"
		}
		t.Run(name, func(t *testing.T) {
			c, d := verdictWireComponent(t)
			fields := validVerdictFields()
			if nested {
				fields = map[string]any{"properties": fields}
			}
			waiter := d.registerWaiter(governanceTestProposal("exec-1")).arrivals
			decision, err := c.handleToolCallVerdictMessage(t.Context(), "agent.toolcall.rejected.exec-1", settlementEnvelope(t, message.NewGenericJSON(fields)))
			require.NoError(t, err)
			require.Equal(t, natsclient.DeliveryDecisionAck, decision)
			require.Len(t, waiter, 1)
			require.Equal(t, verdictArrival{decision: "rejected", reason: "policy", ruleID: "rule"}, <-waiter)
		})
	}
}

// spec: agentic-governance / Governance verdicts use one registered wire and typed handoff
func TestGovernanceVerdictRawInputRefused(t *testing.T) {
	c, d := verdictWireComponent(t)
	waiter := d.registerWaiter(governanceTestProposal("exec-1")).arrivals
	data, err := json.Marshal(validVerdictFields())
	require.NoError(t, err)
	decision, err := c.handleToolCallVerdictMessage(t.Context(), "agent.toolcall.rejected.exec-1", data)
	require.Error(t, err)
	require.Equal(t, natsclient.DeliveryDecisionTerminate, decision)
	require.Empty(t, waiter)
}
