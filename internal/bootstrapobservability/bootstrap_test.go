package bootstrapobservability

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"reflect"
	"sync"
	"testing"

	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/types"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBootFailuresAreLoggedExactlyOnceAndReturnedWithCause(t *testing.T) {
	t.Run("client creation", func(t *testing.T) {
		logger, output := newFailureTestLogger(t)
		_, err := NewClient("nats://127.0.0.1:1", logger, nil)
		require.ErrorContains(t, err, "client metrics registry cannot be nil")
		assertOneBootFailure(t, output, "client-create", "client metrics registry cannot be nil")
	})

	t.Run("client connection", func(t *testing.T) {
		logger, output := newFailureTestLogger(t)
		client, err := NewClient("nats://127.0.0.1:1", logger, metric.NewMetricsRegistry())
		require.NoError(t, err)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		err = ConnectClient(ctx, client, logger)
		require.Error(t, err)
		assert.True(t, errors.Is(err, context.Canceled), "returned error must preserve context cancellation: %v", err)
		assertOneBootFailure(t, output, "client-connect", "connect to NATS")
	})

	t.Run("config manager creation", func(t *testing.T) {
		logger, output := newFailureTestLogger(t)
		_, _, err := StartConfigManager(t.Context(), &config.Config{}, nil, logger)
		require.ErrorContains(t, err, "create config manager: nats client cannot be nil")
		assertOneBootFailure(t, output, "config-manager-create", "nats client cannot be nil")
	})

	t.Run("effective validation", func(t *testing.T) {
		logger, output := newFailureTestLogger(t)
		err := ValidateEffectiveConfig(&config.Config{}, logger)
		require.ErrorContains(t, err, "platform.org is required")
		assertOneBootFailure(t, output, "effective-config-validation", "platform.org is required")
	})

	t.Run("stream provisioning", func(t *testing.T) {
		logger, output := newFailureTestLogger(t)
		cfg := &config.Config{Streams: config.StreamConfigs{
			"UNBOUNDED": {Subjects: []string{"unbounded.>"}},
		}}
		err := EnsureEffectiveStreams(t.Context(), cfg, nil, logger)
		require.Error(t, err)
		assert.True(t, errors.Is(err, config.ErrStreamBoundsUndeclared),
			"returned error must preserve the stream-bounds cause: %v", err)
		assertOneBootFailure(t, output, "stream-provisioning", "ensure streams")
	})

	t.Run("forwarder composition", func(t *testing.T) {
		logger, output := newFailureTestLogger(t)
		_, err := NewForwardingHandler(types.ServiceConfigs{
			"log-forwarder": {Enabled: true, Config: json.RawMessage(`{"min_level":`)},
		}, &recordingPublisher{published: make(chan string, 1)}, logger)
		require.ErrorContains(t, err, "resolve enabled log-forwarder policy")
		assertOneBootFailure(t, output, "log-forwarder-composition", "decode log-forwarder config")
	})
}

func TestProductionPhaseALoggingReusesLocalHandlerAndCountsClientWarnExactlyOnce(t *testing.T) {
	var output lockedBuffer
	metrics, phase, err := NewProductionPhaseA(&output, "debug", "json",
		[]slog.Attr{slog.String("service", "semstreams"), slog.String("version", "test")})
	require.NoError(t, err)

	client, err := NewClient("nats://127.0.0.1:1", phase.Client, metrics)
	require.NoError(t, err)
	_ = client
	phase.Client.Warn("client warning")

	records := decodeJSONRecords(t, output.Bytes())
	require.Len(t, records, 2, "NewClient debug plus the explicit warning use configured local output")
	assert.Equal(t, "natsclient", records[0]["component"])
	assert.Equal(t, "semstreams", records[0]["service"])
	assert.Equal(t, "natsclient", records[1]["component"])
	assert.Equal(t, float64(1), testutil.ToFloat64(
		metrics.CoreMetrics().LogEntriesTotal.WithLabelValues("natsclient", "warn")))
	assert.Equal(t, float64(0), testutil.ToFloat64(
		metrics.CoreMetrics().LogEntriesTotal.WithLabelValues("unknown", "warn")))
	assertRegisteredMetric(t, metrics, "jetstream.stream_messages")
}

func TestE2EPhaseALoggingIsLocalOnlyAndDoesNotCount(t *testing.T) {
	var output lockedBuffer
	metrics, phase, err := NewE2EPhaseA(&output, "info", "json",
		[]slog.Attr{slog.String("service", "e2e-semstreams"), slog.String("version", "test")})
	require.NoError(t, err)

	_, err = NewClient("nats://127.0.0.1:1", phase.Client, metrics)
	require.NoError(t, err)
	phase.Client.Warn("client warning")
	phase.Process.Warn("application warning")

	records := decodeJSONRecords(t, output.Bytes())
	require.Len(t, records, 2)
	assert.Equal(t, "natsclient", records[0]["component"])
	assert.Nil(t, records[1]["component"])
	assert.Equal(t, float64(0), testutil.ToFloat64(
		metrics.CoreMetrics().LogEntriesTotal.WithLabelValues("natsclient", "warn")))
	assert.Equal(t, float64(0), testutil.ToFloat64(
		metrics.CoreMetrics().LogEntriesTotal.WithLabelValues("unknown", "warn")))
	assertRegisteredMetric(t, metrics, "jetstream.stream_messages")
}

func TestLoggerChildrenKeepCommonBaseAttributes(t *testing.T) {
	var output lockedBuffer
	local, err := NewLocalHandler(&output, "info", "json")
	require.NoError(t, err)
	phase, err := NewPhaseALogging(local, nil,
		[]slog.Attr{slog.String("service", "semstreams"), slog.String("version", "v1"), slog.Int("pid", 42)})
	require.NoError(t, err)

	phase.Process.Info("process")
	phase.Client.Info("client")
	phase.ConfigManager.Info("config")
	phase.Steady(nil).Info("steady")

	records := decodeJSONRecords(t, output.Bytes())
	require.Len(t, records, 4)
	for _, record := range records {
		assert.Equal(t, "semstreams", record["service"])
		assert.Equal(t, "v1", record["version"])
		assert.Equal(t, float64(42), record["pid"])
	}
	assert.Nil(t, records[0]["component"])
	assert.Equal(t, "natsclient", records[1]["component"])
	assert.Equal(t, "config-manager", records[2]["component"])
	assert.Nil(t, records[3]["component"])
}

func TestForwardingHandlerUsesOnlyEnabledEffectivePolicy(t *testing.T) {
	tests := []struct {
		name        string
		services    types.ServiceConfigs
		level       slog.Level
		component   string
		wantForward bool
		wantErr     string
	}{
		{name: "absent does not decode", services: nil, level: slog.LevelError},
		{name: "disabled malformed does not decode", services: types.ServiceConfigs{
			"log-forwarder": {Enabled: false, Config: json.RawMessage(`{"min_level":`)},
		}, level: slog.LevelError},
		{name: "default info", services: types.ServiceConfigs{
			"log-forwarder": {Enabled: true, Config: json.RawMessage(`{}`)},
		}, level: slog.LevelInfo, component: "worker", wantForward: true},
		{name: "warn suppresses info", services: types.ServiceConfigs{
			"log-forwarder": {Enabled: true, Config: json.RawMessage(`{"min_level":"warn"}`)},
		}, level: slog.LevelInfo, component: "worker"},
		{name: "warn forwards warn", services: types.ServiceConfigs{
			"log-forwarder": {Enabled: true, Config: json.RawMessage(`{"min_level":"warn"}`)},
		}, level: slog.LevelWarn, component: "worker", wantForward: true},
		{name: "mandatory websocket exclusion", services: types.ServiceConfigs{
			"log-forwarder": {Enabled: true, Config: json.RawMessage(`{"exclude_sources":[]}`)},
		}, level: slog.LevelError, component: "flow-service.websocket.health"},
		{name: "configured dotted-prefix exclusion", services: types.ServiceConfigs{
			"log-forwarder": {Enabled: true, Config: json.RawMessage(`{"exclude_sources":["metrics-forwarder"]}`)},
		}, level: slog.LevelError, component: "metrics-forwarder.internal"},
		{name: "enabled malformed is rejected", services: types.ServiceConfigs{
			"log-forwarder": {Enabled: true, Config: json.RawMessage(`{"min_level":`)},
		}, level: slog.LevelError, wantErr: "decode"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			publisher := &recordingPublisher{published: make(chan string, 1)}
			handler, err := NewForwardingHandler(tt.services, publisher, slog.Default())
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			if handler == nil {
				assert.False(t, tt.wantForward)
				return
			}
			logger := slog.New(handler).With("component", tt.component)
			logger.Log(context.Background(), tt.level, "message")
			if tt.wantForward {
				assert.NotEmpty(t, <-publisher.published)
				return
			}
			select {
			case subject := <-publisher.published:
				t.Fatalf("unexpected forwarded record on %s", subject)
			default:
			}
		})
	}
}

type recordingPublisher struct {
	published chan string
}

func (p *recordingPublisher) PublishToStream(_ context.Context, subject string, _ []byte) error {
	p.published <- subject
	return nil
}

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *lockedBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return bytes.Clone(b.b.Bytes())
}

func decodeJSONRecords(t *testing.T, data []byte) []map[string]any {
	t.Helper()
	lines := bytes.Split(bytes.TrimSpace(data), []byte("\n"))
	if len(lines) == 1 && len(lines[0]) == 0 {
		return nil
	}
	records := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		var record map[string]any
		require.NoError(t, json.Unmarshal(line, &record))
		records = append(records, record)
	}
	return records
}

func assertRegisteredMetric(t *testing.T, metrics *metric.MetricsRegistry, key string) {
	t.Helper()
	registered := reflect.ValueOf(metrics).Elem().FieldByName("registeredMetrics")
	for _, registeredKey := range registered.MapKeys() {
		if registeredKey.String() == key {
			return
		}
	}
	t.Fatalf("metric %q was not registered before Connect", key)
}

func newFailureTestLogger(t *testing.T) (*slog.Logger, *lockedBuffer) {
	t.Helper()
	output := &lockedBuffer{}
	handler, err := NewLocalHandler(output, "info", "json")
	require.NoError(t, err)
	return slog.New(handler), output
}

func assertOneBootFailure(t *testing.T, output *lockedBuffer, stage, errorText string) {
	t.Helper()
	records := decodeJSONRecords(t, output.Bytes())
	failures := make([]map[string]any, 0, 1)
	for _, record := range records {
		if record["msg"] == "Boot phase failed" {
			failures = append(failures, record)
		}
	}
	require.Len(t, failures, 1, "the returned boot failure must have one configured-local record")
	assert.Equal(t, "ERROR", failures[0]["level"])
	assert.Equal(t, stage, failures[0]["boot_stage"])
	assert.Contains(t, failures[0]["error"], errorText)
}
