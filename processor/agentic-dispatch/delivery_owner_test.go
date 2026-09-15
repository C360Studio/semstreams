package agenticdispatch

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/stretchr/testify/require"
)

func TestTerminalDeliveryFatalBuffersBeforeHandleAndDrainsExactHandleOnce(t *testing.T) {
	result := natsclient.ConsumeDeliveryWithHeartbeat(t.Context(), nil, natsclient.HeartbeatDeliveryPolicy{})
	require.True(t, result.OwnerStopRequired())
	observed := make(chan error, 1)
	c := &Component{
		started:                true,
		logger:                 slog.New(slog.NewTextHandler(io.Discard, nil)),
		terminalDeliveryDoneFn: func(err error) { observed <- err },
	}
	admission := newDeliveryLaneAdmission(c.recordAgentCompleteFatal)
	admission.latch(result)
	require.Len(t, admission.fatal, 1)
	health := c.Health()
	require.False(t, health.Healthy)
	require.Contains(t, health.LastError, "agent.complete")

	handle := &causalConsumeHandle{closed: make(chan struct{}), closedCalls: make(chan struct{}, 1)}
	binding := newStreamConsumerBinding(handle)
	ctx, cancel := context.WithCancel(t.Context())
	c.observeDeliveryLane(ctx, &binding, admission)
	select {
	case err := <-observed:
		require.Error(t, err)
	case <-time.After(time.Second):
		t.Fatal("fatal result was not observed")
	}
	require.Eventually(t, func() bool { return handle.drains.Load() == 1 }, time.Second, time.Millisecond)
	binding.drain()
	require.Equal(t, int32(1), handle.drains.Load())
	require.False(t, admission.admit())
	cancel()
	<-binding.observerDone
}

func TestTerminalLaneFatalHealthFailsClosedIndependently(t *testing.T) {
	result := natsclient.ConsumeDeliveryWithHeartbeat(t.Context(), nil, natsclient.HeartbeatDeliveryPolicy{})
	tests := []struct {
		name   string
		lane   string
		record func(*Component, natsclient.DeliveryResult)
	}{
		{name: "complete", lane: "agent.complete", record: (*Component).recordAgentCompleteFatal},
		{name: "failed", lane: "agent.failed", record: (*Component).recordAgentFailedFatal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Component{started: true}
			admission := newDeliveryLaneAdmission(func(result natsclient.DeliveryResult) { tt.record(c, result) })
			admission.latch(result)
			health := c.Health()
			require.False(t, health.Healthy)
			require.Equal(t, "terminal delivery ownership lost", health.Status)
			require.Equal(t, 1, health.ErrorCount)
			require.Contains(t, health.LastError, tt.lane)
		})
	}
}
