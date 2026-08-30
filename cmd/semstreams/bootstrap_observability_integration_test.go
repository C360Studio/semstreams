//go:build integration

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/internal/bootstrapobservability"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/logging"
	"github.com/c360studio/semstreams/types"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegrationProductionBootstrapObservability(t *testing.T) {
	t.Setenv("SEMSTREAMS_NATS_URLS", "")
	testNATS := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithKV())

	t.Run("KV-selected policy and effective streams precede forwarding", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()

		fileCfg := bootstrapIntegrationConfig(testNATS.URL)
		fileCfg.Services["log-forwarder"] = types.ServiceConfig{
			Enabled: false,
			Config:  json.RawMessage(`{"min_level":`),
		}
		seedEffectiveForwarderConfig(t, ctx, testNATS.Client, fileCfg.Platform)

		metrics := metric.NewMetricsRegistry()
		local, err := bootstrapobservability.NewLocalHandler(&channelWriter{records: make(chan []byte, 64)}, "debug", "json")
		require.NoError(t, err)
		phase, err := bootstrapobservability.NewPhaseALogging(
			local,
			logging.NewCounterHandler(metrics.CoreMetrics().LogEntriesTotal),
			[]slog.Attr{slog.String("service", "semstreams"), slog.String("version", "integration")},
		)
		require.NoError(t, err)

		client, err := createNATSClient(fileCfg, phase.Client, metrics)
		require.NoError(t, err)
		require.NoError(t, client.Connect(ctx))
		t.Cleanup(func() { _ = client.Close(context.Background()) })
		require.NoError(t, client.WaitForConnection(ctx))

		manager, effective, err := bootstrapobservability.StartConfigManager(
			ctx, fileCfg, client, phase.ConfigManager,
		)
		require.NoError(t, err)
		t.Cleanup(func() { _ = manager.Stop(5 * time.Second) })
		require.True(t, effective.Services["log-forwarder"].Enabled)
		require.JSONEq(t, `{"min_level":"WARN","exclude_sources":["kv-excluded"]}`,
			string(effective.Services["log-forwarder"].Config))
		require.NoError(t, bootstrapobservability.ValidateEffectiveConfig(effective, phase.ConfigManager))

		js, err := client.JetStream()
		require.NoError(t, err)
		_, err = js.Stream(ctx, "LOGS")
		require.ErrorIs(t, err, jetstream.ErrStreamNotFound,
			"config arbitration must complete while forwarding is still impossible")

		require.NoError(t, bootstrapobservability.EnsureEffectiveStreams(ctx, effective, client, phase.ConfigManager))
		_, err = js.Stream(ctx, "LOGS")
		require.NoError(t, err, "effective stream provisioning completes before forwarding composition")

		sub, err := client.GetConnection().SubscribeSync("logs.>")
		require.NoError(t, err)
		t.Cleanup(func() { _ = sub.Unsubscribe() })
		require.NoError(t, client.GetConnection().FlushWithContext(ctx))

		forwarder, err := bootstrapobservability.NewForwardingHandler(
			effective.Services, client, phase.Process,
		)
		require.NoError(t, err)
		require.NotNil(t, forwarder, "KV enabled forwarding even though the file entry was disabled and malformed")
		logger := phase.Steady(forwarder).With("component", "kv-worker")
		logger.Info("below effective forwarding level")
		logger.Warn("forwarded under effective KV policy")

		msg, err := sub.NextMsgWithContext(ctx)
		require.NoError(t, err)
		assert.Equal(t, "logs.WARN.kv-worker", msg.Subject)
		assert.Equal(t, float64(1), testutil.ToFloat64(
			metrics.CoreMetrics().LogEntriesTotal.WithLabelValues("kv-worker", "warn")))
	})

	t.Run("config-manager start failure is returned and locally visible once", func(t *testing.T) {
		writer := &channelWriter{records: make(chan []byte, 64)}
		local, err := bootstrapobservability.NewLocalHandler(writer, "info", "json")
		require.NoError(t, err)
		phase, err := bootstrapobservability.NewPhaseALogging(
			local,
			nil,
			[]slog.Attr{slog.String("service", "semstreams"), slog.String("version", "integration")},
		)
		require.NoError(t, err)

		canceledCtx, cancel := context.WithCancel(t.Context())
		cancel()
		_, _, err = bootstrapobservability.StartConfigManager(
			canceledCtx, bootstrapIntegrationConfig(testNATS.URL), testNATS.Client, phase.ConfigManager,
		)
		// The canceled context now fails at the first bucket read — identity is
		// established before watchers are created (ADR-104). What this subtest
		// pins is the boot-failure SHAPE below, not which step failed first.
		require.ErrorContains(t, err, "start config manager: ")
		require.ErrorContains(t, err, "context canceled")

		records := drainJSONRecords(t, writer.records)
		var failures []map[string]any
		for _, record := range records {
			if record["msg"] == "Boot phase failed" {
				failures = append(failures, record)
			}
		}
		require.Len(t, failures, 1)
		assert.Equal(t, "config-manager-start", failures[0]["boot_stage"])
		assert.Equal(t, "config-manager", failures[0]["component"])
	})

	t.Run("real async client diagnostic is local and counted but never self-forwarded", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		defer cancel()

		writer := &channelWriter{records: make(chan []byte, 64)}
		local, err := bootstrapobservability.NewLocalHandler(writer, "debug", "json")
		require.NoError(t, err)
		metrics := metric.NewMetricsRegistry()
		phase, err := bootstrapobservability.NewPhaseALogging(
			local,
			logging.NewCounterHandler(metrics.CoreMetrics().LogEntriesTotal),
			[]slog.Attr{slog.String("service", "semstreams"), slog.String("version", "integration")},
		)
		require.NoError(t, err)

		cfg := bootstrapIntegrationConfig(testNATS.URL)
		cfg.Services["log-forwarder"] = types.ServiceConfig{
			Enabled: true,
			Config:  json.RawMessage(`{"min_level":"DEBUG"}`),
		}
		client, err := createNATSClient(cfg, phase.Client, metrics)
		require.NoError(t, err)
		require.NoError(t, client.Connect(ctx))
		t.Cleanup(func() { _ = client.Close(context.Background()) })
		require.NoError(t, client.WaitForConnection(ctx))
		require.NoError(t, bootstrapobservability.EnsureEffectiveStreams(ctx, cfg, client, phase.ConfigManager))

		logsSub, err := client.GetConnection().SubscribeSync("logs.>")
		require.NoError(t, err)
		t.Cleanup(func() { _ = logsSub.Unsubscribe() })
		require.NoError(t, client.GetConnection().FlushWithContext(ctx))

		forwarder, err := bootstrapobservability.NewForwardingHandler(
			cfg.Services, client, phase.Process,
		)
		require.NoError(t, err)
		require.NotNil(t, forwarder)
		steadyLogger := phase.Steady(forwarder).With("component", "application-sentinel")
		steadyLogger.Info("forwarding-active-before-client-diagnostic")
		sentinel, err := logsSub.NextMsgWithContext(ctx)
		require.NoError(t, err)
		assert.Equal(t, "logs.INFO.application-sentinel", sentinel.Subject,
			"same-client steady-state forwarding must be active before the negative recursion assertion")

		handlerEntered := make(chan struct{})
		releaseHandler := make(chan struct{})
		var enterOnce sync.Once
		var releaseOnce sync.Once
		release := func() { releaseOnce.Do(func() { close(releaseHandler) }) }
		t.Cleanup(release)
		slowSub, err := client.GetConnection().Subscribe("diagnostics.gh955", func(_ *nats.Msg) {
			enterOnce.Do(func() { close(handlerEntered) })
			<-releaseHandler
		})
		require.NoError(t, err)
		t.Cleanup(func() { _ = slowSub.Unsubscribe() })
		require.NoError(t, slowSub.SetPendingLimits(1, -1))
		require.NoError(t, client.GetConnection().FlushWithContext(ctx))

		require.NoError(t, client.GetConnection().Publish("diagnostics.gh955", []byte("block")))
		require.NoError(t, client.GetConnection().FlushWithContext(ctx))
		select {
		case <-handlerEntered:
		case <-ctx.Done():
			t.Fatalf("slow-consumer handler did not block: %v", ctx.Err())
		}
		for i := range 8 {
			require.NoError(t, client.GetConnection().Publish(
				"diagnostics.gh955", []byte(fmt.Sprintf("overflow-%d", i))))
		}
		require.NoError(t, client.GetConnection().FlushWithContext(ctx))

		record := waitForJSONRecord(t, ctx, writer.records, "NATS error")
		assert.Equal(t, "ERROR", record["level"])
		assert.Equal(t, "natsclient", record["component"])
		assert.Equal(t, float64(1), testutil.ToFloat64(
			metrics.CoreMetrics().LogEntriesTotal.WithLabelValues("natsclient", "error")))

		// The sentinel above proves this exact client forwards steady-state
		// application logs. The local observation proves the async callback
		// completed; Flush then bounds the negative assertion without a sleep.
		require.NoError(t, client.GetConnection().FlushWithContext(ctx))
		noMessageCtx, noMessageCancel := context.WithTimeout(ctx, 250*time.Millisecond)
		defer noMessageCancel()
		_, err = logsSub.NextMsgWithContext(noMessageCtx)
		require.Error(t, err)
		assert.True(t, errors.Is(err, context.DeadlineExceeded) || errors.Is(err, nats.ErrTimeout),
			"unexpected logs.> receive result: %v", err)
		release()
	})
}

func drainJSONRecords(t *testing.T, records <-chan []byte) []map[string]any {
	t.Helper()
	result := make([]map[string]any, 0, len(records))
	for {
		select {
		case data := <-records:
			var record map[string]any
			require.NoError(t, json.Unmarshal(data, &record))
			result = append(result, record)
		default:
			return result
		}
	}
}

func bootstrapIntegrationConfig(url string) *config.Config {
	return &config.Config{
		Version: "1.0.0",
		Platform: config.PlatformConfig{
			Org: "c360", ID: "gh955-bootstrap", Type: "test",
		},
		NATS: config.NATSConfig{
			URLs:      []string{url},
			JetStream: config.JetStreamConfig{Enabled: true},
		},
		Services:   make(types.ServiceConfigs),
		Components: make(config.ComponentConfigs),
	}
}

func seedEffectiveForwarderConfig(
	t *testing.T,
	ctx context.Context,
	client *natsclient.Client,
	platform config.PlatformConfig,
) {
	t.Helper()
	bucket, err := client.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket: "semstreams_config", History: 5,
	})
	require.NoError(t, err)
	putKVJSON(t, ctx, bucket, "version", "2.0.0")
	putKVJSON(t, ctx, bucket, "platform", platform)
	// A bucket that already holds configuration must also hold its identity
	// record, or Start refuses it as predating identity minting (ADR-104).
	// id == stem is the operator-provisioned, unsuffixed form.
	putKVJSON(t, ctx, bucket, "platform_identity", map[string]string{
		"org": platform.Org, "stem": platform.ID, "id": platform.ID,
	})
	putKVJSON(t, ctx, bucket, "services.log-forwarder", types.ServiceConfig{
		Enabled: true,
		Config:  json.RawMessage(`{"min_level":"WARN","exclude_sources":["kv-excluded"]}`),
	})
}

func putKVJSON(t *testing.T, ctx context.Context, bucket jetstream.KeyValue, key string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	require.NoError(t, err)
	_, err = bucket.Put(ctx, key, data)
	require.NoError(t, err)
}

type channelWriter struct {
	records chan []byte
}

func (w *channelWriter) Write(data []byte) (int, error) {
	copyOfData := append([]byte(nil), data...)
	w.records <- copyOfData
	return len(data), nil
}

func waitForJSONRecord(
	t *testing.T,
	ctx context.Context,
	records <-chan []byte,
	message string,
) map[string]any {
	t.Helper()
	for {
		select {
		case data := <-records:
			var record map[string]any
			require.NoError(t, json.Unmarshal(data, &record))
			if record["msg"] == message {
				return record
			}
		case <-ctx.Done():
			t.Fatalf("did not observe log message %q: %v", message, ctx.Err())
		}
	}
}
