//go:build integration

package agenticloop_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	agenticloop "github.com/c360studio/semstreams/processor/agentic-loop"
)

// TestIntegration_InFlightQuery drives the PRODUCTION wire for gh#733: a real
// request over NATS, to a real component, holding a real JetStream consumer.
//
// The wire IS the contract here — §1 Q4 chose request/reply precisely so the
// consumer name never crosses a boundary — so a mock returning a canned count
// would test nothing that matters. The three assertions are the three contract
// points:
//
//  1. a bound subject answers, and the answer decodes;
//  2. an UNBOUND subject is unknown, and the sentinel survives the wire;
//  3. a STOPPED component is unknown via no-responders, not zero.
//
// (3) is the failure mode the wire shape introduced, and the one where a wrong
// answer costs the most: messages may be sitting on the stream with nobody to
// answer for them, which is exactly when a recovery pass is running.
func TestIntegration_InFlightQuery(t *testing.T) {
	natsClient := getSharedNATSClient(t)

	const taskSubject = "agent.task.*"

	config := agenticloop.Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{Name: "agent.task", Type: "jetstream", Subject: taskSubject, StreamName: "AGENT", Required: true},
			},
			Outputs: []component.PortDefinition{
				{Name: "agent.request", Type: "jetstream", Subject: "agent.request.*", StreamName: "AGENT"},
				{Name: "agent.complete", Type: "jetstream", Subject: "agent.complete.*", StreamName: "AGENT"},
			},
		},
		MaxIterations:      10,
		Timeout:            "60s",
		StreamName:         "AGENT",
		ConsumerNameSuffix: "inflight-query-test",
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

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	require.NoError(t, lc.Start(ctx))
	stopped := false
	defer func() {
		if !stopped {
			lc.Stop(5 * time.Second)
		}
	}()

	// Let the subscription and consumer bind.
	require.Eventually(t, func() bool {
		req, _ := json.Marshal(agenticloop.InFlightRequest{Subject: taskSubject})
		_, qErr := natsClient.RequestClassified(ctx, agenticloop.InFlightQuerySubject, req, 2*time.Second)
		return qErr == nil
	}, 10*time.Second, 100*time.Millisecond, "in-flight query handler should come up")

	// (1) A bound subject answers, and the answer decodes.
	t.Run("bound subject returns a known, decodable answer", func(t *testing.T) {
		req, err := json.Marshal(agenticloop.InFlightRequest{Subject: taskSubject})
		require.NoError(t, err)

		raw, err := natsClient.RequestClassified(ctx, agenticloop.InFlightQuerySubject, req, 5*time.Second)
		require.NoError(t, err, "a bound consumer must produce a KNOWN answer")

		var resp agenticloop.InFlightResponse
		require.NoError(t, json.Unmarshal(raw, &resp))
		assert.Equal(t, taskSubject, resp.Subject)
		assert.Equal(t, resp.Outstanding > 0, resp.InFlight,
			"InFlight must agree with Outstanding; a caller relies on not having to re-derive it")
	})

	// (2) An unbound subject is UNKNOWN, and the sentinel survives the wire.
	// This is the defect gh#733 was filed about, asserted through the transport
	// an out-of-process caller actually uses.
	t.Run("unbound subject is unknown, sentinel round-trips the wire", func(t *testing.T) {
		req, err := json.Marshal(agenticloop.InFlightRequest{Subject: "agent.never.bound.here"})
		require.NoError(t, err)

		raw, err := natsClient.RequestClassified(ctx, agenticloop.InFlightQuerySubject, req, 5*time.Second)
		require.Error(t, err, "an unbound subject must NOT answer zero outstanding work")
		assert.Nil(t, raw, "an unknown answer carries no payload to misread")
		assert.True(t, errors.Is(err, agenticloop.ErrInFlightUnknownNoConsumer),
			"the sentinel must survive ClassifyReply so an out-of-process caller branches on "+
				"identity rather than message text; got %v", err)
	})

	// (3) A stopped component is UNKNOWN via no-responders, never zero.
	t.Run("stopped component is no-responders, which is unknown not zero", func(t *testing.T) {
		require.NoError(t, lc.Stop(5*time.Second))
		stopped = true

		req, err := json.Marshal(agenticloop.InFlightRequest{Subject: taskSubject})
		require.NoError(t, err)

		var lastErr error
		require.Eventually(t, func() bool {
			_, lastErr = natsClient.RequestClassified(ctx, agenticloop.InFlightQuerySubject, req, 2*time.Second)
			return lastErr != nil && natsclient.IsNoResponders(lastErr)
		}, 10*time.Second, 100*time.Millisecond,
			"a stopped loop must surface as no-responders — which the caller reads as UNKNOWN. "+
				"Work may still be sitting on the stream with nobody answering for it, so "+
				"concluding 'nothing in flight' here is the most expensive possible wrong answer. "+
				"last error: %v", lastErr)
	})
}
