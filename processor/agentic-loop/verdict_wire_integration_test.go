//go:build integration

package agenticloop

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/processor/rule"
	"github.com/c360studio/semstreams/types"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

type verdictWireNativePublisher struct{ client *natsclient.Client }

func (p verdictWireNativePublisher) Publish(ctx context.Context, subject string, data []byte) error {
	return p.client.PublishToStream(ctx, subject, data)
}

// spec: agentic-governance / Governance publications are durably at-least-once
// spec: agentic-governance / Governance verdicts use one registered wire and typed handoff
func TestIntegrationGovernanceLiveProposalMatch(t *testing.T) {
	for _, row := range []struct {
		name     string
		conflict bool
	}{
		{name: "matched"},
		{name: "wrong-fingerprint", conflict: true},
	} {
		t.Run(row.name, func(t *testing.T) {
			// A quarantined owner is isolated from the matching controls.
			tc := natsclient.NewTestClient(t, natsclient.WithStreams(
				natsclient.TestStreamConfig{Name: "AGENT", Subjects: []string{"agent.>", "tool.>"}},
			))
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			c := newTerminalMarkerProcess(t, tc.Client, "live-proposal-"+row.name)
			published := make(chan []byte, 1)
			releasePublisher := make(chan struct{}, 1)
			publisher := proposalTestPublisher(func(ctx context.Context, subject string, data []byte) error {
				if err := tc.Client.PublishToStream(ctx, subject, data); err != nil {
					return err
				}
				published <- data
				// Keep Propose in publication while the real callback observes the
				// buffered arrival, so source ACK can inspect it without a race.
				select {
				case <-releasePublisher:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			})
			dispatcher := NewGovernanceDispatcher(ToolCallGovernanceConfig{Mode: ToolCallGovernanceModeEnforce, Timeout: "5s"},
				publisher, c.logger, nil).(*enforceDispatcher)
			c.handler.SetGovernanceDispatcher(dispatcher)
			call := governanceTestCall("native-call", "lookup")
			const port = "agent.toolcall.approved"
			settled := make(chan *approvalReplacementDelivery, 1)
			handles := make(map[string]*fastLaneObservedConsumer)
			c.consumeStream = func(setupCtx, ownerCtx context.Context, owner natsclient.PortConsumerContext, cfg natsclient.StreamConsumerConfig,
				handler func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
				handle, err := tc.Client.ConsumeStreamWithConfigContexts(setupCtx, ownerCtx, owner, cfg, func(msgCtx context.Context, msg jetstream.Msg) {
					if owner.Port != port {
						handler(msgCtx, msg)
						return
					}
					observed := &approvalReplacementDelivery{terminalMarkerDelivery: &terminalMarkerDelivery{Msg: msg, beforeAck: func() error {
						waiter, ok := dispatcher.lookupWaiter(call.ExecutionID)
						if !ok || len(waiter.arrivals) != 1 {
							return fmt.Errorf("native source ACK preceded delivery to its Propose-created waiter")
						}
						return nil
					}}}
					handler(msgCtx, observed)
					settled <- observed
				})
				if err != nil {
					return nil, err
				}
				observed := &fastLaneObservedConsumer{ConsumeContext: handle}
				handles[owner.Port] = observed
				return observed, nil
			}
			require.NoError(t, c.Start(ctx))
			proposeCtx, cancelPropose := context.WithCancel(ctx)
			finished := make(chan struct{})
			var result DispatcherResult
			var proposeErr error
			go func() {
				defer close(finished)
				result, proposeErr = dispatcher.Propose(proposeCtx, "loop-1", "parent", []agentic.ToolCall{call})
			}()
			defer func() {
				cancelPropose()
				<-finished
			}()
			var proposal ProposedToolCallPayload
			select {
			case data := <-published:
				proposal = publishedGovernanceProposal(t, data)
			case <-ctx.Done():
				t.Fatal("Propose did not publish its registered proposal")
			}
			verdict := verdictForPublishedProposal(proposal, "approved")
			verdict.Reason = "native policy"
			if row.conflict {
				verdict.ProposalFingerprint = "wrong-originating-fingerprint"
			}
			require.NoError(t, tc.Client.PublishToStream(ctx, port+"."+proposal.ExecutionID, registeredProposalVerdict(t, verdict, true)))
			var observed *approvalReplacementDelivery
			select {
			case observed = <-settled:
			case <-ctx.Done():
				t.Fatal("native verdict callback did not return")
			}
			waiter, ok := dispatcher.lookupWaiter(proposal.ExecutionID)
			require.True(t, ok, "publication hold keeps the real waiter alive")
			if row.conflict {
				require.Zero(t, observed.acks+observed.naks+observed.terms)
				require.Empty(t, waiter.arrivals)
				require.False(t, c.Health().Healthy)
				select {
				case <-handles[port].Closed():
				case <-ctx.Done():
					t.Fatal("conflicting verdict did not close its exact owner")
				}
				for ownerPort, handle := range handles {
					want := int32(0)
					if ownerPort == port {
						want = 1
					}
					require.Equal(t, want, handle.drains.Load(), "wrong owner drain for %s", ownerPort)
				}
				return
			}
			require.NoError(t, observed.ackCheckErr)
			require.Equal(t, 1, observed.acks)
			require.Zero(t, observed.naks+observed.terms)
			require.Len(t, waiter.arrivals, 1)
			for ownerPort, handle := range handles {
				require.Zero(t, handle.drains.Load(), "matching verdict drained %s", ownerPort)
			}
			releasePublisher <- struct{}{}
			select {
			case <-finished:
			case <-ctx.Done():
				t.Fatal("matching verdict did not complete Propose")
			}
			require.NoError(t, proposeErr)
			require.Equal(t, []agentic.ToolCall{call}, result.Approved)
			require.Empty(t, result.Rejected)
		})
	}
}

// spec: agentic-governance / Governance verdicts use one registered wire and typed handoff
// One isolated NATS container proves real rule-action bytes reach both installed
// verdict callbacks and the observed subject is checked before the native source
// ACK. The publisher adapter only supplies transport; R8 separately proves its
// production actionPublisher classifier and PubAck refusal.
func TestIntegrationGovernanceVerdictWireCallbacks(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithStreams(
		natsclient.TestStreamConfig{Name: "AGENT", Subjects: []string{"agent.>", "tool.>"}},
	))
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	c := newTerminalMarkerProcess(t, tc.Client, "verdict-wire")
	dispatcher := &enforceDispatcher{logger: c.logger, waiters: make(map[string]verdictWaiter)}
	c.handler.SetGovernanceDispatcher(dispatcher)
	rows := []struct{ name, action, decision string }{
		{"approve", rule.ActionTypeApprove, "approved"},
		{"publish-approved", rule.ActionTypePublish, "approved"},
		{"publish-rejected", rule.ActionTypePublish, "rejected"},
	}
	waiters := make(map[string]chan verdictArrival)
	expected := make(map[string]verdictArrival)
	for _, row := range rows {
		subject := "agent.toolcall." + row.decision + "." + row.name
		waiters[subject] = dispatcher.registerWaiter(governanceTestProposal(row.name)).arrivals
		expected[subject] = verdictArrival{decision: row.decision, reason: "policy", ruleID: "rule"}
	}
	conflictingWaiter := dispatcher.registerWaiter(governanceTestProposal("conflict")).arrivals
	settled := make(chan *approvalReplacementDelivery, 4)
	c.consumeStream = func(setupCtx, ownerCtx context.Context, owner natsclient.PortConsumerContext, cfg natsclient.StreamConsumerConfig,
		handler func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
		return tc.Client.ConsumeStreamWithConfigContexts(setupCtx, ownerCtx, owner, cfg, func(msgCtx context.Context, msg jetstream.Msg) {
			if owner.Port != "agent.toolcall.approved" && owner.Port != "agent.toolcall.rejected" {
				handler(msgCtx, msg)
				return
			}
			observed := &approvalReplacementDelivery{terminalMarkerDelivery: &terminalMarkerDelivery{Msg: msg, beforeAck: func() error {
				select {
				case actual := <-waiters[msg.Subject()]:
					if actual != expected[msg.Subject()] {
						return fmt.Errorf("verdict before ACK = %+v, expected %+v", actual, expected[msg.Subject()])
					}
					return nil
				default:
					return fmt.Errorf("source ACK preceded its matching verdict delivery on %s", msg.Subject())
				}
			}}}
			handler(msgCtx, observed)
			settled <- observed
		})
	}
	require.NoError(t, c.Start(ctx))
	executor := rule.NewActionExecutorFull(c.logger, nil, verdictWireNativePublisher{tc.Client}, types.PlatformMeta{})
	stream, err := tc.Client.GetStream(ctx, "AGENT")
	require.NoError(t, err)
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			subject := "agent.toolcall." + row.decision + "." + row.name
			fields := validVerdictFields()
			fields["decision"], fields["execution_id"] = row.decision, row.name
			ec := &rule.ExecutionContext{EntityID: "acme.ops.test.svc.entity.001", RelatedID: "related", MessageData: fields, State: &rule.MatchState{RuleID: "rule"}}
			action := rule.Action{Type: row.action, Subject: "agent.toolcall." + row.decision + ".$message.execution_id", Reason: "policy", Properties: fields}
			require.NoError(t, executor.Execute(ctx, action, ec))
			stored, err := stream.GetLastMsgForSubject(ctx, subject)
			require.NoError(t, err)
			decoded, err := c.decoder.Decode(stored.Data)
			require.NoError(t, err)
			require.NoError(t, decoded.Validate())
			payload := decoded.Payload().(*message.GenericJSONPayload).Data
			if row.action == rule.ActionTypePublish {
				require.Equal(t, "related", payload["related_id"])
				require.Equal(t, subject, payload["subject"])
				require.Equal(t, fields, payload["properties"])
			}
			select {
			case observed := <-settled:
				require.Equal(t, subject, observed.Subject())
				require.NoError(t, observed.ackCheckErr)
				require.Equal(t, 1, observed.acks)
				require.Zero(t, observed.naks+observed.terms)
			case <-ctx.Done():
				t.Fatalf("installed callback did not settle %s: %v", subject, ctx.Err())
			}
		})
	}
	// Valid payload with a mismatching actual subject must not reach its waiter,
	// even if wrapper metadata names the otherwise-correct rejection subject.
	fields := validVerdictFields()
	fields["execution_id"], fields["subject"] = "conflict", "agent.toolcall.rejected.conflict"
	require.NoError(t, tc.Client.PublishToStream(ctx, "agent.toolcall.approved.conflict", settlementEnvelope(t, message.NewGenericJSON(fields))))
	select {
	case observed := <-settled:
		require.Zero(t, observed.acks+observed.naks+observed.terms)
		require.Empty(t, conflictingWaiter)
		require.False(t, c.Health().Healthy, "subject conflict must quarantine its delivery owner")
	case <-ctx.Done():
		t.Fatalf("subject conflict did not return from installed callback: %v", ctx.Err())
	}
}
