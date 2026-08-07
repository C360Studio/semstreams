//go:build integration

package agenticloop_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/payloadbuiltins"
	agenticloop "github.com/c360studio/semstreams/processor/agentic-loop"
)

// TestIntegration_AgenticLoopStart_ContentStoreRetentionGraceful drives the
// FINDING-1 agentic-loop wire end-to-end against real NATS: Start →
// initializeKVBuckets → the trajectory content store's D2 retention guard
// (#600/#616). The trajectory content store is legitimately OPTIONAL, but when its
// backing stream carries a legacy 24h TTL the guard must STRIP it in place and Start
// must boot cleanly (the strippable, graceful path the new fatal branch must not
// disturb). This proves the component runs the guard on its content store and that
// graceful degradation is intact.
func TestIntegration_AgenticLoopStart_ContentStoreRetentionGraceful(t *testing.T) {
	natsClient := getSharedNATSClient(t)
	ctx := context.Background()
	const bucket = "AL_START_LEGACY_CONTENT"

	// Seed a pre-contract content backing stream: OBJ_<bucket> with the historical
	// 24h TTL the D2 guard exists to strip.
	js, err := natsClient.JetStream()
	require.NoError(t, err)
	_, err = js.CreateObjectStore(ctx, jetstream.ObjectStoreConfig{
		Bucket:      bucket,
		Description: "legacy retention fixture (agentic-loop start)",
		TTL:         24 * time.Hour,
	})
	require.NoError(t, err)

	config := agenticloop.Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name: "tasks", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"agent.task.*"}}, Required: true,
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name: "complete", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"agent.complete.*"}},
				},
			},
		},
		MaxIterations:      10,
		Timeout:            "60s",
		StreamName:         "AGENT",
		ConsumerNameSuffix: "content-retention-graceful-test",
		LoopsBucket:        "AGENT_LOOPS",
		ContentBucket:      bucket,
	}

	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)

	comp, err := agenticloop.NewComponent(rawConfig, component.Dependencies{
		NATSClient:      natsClient,
		PayloadRegistry: payloadbuiltins.NewTestRegistry(t),
	})
	require.NoError(t, err)

	lc, ok := comp.(component.LifecycleComponent)
	require.True(t, ok)
	require.NoError(t, lc.Initialize())

	startCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	// Start must NOT fail closed: the legacy TTL is strippable, so the guard
	// reconciles it and the component boots.
	require.NoError(t, lc.Start(startCtx),
		"a strippable legacy retention on the content store must reconcile, not fail Start")
	t.Cleanup(func() { _ = lc.Stop(5 * time.Second) })

	// The backing stream's TTL must have been stripped in place by the guard.
	stream, err := js.Stream(ctx, "OBJ_"+bucket)
	require.NoError(t, err)
	info, err := stream.Info(ctx)
	require.NoError(t, err)
	assert.Equal(t, time.Duration(0), info.Config.MaxAge,
		"Start's content-store guard must have stripped the legacy TTL in place")
}
