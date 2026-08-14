package agenticgovernance

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

func TestViolationHandlerRetainsAuditAdminMetricsLoggingAndViolationEvent(t *testing.T) {
	server, err := natsserver.NewServer(&natsserver.Options{
		Port: -1, JetStream: true, StoreDir: t.TempDir(), NoLog: true, NoSigs: true,
	})
	require.NoError(t, err)
	go server.Start()
	require.True(t, server.ReadyForConnections(5*time.Second))
	t.Cleanup(server.Shutdown)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	client, err := natsclient.NewClient(server.ClientURL())
	require.NoError(t, err)
	require.NoError(t, client.Connect(ctx))
	t.Cleanup(func() { _ = client.Close(context.Background()) })
	_, err = client.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket: "GOVERNANCE_VIOLATIONS", History: 1,
	})
	require.NoError(t, err)

	connection := client.GetConnection()
	adminSub, err := connection.SubscribeSync("admin.governance.alert")
	require.NoError(t, err)
	violationSub, err := connection.SubscribeSync("governance.violation.>")
	require.NoError(t, err)
	require.NoError(t, connection.Flush())

	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	metrics := getMetrics(nil)
	before := testutil.ToFloat64(metrics.violationTotal.WithLabelValues("injection_detection", string(SeverityHigh)))
	defaults := DefaultConfig()
	handler := NewViolationHandler(defaults.Violations, client, logger, metrics, defaults.Ports.Outputs)
	violation := &Violation{
		ID: "preservation-1", FilterName: "injection_detection", Severity: SeverityHigh,
		Timestamp: time.Now().UTC(), UserID: "user-1", ChannelID: "cli-1", Action: ViolationActionBlocked,
	}

	require.NoError(t, handler.Handle(ctx, violation))
	_, err = adminSub.NextMsgWithContext(ctx)
	require.NoError(t, err)
	_, err = violationSub.NextMsgWithContext(ctx)
	require.NoError(t, err)
	require.Equal(t, before+1,
		testutil.ToFloat64(metrics.violationTotal.WithLabelValues("injection_detection", string(SeverityHigh))))
	require.Contains(t, logs.String(), "Policy violation detected")

	bucket, err := client.GetKeyValueBucket(ctx, "GOVERNANCE_VIOLATIONS")
	require.NoError(t, err)
	entry, err := bucket.Get(ctx, "violation.preservation-1")
	require.NoError(t, err)
	require.NotEmpty(t, entry.Value())

	for _, output := range defaults.Ports.Outputs {
		require.NotEqual(t, "user_errors", output.Name)
		facts, factsErr := output.Resolve(component.DirectionOutput)
		require.NoError(t, factsErr)
		require.NotNil(t, facts.Config)
	}
}

func TestViolationStoreRejectsInvalidIDBeforeNATSIO(t *testing.T) {
	handler := NewViolationHandler(ViolationConfig{Store: "GOVERNANCE_VIOLATIONS"},
		&natsclient.Client{}, slog.Default(), nil, nil)
	err := handler.storeViolation(t.Context(), &Violation{ID: "invalid:id"})
	require.Error(t, err)
	var classified *errs.ClassifiedError
	require.ErrorAs(t, err, &classified)
	require.Equal(t, natsclient.ErrorCodeKVKeyInvalid, classified.Code)
	require.True(t, errs.IsInvalid(err))
	require.NotErrorIs(t, err, natsclient.ErrNotConnected,
		"key validation must precede bucket lookup or any other NATS I/O")
}
