//go:build integration

package agenticloop_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	agenticloop "github.com/c360studio/semstreams/processor/agentic-loop"
)

// inflightTestComponent starts a loop bound to taskSubject under the given
// deployment suffix, and returns it plus the subject that addresses it.
func inflightTestComponent(ctx context.Context, t *testing.T, natsClient *natsclient.Client,
	taskSubject, suffix string) (component.LifecycleComponent, string) {
	t.Helper()

	config := agenticloop.Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{Name: "agent.task", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{taskSubject}}, Required: true},
			},
			Outputs: []component.PortDefinition{
				{Name: "agent.request", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"agent.request.*"}}},
				{Name: "agent.complete", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"agent.complete.*"}}},
			},
		},
		MaxIterations:      10,
		Timeout:            "60s",
		StreamName:         "AGENT",
		ConsumerNameSuffix: suffix,
		LoopsBucket:        "AGENT_LOOPS",
	}
	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)

	comp, err := agenticloop.NewComponent(rawConfig, component.Dependencies{
		NATSClient:      natsClient,
		PayloadRegistry: payloadbuiltins.NewTestRegistry(t),
	})
	require.NoError(t, err)

	lc := comp.(component.LifecycleComponent)
	require.NoError(t, lc.Initialize())
	require.NoError(t, lc.Start(ctx))

	subject := agenticloop.InFlightQuerySubjectFor(suffix)
	require.Eventually(t, func() bool {
		req, _ := json.Marshal(agenticloop.InFlightRequest{Subject: taskSubject})
		_, qErr := natsClient.RequestClassified(ctx, subject, req, 2*time.Second)
		return qErr == nil
	}, 10*time.Second, 100*time.Millisecond, "in-flight handler for %q should come up", suffix)

	return lc, subject
}

func queryInFlight(ctx context.Context, t *testing.T, natsClient *natsclient.Client,
	querySubject, taskSubject string) (agenticloop.InFlightResponse, error) {
	t.Helper()
	req, err := json.Marshal(agenticloop.InFlightRequest{Subject: taskSubject})
	require.NoError(t, err)

	raw, err := natsClient.RequestClassified(ctx, querySubject, req, 5*time.Second)
	if err != nil {
		return agenticloop.InFlightResponse{}, err
	}
	var resp agenticloop.InFlightResponse
	require.NoError(t, json.Unmarshal(raw, &resp))
	return resp, nil
}

// publishTaskBurst publishes n tasks so the consumer has genuinely pending work.
//
// A burst is used rather than one task held unacked, because MEASUREMENT showed the
// original premise was wrong: gh#733 assumed a task "lives unacked while the model
// works", but handleTaskMessage returns nil on a failed HandleTask and HandleTask
// does not block on the model reply, so a single task is acked promptly. Depth, not
// duration, is what makes outstanding work observable here.
func publishTaskBurst(t *testing.T, natsClient *natsclient.Client, subject, loopPrefix string, n int) {
	t.Helper()
	for i := range n {
		publishTaskMessage(t, natsClient, subject, &agentic.TaskMessage{
			LoopID: fmt.Sprintf("%s_%d", loopPrefix, i),
			TaskID: fmt.Sprintf("%s_task_%d", loopPrefix, i),
			Role:   "general",
			Model:  "test-model",
			Prompt: "burst work",
		})
	}
}

// TestIntegration_InFlight_OutstandingWork closes the verification gap Codex found:
// the previous version published NO task and only asserted
// `InFlight == (Outstanding > 0)`, which a handler hardcoded to zero satisfies.
//
// It now drives a real ack cycle: authoritative zero when idle, a nonzero count
// under a real backlog, and a return to zero once the consumer drains. The nonzero
// leg is what a hardcoded-zero implementation cannot pass.
//
// NOT asserted: a count sustained across a heartbeat renewal. That was in the plan,
// but this path acks promptly (measured — see publishTaskBurst), so there is no
// long-lived unacked task to observe. Asserting it would have meant inventing a
// scenario the code does not have.
func TestIntegration_InFlight_OutstandingWork(t *testing.T) {
	natsClient := getSharedNATSClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	const taskSubject = "agent.task.inflight_work"
	lc, querySubject := inflightTestComponent(ctx, t, natsClient, taskSubject, "inflight-work-test")
	stopped := false
	defer func() {
		if !stopped {
			lc.Stop(5 * time.Second)
		}
	}()

	// Idle: a bound consumer with nothing pending answers ZERO — authoritatively,
	// not as a fallback. This is the reading the unknown cases must never be
	// confused with.
	resp, err := queryInFlight(ctx, t, natsClient, querySubject, taskSubject)
	require.NoError(t, err)
	require.Equal(t, uint64(0), resp.Outstanding, "no task published yet")
	require.False(t, resp.InFlight)

	publishTaskBurst(t, natsClient, taskSubject, "loop_inflight_work", 300)

	// Nonzero under a real backlog. THIS is the leg the old test lacked: a handler
	// hardcoded to zero passes everything else in this file and fails here.
	var peak uint64
	require.Eventually(t, func() bool {
		r, qErr := queryInFlight(ctx, t, natsClient, querySubject, taskSubject)
		if qErr == nil && r.Outstanding > 0 && r.InFlight {
			peak = r.Outstanding
			return true
		}
		return false
	}, 30*time.Second, 50*time.Millisecond,
		"a published backlog must show as outstanding work; if this never goes nonzero "+
			"the query is not measuring the consumer at all")
	t.Logf("observed outstanding work: %d", peak)

	// ...and returns to zero once the consumer drains and acks. This is the
	// release/ack leg: the count tracks real consumer state in BOTH directions,
	// so neither reading is a constant.
	require.Eventually(t, func() bool {
		r, qErr := queryInFlight(ctx, t, natsClient, querySubject, taskSubject)
		return qErr == nil && r.Outstanding == 0 && !r.InFlight
	}, 60*time.Second, 250*time.Millisecond,
		"outstanding work must fall back to zero once the backlog is acked")

	// Stopped WITH work freshly published: the caller must see unknown, never zero.
	// This is the most expensive wrong answer in the whole API — the work is on the
	// stream, and "nothing in flight" would strand it.
	publishTaskBurst(t, natsClient, taskSubject, "loop_inflight_pending", 300)
	require.NoError(t, lc.Stop(5*time.Second))
	stopped = true

	var lastErr error
	require.Eventually(t, func() bool {
		_, lastErr = queryInFlight(ctx, t, natsClient, querySubject, taskSubject)
		return lastErr != nil && natsclient.IsNoResponders(lastErr)
	}, 10*time.Second, 100*time.Millisecond,
		"a stopped loop must surface as no-responders (UNKNOWN), never as zero, while "+
			"work may still sit on the stream. last error: %v", lastErr)
}

// TestIntegration_InFlight_DeploymentsAreAddressedSeparately closes Codex finding
// 1. SubscribeForRequests uses a plain conn.Subscribe, so before the subject
// carried deployment identity BOTH loops received every request and the requester
// kept whichever reply landed first — an arbitrary deployment's answer, delivered
// with full confidence.
//
// The decisive assertion is the last one: asking the IDLE deployment about the BUSY
// deployment's subject must be UNKNOWN. On a shared subject the busy component
// would answer with a real number, so that case cannot pass by luck or timing.
func TestIntegration_InFlight_DeploymentsAreAddressedSeparately(t *testing.T) {
	natsClient := getSharedNATSClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	const busySubject = "agent.task.deploy_busy"
	const idleSubject = "agent.task.deploy_idle"

	busy, busyQuery := inflightTestComponent(ctx, t, natsClient, busySubject, "deploy-busy")
	defer busy.Stop(5 * time.Second)
	idle, idleQuery := inflightTestComponent(ctx, t, natsClient, idleSubject, "deploy-idle")
	defer idle.Stop(5 * time.Second)

	require.NotEqual(t, busyQuery, idleQuery,
		"distinct deployments must occupy distinct subjects, or the requester cannot "+
			"address one of them")

	// Each deployment answers about its OWN subject, and never about the other's.
	// A shared subject makes every one of these rounds a coin flip.
	for i := range 5 {
		_, err := queryInFlight(ctx, t, natsClient, busyQuery, busySubject)
		require.NoError(t, err, "round %d: busy deployment must answer about its own subject", i)

		idleResp, err := queryInFlight(ctx, t, natsClient, idleQuery, idleSubject)
		require.NoError(t, err, "round %d", i)
		assert.Equal(t, uint64(0), idleResp.Outstanding,
			"round %d: idle deployment must report its OWN zero", i)

		// THE decisive one: the idle deployment binds no consumer for the busy
		// subject, so the honest answer is UNKNOWN. If both components were still
		// sharing one subject, the busy one would answer this with a number.
		_, err = queryInFlight(ctx, t, natsClient, idleQuery, busySubject)
		require.Error(t, err, "round %d: a deployment must not answer for a subject it does not bind", i)
		assert.True(t, errors.Is(err, agenticloop.ErrInFlightUnknownNoConsumer),
			"round %d: expected the no-consumer sentinel, got %v", i, err)
	}

	// And with a real backlog on the busy deployment, the idle one still reports its
	// own zero rather than the neighbour's count.
	publishTaskBurst(t, natsClient, busySubject, "loop_deploy_busy", 300)
	require.Eventually(t, func() bool {
		r, qErr := queryInFlight(ctx, t, natsClient, busyQuery, busySubject)
		if qErr != nil || r.Outstanding == 0 {
			return false
		}
		idleResp, idleErr := queryInFlight(ctx, t, natsClient, idleQuery, idleSubject)
		require.NoError(t, idleErr)
		assert.Equal(t, uint64(0), idleResp.Outstanding,
			"the idle deployment must report its own zero while its neighbour is busy")
		return true
	}, 30*time.Second, 50*time.Millisecond, "busy deployment should report outstanding work")
}

// TestIntegration_InFlightQuery covers the wire contract itself: a bound subject
// answers, and an unbound subject is unknown with the sentinel surviving
// ClassifyReply — which is what lets an OUT-OF-PROCESS caller branch on identity
// rather than message text.
func TestIntegration_InFlightQuery(t *testing.T) {
	natsClient := getSharedNATSClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	const taskSubject = "agent.task.wire_contract"
	lc, querySubject := inflightTestComponent(ctx, t, natsClient, taskSubject, "wire-contract-test")
	defer lc.Stop(5 * time.Second)

	t.Run("bound subject returns a known, decodable answer", func(t *testing.T) {
		resp, err := queryInFlight(ctx, t, natsClient, querySubject, taskSubject)
		require.NoError(t, err, "a bound consumer must produce a KNOWN answer")
		assert.Equal(t, taskSubject, resp.Subject)
		assert.Equal(t, resp.Outstanding > 0, resp.InFlight,
			"InFlight must agree with Outstanding; a caller relies on not re-deriving it")
	})

	t.Run("unbound subject is unknown, sentinel round-trips the wire", func(t *testing.T) {
		_, err := queryInFlight(ctx, t, natsClient, querySubject, "agent.never.bound.here")
		require.Error(t, err, "an unbound subject must NOT answer zero outstanding work")
		assert.True(t, errors.Is(err, agenticloop.ErrInFlightUnknownNoConsumer),
			"the sentinel must survive ClassifyReply so an out-of-process caller branches on "+
				"identity rather than message text; got %v", err)
	})
}
