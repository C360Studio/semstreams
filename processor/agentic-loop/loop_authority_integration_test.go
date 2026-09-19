//go:build integration

package agenticloop

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

// spec: agentic-loop / Loop-state authority has one port declaration
// spec: agentic-loop / Loop-state authority is acquired and observed before loop work
func TestIntegration_LoopAuthorityAdmission(t *testing.T) {
	// Real server configuration and concurrent public Start acquisition cannot
	// be established by the manager fake. One fixture owns all admission cases.
	tc := natsclient.NewTestClient(t, natsclient.WithStreams(
		natsclient.TestStreamConfig{Name: "AGENT", Subjects: []string{"agent.>", "tool.>"}},
	))
	js, err := tc.Client.JetStream()
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	t.Cleanup(cancel)
	runID := strings.ReplaceAll(uuid.NewString(), "-", "")
	newProcess := func(t *testing.T, bucket, suffix string) *Component {
		t.Helper()
		cfg := map[string]any{"consumer_name_suffix": suffix}
		if bucket != "AGENT_LOOPS" {
			cfg["ports"] = component.PortConfig{Outputs: []component.PortDefinition{{
				Name: "loops", Config: component.KVWritePort{Bucket: bucket},
			}}}
		}
		raw, err := json.Marshal(cfg)
		require.NoError(t, err)
		d, err := NewComponent(raw, component.Dependencies{
			NATSClient: tc.Client, PayloadRegistry: payloadbuiltins.NewTestRegistry(t),
		})
		require.NoError(t, err)
		c := d.(*Component)
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			require.NoError(t, c.Stop(ctx))
		})
		return c
	}
	assertStarted := func(t *testing.T, c *Component, bucket string) {
		t.Helper()
		require.NotNil(t, c.loopsBucket)
		require.Equal(t, bucket, c.loopsBucket.Bucket())
		require.NotEmpty(t, c.consumers)
		require.NotNil(t, c.trajectorySub)
		require.NotNil(t, c.inflightSub)
		require.NotNil(t, c.sweeperDone)
		status, err := c.loopsBucket.Status(ctx)
		require.NoError(t, err)
		require.EqualValues(t, 10, status.History())
		require.Equal(t, 24*time.Hour, status.TTL())
		info, err := js.Stream(ctx, "KV_"+bucket)
		require.NoError(t, err)
		require.Equal(t, 24*time.Hour, info.CachedInfo().Config.MaxAge)
		require.LessOrEqual(t, info.CachedInfo().Config.MaxBytes, int64(0))
	}
	for _, row := range []struct {
		name, bucket string
		existing     bool
	}{
		{"fresh default", "AGENT_LOOPS", false},
		{"existing custom", "R8_EXISTING_" + runID, true},
	} {
		t.Run(row.name, func(t *testing.T) {
			if row.existing {
				_, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: row.bucket, History: 10, TTL: 24 * time.Hour})
				require.NoError(t, err)
			}
			c := newProcess(t, row.bucket, "r8-"+row.bucket)
			require.NoError(t, c.Start(ctx))
			assertStarted(t, c, row.bucket)
			done := c.sweeperDone
			require.NoError(t, c.Stop(ctx))
			select {
			case <-done:
			default:
				t.Fatal("Stop returned before the approval sweeper joined")
			}
		})
	}
	for _, row := range []struct {
		name     string
		history  uint8
		ttl      time.Duration
		maxBytes int64
	}{
		{"history", 1, 24 * time.Hour, 0},
		{"ttl", 10, time.Hour, 0},
		{"bytes", 10, 24 * time.Hour, 1024 * 1024},
	} {
		t.Run("refuse "+row.name, func(t *testing.T) {
			name := "R8_REFUSE_" + row.name + "_" + runID
			_, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: name, History: row.history, TTL: row.ttl, MaxBytes: row.maxBytes})
			require.NoError(t, err)
			stream, err := js.Stream(ctx, "KV_"+name)
			require.NoError(t, err)
			before := stream.CachedInfo().Config
			c := newProcess(t, name, "r8-"+name)
			err = c.Start(ctx)
			require.ErrorContains(t, err, name)
			require.ErrorContains(t, err, "observed History=")
			require.ErrorContains(t, err, "no reconciliation")
			require.Nil(t, c.loopsBucket)
			require.Nil(t, c.trajectoryBucket)
			require.Nil(t, c.trajectorySub)
			require.Nil(t, c.inflightSub)
			require.Empty(t, c.consumers)
			require.Nil(t, c.sweeperDone)
			require.False(t, c.Health().Healthy)
			require.True(t, c.terminal, "failed Start must complete rollback")
			after, err := stream.Info(ctx)
			require.NoError(t, err)
			require.Equal(t, before, after.Config, "refusal must not reconcile retained policy")
			require.NoError(t, c.Stop(ctx))
		})
	}
	t.Run("concurrent fresh acquisition", func(t *testing.T) {
		name := "R8_RACE_" + runID
		first := newProcess(t, name, "r8-race-first-"+runID)
		second := newProcess(t, name, "r8-race-second-"+runID)
		start := make(chan struct{})
		results := make(chan error, 2)
		for _, c := range []*Component{first, second} {
			go func() {
				<-start
				results <- c.Start(ctx)
			}()
		}
		close(start)
		firstErr, secondErr := <-results, <-results
		require.NoError(t, firstErr)
		require.NoError(t, secondErr)
		// This proves real concurrent owner convergence. The unit manager proof
		// separately forces typed ErrBucketExists and counts the exact re-get.
		assertStarted(t, first, name)
		assertStarted(t, second, name)
		require.NoError(t, first.Stop(ctx))
		require.NoError(t, second.Stop(ctx))
	})
}
