//go:build integration

package natsclient

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegration_ConsumeStreamWithConfigContexts_CancelledSetupDoesNotStartConsumer(t *testing.T) {
	ctx := context.Background()
	natsContainer, natsURL := startNATSContainerWithJS(ctx, t)
	defer natsContainer.Terminate(ctx)

	client, err := NewClient(natsURL)
	require.NoError(t, err)
	require.NoError(t, client.Connect(ctx))
	defer client.Close(ctx)

	_, err = client.EnsureStream(ctx, jetstream.StreamConfig{
		Name:     "CONTEXT_SPLIT_STREAM",
		Subjects: []string{"context.split.>"},
		Storage:  jetstream.MemoryStorage,
		MaxAge:   testStreamMaxAge,
		MaxBytes: testStreamMaxBytes,
	})
	require.NoError(t, err)

	setupCtx, cancelSetup := context.WithCancel(ctx)
	cancelSetup()
	handlerCtx, cancelHandler := context.WithCancel(ctx)
	defer cancelHandler()

	err = client.ConsumeStreamWithConfigContexts(setupCtx, handlerCtx,
		PortConsumerContext{Component: "integration", Port: "input"}, StreamConsumerConfig{
			StreamName:    "CONTEXT_SPLIT_STREAM",
			ConsumerName:  "cancelled-setup",
			FilterSubject: "context.split.>",
			DeliverPolicy: "all",
			AckPolicy:     "explicit",
		}, func(context.Context, jetstream.Msg) {
			t.Error("handler ran despite cancelled consumer setup")
		})
	require.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled), "setup error = %v, want context cancellation", err)
	select {
	case <-handlerCtx.Done():
		t.Fatal("setup cancellation leaked into independent handler lifecycle")
	default:
	}

	client.consumersMu.Lock()
	_, started := client.consumers["CONTEXT_SPLIT_STREAM:cancelled-setup"]
	client.consumersMu.Unlock()
	assert.False(t, started, "cancelled setup registered a live consumer")
}

// TestIntegration_EnsureStream tests stream creation
func TestIntegration_EnsureStream(t *testing.T) {
	ctx := context.Background()

	// Start NATS container with JetStream
	natsContainer, natsURL := startNATSContainerWithJS(ctx, t)
	defer natsContainer.Terminate(ctx)

	// Create and connect client
	client, err := NewClient(natsURL)
	require.NoError(t, err)
	err = client.Connect(ctx)
	require.NoError(t, err)
	defer client.Close(ctx)

	// Create stream
	cfg := jetstream.StreamConfig{
		Name:     "TEST_STREAM",
		Subjects: []string{"test.>"},
		Storage:  jetstream.MemoryStorage,
		MaxAge:   testStreamMaxAge,
		MaxBytes: testStreamMaxBytes,
	}

	stream, err := client.EnsureStream(ctx, cfg)
	require.NoError(t, err)
	assert.NotNil(t, stream)

	// Verify stream info
	info, err := stream.Info(ctx)
	require.NoError(t, err)
	assert.Equal(t, "TEST_STREAM", info.Config.Name)
	assert.Contains(t, info.Config.Subjects, "test.>")
}

// TestIntegration_EnsureStream_Existing tests that existing stream is returned
func TestIntegration_EnsureStream_Existing(t *testing.T) {
	ctx := context.Background()

	// Start NATS container with JetStream
	natsContainer, natsURL := startNATSContainerWithJS(ctx, t)
	defer natsContainer.Terminate(ctx)

	// Create and connect client
	client, err := NewClient(natsURL)
	require.NoError(t, err)
	err = client.Connect(ctx)
	require.NoError(t, err)
	defer client.Close(ctx)

	cfg := jetstream.StreamConfig{
		Name:     "EXISTING_STREAM",
		Subjects: []string{"existing.>"},
		Storage:  jetstream.MemoryStorage,
		MaxAge:   testStreamMaxAge,
		MaxBytes: testStreamMaxBytes,
	}

	// Create stream first time
	stream1, err := client.EnsureStream(ctx, cfg)
	require.NoError(t, err)

	// Create stream second time - should return existing
	stream2, err := client.EnsureStream(ctx, cfg)
	require.NoError(t, err)

	// Both should reference the same stream
	info1, _ := stream1.Info(ctx)
	info2, _ := stream2.Info(ctx)
	assert.Equal(t, info1.Config.Name, info2.Config.Name)
}

// TestIntegration_EnsureStream_NotConnected tests EnsureStream when not connected
func TestIntegration_EnsureStream_NotConnected(t *testing.T) {
	client, err := NewClient("nats://localhost:4222")
	require.NoError(t, err)

	ctx := context.Background()
	cfg := jetstream.StreamConfig{
		Name:     "TEST_STREAM",
		Subjects: []string{"test.>"},
		MaxAge:   testStreamMaxAge,
		MaxBytes: testStreamMaxBytes,
	}

	_, err = client.EnsureStream(ctx, cfg)
	assert.Equal(t, ErrNotConnected, err)
}

// TestIntegration_ConsumeStreamWithConfig tests basic stream consumption
func TestIntegration_ConsumeStreamWithConfig(t *testing.T) {
	ctx := context.Background()

	// Start NATS container with JetStream
	natsContainer, natsURL := startNATSContainerWithJS(ctx, t)
	defer natsContainer.Terminate(ctx)

	// Create and connect client
	client, err := NewClient(natsURL)
	require.NoError(t, err)
	err = client.Connect(ctx)
	require.NoError(t, err)
	defer client.Close(ctx)

	// Create stream first
	streamCfg := jetstream.StreamConfig{
		Name:     "CONSUME_STREAM",
		Subjects: []string{"consume.>"},
		Storage:  jetstream.MemoryStorage,
		MaxAge:   testStreamMaxAge,
		MaxBytes: testStreamMaxBytes,
	}
	_, err = client.EnsureStream(ctx, streamCfg)
	require.NoError(t, err)

	// Publish some messages
	js, err := client.JetStream()
	require.NoError(t, err)

	for i := 0; i < 5; i++ {
		_, err = js.Publish(ctx, "consume.test", []byte("message"))
		require.NoError(t, err)
	}

	// Set up consumer
	var received atomic.Int32
	var wg sync.WaitGroup
	wg.Add(5)

	cfg := StreamConsumerConfig{
		StreamName:    "CONSUME_STREAM",
		ConsumerName:  "test-consumer",
		FilterSubject: "consume.>",
		DeliverPolicy: "all",
		AckPolicy:     "explicit",
	}

	err = client.ConsumeStreamWithConfig(ctx, PortConsumerContext{Component: "integration", Port: "input"}, cfg, func(_ context.Context, msg jetstream.Msg) {
		received.Add(1)
		msg.Ack()
		wg.Done()
	})
	require.NoError(t, err)

	// Wait for messages to be received
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(5 * time.Second):
		t.Fatalf("Timeout waiting for messages, received %d/5", received.Load())
	}

	assert.Equal(t, int32(5), received.Load())
}

// TestIntegration_ConsumeStreamWithConfig_AutoCreate tests auto-creation of stream
func TestIntegration_ConsumeStreamWithConfig_AutoCreate(t *testing.T) {
	ctx := context.Background()

	// Start NATS container with JetStream
	natsContainer, natsURL := startNATSContainerWithJS(ctx, t)
	defer natsContainer.Terminate(ctx)

	// Create and connect client
	client, err := NewClient(natsURL)
	require.NoError(t, err)
	err = client.Connect(ctx)
	require.NoError(t, err)
	defer client.Close(ctx)

	// Configure consumer with auto-create (stream doesn't exist yet)
	cfg := StreamConsumerConfig{
		StreamName:    "AUTO_STREAM",
		ConsumerName:  "auto-consumer",
		FilterSubject: "auto.test.*",
		DeliverPolicy: "new",
		AckPolicy:     "explicit",
		AutoCreate:    true,
		AutoCreateConfig: &StreamAutoCreateConfig{
			Subjects:  []string{"auto.test.>"},
			Storage:   "memory",
			Retention: "limits",
			// Auto-create is stream PROVISIONING, so it declares bounds like every
			// other creation seam. Without these the seam refuses the creation.
			MaxAge:   testStreamMaxAge,
			MaxBytes: testStreamMaxBytes,
		},
	}

	var received atomic.Int32
	err = client.ConsumeStreamWithConfig(ctx, PortConsumerContext{Component: "integration", Port: "input"}, cfg, func(_ context.Context, msg jetstream.Msg) {
		received.Add(1)
		msg.Ack()
	})
	require.NoError(t, err)

	// Verify stream was created
	js, err := client.JetStream()
	require.NoError(t, err)

	stream, err := js.Stream(ctx, "AUTO_STREAM")
	require.NoError(t, err)
	assert.NotNil(t, stream)

	// Publish message and verify it's received
	_, err = js.Publish(ctx, "auto.test.msg", []byte("test"))
	require.NoError(t, err)

	// Wait for message
	time.Sleep(200 * time.Millisecond)
	assert.GreaterOrEqual(t, received.Load(), int32(1))
}

// TestIntegration_ConsumeStreamWithConfig_DeliverPolicies tests different deliver policies
func TestIntegration_ConsumeStreamWithConfig_DeliverPolicies(t *testing.T) {
	testCases := []struct {
		name          string
		deliverPolicy string
		publishBefore int
		publishAfter  int
		expectedMin   int32
		expectedMax   int32
	}{
		{
			name:          "deliver_all",
			deliverPolicy: "all",
			publishBefore: 3,
			publishAfter:  2,
			expectedMin:   5, // Should receive all
			expectedMax:   5,
		},
		{
			name:          "deliver_new",
			deliverPolicy: "new",
			publishBefore: 3,
			publishAfter:  2,
			expectedMin:   2, // Should receive only new
			expectedMax:   2,
		},
		{
			name:          "deliver_last",
			deliverPolicy: "last",
			publishBefore: 3,
			publishAfter:  2,
			expectedMin:   3, // Last before + 2 after
			expectedMax:   3,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()

			// Start NATS container with JetStream
			natsContainer, natsURL := startNATSContainerWithJS(ctx, t)
			defer natsContainer.Terminate(ctx)

			// Create and connect client
			client, err := NewClient(natsURL)
			require.NoError(t, err)
			err = client.Connect(ctx)
			require.NoError(t, err)
			defer client.Close(ctx)

			streamName := "POLICY_" + tc.name
			subject := "policy." + tc.name + ".test"

			// Create stream
			streamCfg := jetstream.StreamConfig{
				Name:     streamName,
				Subjects: []string{"policy." + tc.name + ".>"},
				Storage:  jetstream.MemoryStorage,
				MaxAge:   testStreamMaxAge,
				MaxBytes: testStreamMaxBytes,
			}
			_, err = client.EnsureStream(ctx, streamCfg)
			require.NoError(t, err)

			// Publish messages before consumer
			js, err := client.JetStream()
			require.NoError(t, err)

			for i := 0; i < tc.publishBefore; i++ {
				_, err = js.Publish(ctx, subject, []byte("before"))
				require.NoError(t, err)
			}

			// Set up consumer
			var received atomic.Int32
			cfg := StreamConsumerConfig{
				StreamName:    streamName,
				ConsumerName:  tc.name + "-consumer",
				FilterSubject: "policy." + tc.name + ".>",
				DeliverPolicy: tc.deliverPolicy,
				AckPolicy:     "explicit",
			}

			err = client.ConsumeStreamWithConfig(ctx, PortConsumerContext{Component: "integration", Port: "input"}, cfg, func(_ context.Context, msg jetstream.Msg) {
				received.Add(1)
				msg.Ack()
			})
			require.NoError(t, err)

			// Small delay for consumer to be ready
			time.Sleep(100 * time.Millisecond)

			// Publish messages after consumer
			for i := 0; i < tc.publishAfter; i++ {
				_, err = js.Publish(ctx, subject, []byte("after"))
				require.NoError(t, err)
			}

			// Wait for processing
			time.Sleep(500 * time.Millisecond)

			count := received.Load()
			assert.GreaterOrEqual(t, count, tc.expectedMin, "received fewer messages than expected")
			assert.LessOrEqual(t, count, tc.expectedMax, "received more messages than expected")
		})
	}
}

// TestIntegration_ConsumeStreamWithConfig_AckPolicies tests different ack policies
func TestIntegration_ConsumeStreamWithConfig_AckPolicies(t *testing.T) {
	ctx := context.Background()

	// Start NATS container with JetStream
	natsContainer, natsURL := startNATSContainerWithJS(ctx, t)
	defer natsContainer.Terminate(ctx)

	// Create and connect client
	client, err := NewClient(natsURL)
	require.NoError(t, err)
	err = client.Connect(ctx)
	require.NoError(t, err)
	defer client.Close(ctx)

	// Create stream
	streamCfg := jetstream.StreamConfig{
		Name:     "ACK_STREAM",
		Subjects: []string{"ack.>"},
		Storage:  jetstream.MemoryStorage,
		MaxAge:   testStreamMaxAge,
		MaxBytes: testStreamMaxBytes,
	}
	_, err = client.EnsureStream(ctx, streamCfg)
	require.NoError(t, err)

	// Test explicit ack
	var received atomic.Int32
	cfg := StreamConsumerConfig{
		StreamName:    "ACK_STREAM",
		ConsumerName:  "explicit-consumer",
		FilterSubject: "ack.explicit",
		DeliverPolicy: "all",
		AckPolicy:     "explicit",
	}

	err = client.ConsumeStreamWithConfig(ctx, PortConsumerContext{Component: "integration", Port: "input"}, cfg, func(_ context.Context, msg jetstream.Msg) {
		received.Add(1)
		// Explicitly ack
		msg.Ack()
	})
	require.NoError(t, err)

	// Publish and verify
	js, err := client.JetStream()
	require.NoError(t, err)

	_, err = js.Publish(ctx, "ack.explicit", []byte("test"))
	require.NoError(t, err)

	time.Sleep(200 * time.Millisecond)
	assert.Equal(t, int32(1), received.Load())
}

// TestIntegration_ConsumeStreamWithConfig_Nak tests message Nak behavior
func TestIntegration_ConsumeStreamWithConfig_Nak(t *testing.T) {
	ctx := context.Background()

	// Start NATS container with JetStream
	natsContainer, natsURL := startNATSContainerWithJS(ctx, t)
	defer natsContainer.Terminate(ctx)

	// Create and connect client
	client, err := NewClient(natsURL)
	require.NoError(t, err)
	err = client.Connect(ctx)
	require.NoError(t, err)
	defer client.Close(ctx)

	// Create stream
	streamCfg := jetstream.StreamConfig{
		Name:     "NAK_STREAM",
		Subjects: []string{"nak.>"},
		Storage:  jetstream.MemoryStorage,
		MaxAge:   testStreamMaxAge,
		MaxBytes: testStreamMaxBytes,
	}
	_, err = client.EnsureStream(ctx, streamCfg)
	require.NoError(t, err)

	// Consumer that Naks first delivery, then Acks
	var deliveryCount atomic.Int32
	cfg := StreamConsumerConfig{
		StreamName:    "NAK_STREAM",
		ConsumerName:  "nak-consumer",
		FilterSubject: "nak.test",
		DeliverPolicy: "all",
		AckPolicy:     "explicit",
		MaxDeliver:    3,
		AckWait:       100 * time.Millisecond,
	}

	err = client.ConsumeStreamWithConfig(ctx, PortConsumerContext{Component: "integration", Port: "input"}, cfg, func(_ context.Context, msg jetstream.Msg) {
		count := deliveryCount.Add(1)
		if count == 1 {
			// First delivery - Nak for redelivery
			msg.Nak()
		} else {
			// Second delivery - Ack
			msg.Ack()
		}
	})
	require.NoError(t, err)

	// Publish message
	js, err := client.JetStream()
	require.NoError(t, err)

	_, err = js.Publish(ctx, "nak.test", []byte("test"))
	require.NoError(t, err)

	// Wait for redelivery
	time.Sleep(500 * time.Millisecond)

	// Should have been delivered at least twice (Nak then Ack)
	assert.GreaterOrEqual(t, deliveryCount.Load(), int32(2))
}

// TestIntegration_ConsumeStreamWithConfig_MissingStreamName tests validation
func TestIntegration_ConsumeStreamWithConfig_MissingStreamName(t *testing.T) {
	ctx := context.Background()

	// Start NATS container with JetStream
	natsContainer, natsURL := startNATSContainerWithJS(ctx, t)
	defer natsContainer.Terminate(ctx)

	// Create and connect client
	client, err := NewClient(natsURL)
	require.NoError(t, err)
	err = client.Connect(ctx)
	require.NoError(t, err)
	defer client.Close(ctx)

	cfg := StreamConsumerConfig{
		// StreamName intentionally omitted
		ConsumerName: "test-consumer",
	}

	err = client.ConsumeStreamWithConfig(ctx, PortConsumerContext{Component: "integration", Port: "input"}, cfg, func(_ context.Context, _ jetstream.Msg) {})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "stream name is required")
}

// TestIntegration_ConsumeStreamWithConfig_NotConnected tests behavior when not connected
func TestIntegration_ConsumeStreamWithConfig_NotConnected(t *testing.T) {
	client, err := NewClient("nats://localhost:4222")
	require.NoError(t, err)

	ctx := context.Background()
	cfg := StreamConsumerConfig{
		StreamName:   "TEST_STREAM",
		ConsumerName: "test-consumer",
	}

	err = client.ConsumeStreamWithConfig(ctx, PortConsumerContext{Component: "integration", Port: "input"}, cfg, func(_ context.Context, _ jetstream.Msg) {})
	assert.Equal(t, ErrNotConnected, err)
}

// TestIntegration_StopConsumer tests stopping a specific consumer
func TestIntegration_StopConsumer(t *testing.T) {
	ctx := context.Background()

	// Start NATS container with JetStream
	natsContainer, natsURL := startNATSContainerWithJS(ctx, t)
	defer natsContainer.Terminate(ctx)

	// Create and connect client
	client, err := NewClient(natsURL)
	require.NoError(t, err)
	err = client.Connect(ctx)
	require.NoError(t, err)
	defer client.Close(ctx)

	// Create stream
	streamCfg := jetstream.StreamConfig{
		Name:     "STOP_STREAM",
		Subjects: []string{"stop.>"},
		Storage:  jetstream.MemoryStorage,
		MaxAge:   testStreamMaxAge,
		MaxBytes: testStreamMaxBytes,
	}
	_, err = client.EnsureStream(ctx, streamCfg)
	require.NoError(t, err)

	// Start consumer
	cfg := StreamConsumerConfig{
		StreamName:    "STOP_STREAM",
		ConsumerName:  "stop-consumer",
		FilterSubject: "stop.test",
		DeliverPolicy: "all",
		AckPolicy:     "explicit",
	}

	err = client.ConsumeStreamWithConfig(ctx, PortConsumerContext{Component: "integration", Port: "input"}, cfg, func(_ context.Context, msg jetstream.Msg) {
		msg.Ack()
	})
	require.NoError(t, err)

	// Stop consumer - should not panic
	client.StopConsumer("STOP_STREAM", "stop-consumer")

	// Stop again - should be no-op
	client.StopConsumer("STOP_STREAM", "stop-consumer")
}

// TestIntegration_StopAllConsumers tests stopping all consumers
func TestIntegration_StopAllConsumers(t *testing.T) {
	ctx := context.Background()

	// Start NATS container with JetStream
	natsContainer, natsURL := startNATSContainerWithJS(ctx, t)
	defer natsContainer.Terminate(ctx)

	// Create and connect client
	client, err := NewClient(natsURL)
	require.NoError(t, err)
	err = client.Connect(ctx)
	require.NoError(t, err)
	defer client.Close(ctx)

	// Create stream
	streamCfg := jetstream.StreamConfig{
		Name:     "STOPALL_STREAM",
		Subjects: []string{"stopall.>"},
		Storage:  jetstream.MemoryStorage,
		MaxAge:   testStreamMaxAge,
		MaxBytes: testStreamMaxBytes,
	}
	_, err = client.EnsureStream(ctx, streamCfg)
	require.NoError(t, err)

	// Start multiple consumers
	for i := 0; i < 3; i++ {
		cfg := StreamConsumerConfig{
			StreamName:    "STOPALL_STREAM",
			ConsumerName:  "stopall-consumer-" + string(rune('a'+i)),
			FilterSubject: "stopall.test",
			DeliverPolicy: "all",
			AckPolicy:     "explicit",
		}

		err = client.ConsumeStreamWithConfig(ctx, PortConsumerContext{Component: "integration", Port: "input"}, cfg, func(_ context.Context, msg jetstream.Msg) {
			msg.Ack()
		})
		require.NoError(t, err)
	}

	// Stop all consumers - should not panic
	client.StopAllConsumers()

	// Stop again - should be no-op
	client.StopAllConsumers()
}

// TestIntegration_PublishToStreamWithAck tests publishing with acknowledgment
func TestIntegration_PublishToStreamWithAck(t *testing.T) {
	ctx := context.Background()

	// Start NATS container with JetStream
	natsContainer, natsURL := startNATSContainerWithJS(ctx, t)
	defer natsContainer.Terminate(ctx)

	// Create and connect client
	client, err := NewClient(natsURL)
	require.NoError(t, err)
	err = client.Connect(ctx)
	require.NoError(t, err)
	defer client.Close(ctx)

	// Create stream
	streamCfg := jetstream.StreamConfig{
		Name:     "PUBACK_STREAM",
		Subjects: []string{"puback.>"},
		Storage:  jetstream.MemoryStorage,
		MaxAge:   testStreamMaxAge,
		MaxBytes: testStreamMaxBytes,
	}
	_, err = client.EnsureStream(ctx, streamCfg)
	require.NoError(t, err)

	// Publish with ack
	ack, err := client.PublishToStreamWithAck(ctx, "puback.test", []byte("test message"))
	require.NoError(t, err)
	assert.NotNil(t, ack)
	assert.Equal(t, "PUBACK_STREAM", ack.Stream)
	assert.GreaterOrEqual(t, ack.Sequence, uint64(1))
}

// TestIntegration_PublishToStreamWithAck_NotConnected tests publish when not connected
func TestIntegration_PublishToStreamWithAck_NotConnected(t *testing.T) {
	client, err := NewClient("nats://localhost:4222")
	require.NoError(t, err)

	ctx := context.Background()
	_, err = client.PublishToStreamWithAck(ctx, "test.subject", []byte("data"))
	assert.Equal(t, ErrNotConnected, err)
}

// TestIntegration_DefaultStreamConfig tests default configuration values.
//
// The bounds are asserted ABSENT deliberately. This used to return MaxAge 7 days
// with MaxBytes unset — the exact silent framework default the bounds requirement
// removed from the configuration path, handing out a retention window nobody chose
// and no size ceiling at all. A caller that auto-creates now states its own bounds
// or the seam refuses the creation.
func TestIntegration_DefaultStreamConfig(t *testing.T) {
	cfg := DefaultStreamConfig()

	assert.Equal(t, "file", cfg.Storage)
	assert.Equal(t, "limits", cfg.Retention)
	assert.Equal(t, 1, cfg.Replicas)
	assert.Zero(t, cfg.MaxAge, "no default retention window: a bound nobody chose is what this requirement ends")
	assert.Zero(t, cfg.MaxBytes, "and no default size ceiling")
}
