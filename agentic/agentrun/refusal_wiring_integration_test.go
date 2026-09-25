//go:build integration

package agentrun_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/agentic/agentrun"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	semerrs "github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/pkg/lifecycle"
)

// TestIntegration_MilestoneLanesDeclareBufferedRefusal is the wiring test for
// the milestone lanes' refusal declarer (#1342, obligation inherited from
// #1341). Every other refusal test builds its lane by hand, so passing nil as
// onRefused where Start constructs each lane's admission survives them all.
//
// Per lane, Start binds the production callback over real NATS; the first
// milestone's run resolution fails fatally (Quarantine, owner stop), but only
// once the server has already sent that lane a second milestone. The drained
// handle flushes the second into the closed lane, and only the declarer Start
// wired can move the refusal counter under that lane's label — read back
// through the registry a /metrics scrape gathers.
//
// spec: jetstream-consumer-policy / control loss shuts down through the existing exact owner
func TestIntegration_MilestoneLanesDeclareBufferedRefusal(t *testing.T) {
	at := time.Now().UTC()
	rows := []struct {
		lane     string
		consumer string
		subject  string
		payload  message.Payload
	}{
		{
			lane: "complete", consumer: "agentrun-milestone-complete-refusal", subject: "agent.complete.refusal",
			payload: &agentic.LoopCompletedEvent{
				LoopID: "refusal-complete", TaskID: "task-complete", Outcome: agentic.OutcomeSuccess, CompletedAt: at,
				RunEntityID: agentic.ChainExecutionEntityID("acme", "ops", "refusal-complete"),
			},
		},
		{
			lane: "failed", consumer: "agentrun-milestone-failed-refusal", subject: "agent.failed.refusal",
			payload: &agentic.LoopFailedEvent{
				LoopID: "refusal-failed", TaskID: "task-failed", Outcome: agentic.OutcomeFailed, FailedAt: at,
				RunEntityID: agentic.ChainExecutionEntityID("acme", "ops", "refusal-failed"),
			},
		},
	}
	for _, row := range rows {
		t.Run(row.lane, func(t *testing.T) {
			ctx := t.Context()
			tc := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithStreams(
				natsclient.TestStreamConfig{Name: agentrun.AgentStreamName, Subjects: []string{"agent.>"}},
			))
			reader := &heldFatalRunReader{client: tc.Client, consumer: row.consumer, want: 2}
			sub := agentrun.NewMilestoneSubscriberWithRunStateReader(reader, nil, "acme", "ops", nil)
			registry := metric.NewMetricsRegistry()
			require.NoError(t, sub.RegisterMetrics(registry))
			stop, err := sub.Start(ctx, tc.Client, agentrun.StartConfig{
				StreamName: agentrun.AgentStreamName, ConsumerNameSuffix: "refusal",
			})
			require.NoError(t, err)
			defer func() { require.NoError(t, stop(context.Background())) }()

			data, err := json.Marshal(message.NewBaseMessage(row.payload.Schema(), row.payload, "agentic-loop"))
			require.NoError(t, err)
			for range 2 {
				require.NoError(t, tc.Client.PublishToStream(ctx, row.subject, data))
			}

			require.Eventually(t, func() bool { return sub.DeliveryFatal() != nil }, 10*time.Second, 10*time.Millisecond,
				"the first milestone must latch the %s lane", row.lane)
			require.Eventually(t, func() bool { return refusalSeries(t, registry)[row.lane] == 1 },
				10*time.Second, 10*time.Millisecond,
				"the buffered milestone the drain flushed must reach the %s lane's own refusal declarer", row.lane)
			require.Len(t, refusalSeries(t, registry), 1, "only the latched lane refused")

			stream, err := tc.Client.GetStream(ctx, agentrun.AgentStreamName)
			require.NoError(t, err)
			consumer, err := stream.Consumer(ctx, row.consumer)
			require.NoError(t, err)
			info, err := consumer.Info(ctx)
			require.NoError(t, err)
			require.Equal(t, uint64(2), info.Delivered.Consumer)
			require.Zero(t, info.AckFloor.Consumer)
			require.Equal(t, 2, info.NumAckPending)
		})
	}
}

// refusalSeries reads the refusal counter back out of the registry, by lane.
func refusalSeries(t *testing.T, registry *metric.MetricsRegistry) map[string]float64 {
	t.Helper()
	families, err := registry.PrometheusRegistry().Gather()
	require.NoError(t, err)
	series := map[string]float64{}
	for _, family := range families {
		if family.GetName() != "semstreams_agentrun_delivery_refusals_total" {
			continue
		}
		for _, m := range family.GetMetric() {
			for _, label := range m.GetLabel() {
				if label.GetName() == "lane" {
					series[label.GetValue()] = m.GetCounter().GetValue()
				}
			}
		}
	}
	return series
}

// heldFatalRunReader answers every run resolution with a fatal error, which
// the milestone lanes settle as Quarantine and owner stop. The first answer is
// held until the server reports want deliveries to the lane's consumer, so the
// second milestone is already on its way to the client when the first latches
// the lane: that makes the flush of a buffered delivery into a drained lane
// deterministic rather than a race with the drain. A failed wait only releases
// the hold; the test's counter assertion is what fails.
type heldFatalRunReader struct {
	client   *natsclient.Client
	consumer string
	want     uint64
	once     sync.Once
}

func (r *heldFatalRunReader) Get(ctx context.Context, _, _ string) (lifecycle.Participant, error) {
	r.once.Do(func() { r.awaitDelivered(ctx) })
	return nil, semerrs.WrapFatal(errors.New("injected: run authority unavailable"),
		"heldFatalRunReader", "Get", "resolve run")
}

func (r *heldFatalRunReader) awaitDelivered(ctx context.Context) {
	stream, err := r.client.GetStream(ctx, agentrun.AgentStreamName)
	if err != nil {
		return
	}
	consumer, err := stream.Consumer(ctx, r.consumer)
	if err != nil {
		return
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		info, infoErr := consumer.Info(ctx)
		if infoErr == nil && info.Delivered.Consumer >= r.want {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
