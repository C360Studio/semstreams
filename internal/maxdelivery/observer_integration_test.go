//go:build integration

package maxdelivery

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/natsclient"
)

const integrationTimeout = 30 * time.Second

type integrationTelemetry struct {
	mu        sync.Mutex
	attempts  int
	successes int
	failFirst bool
	events    chan advisory
}

func newIntegrationTelemetry(failFirst bool) *integrationTelemetry {
	return &integrationTelemetry{failFirst: failFirst, events: make(chan advisory, 8)}
}

func (t *integrationTelemetry) reportOccurrence(_ context.Context, event advisory) error {
	t.mu.Lock()
	t.attempts++
	if t.failFirst && t.attempts == 1 {
		t.mu.Unlock()
		return errors.New("injected telemetry failure")
	}
	t.successes++
	t.mu.Unlock()
	t.events <- event
	return nil
}

func (t *integrationTelemetry) reportDecodeError(context.Context, decodeErrorReason, error) {}
func (t *integrationTelemetry) reportSettlementError(context.Context, string, error)        {}

func (t *integrationTelemetry) counts() (int, int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.attempts, t.successes
}

func integrationClient(t *testing.T) (*natsclient.TestClient, context.Context) {
	t.Helper()
	tc := natsclient.NewTestClient(t,
		natsclient.WithJetStream(),
		natsclient.WithFileStorage(),
		natsclient.WithNATSVersion("2.12.4-alpine"),
	)
	ctx, cancel := context.WithTimeout(context.Background(), integrationTimeout)
	t.Cleanup(cancel)
	return tc, ctx
}

func ensureFrameworkStreams(t *testing.T, ctx context.Context, client *natsclient.Client) {
	t.Helper()
	manager := config.NewStreamsManager(client, slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.NoError(t, manager.EnsureStreams(ctx, &config.Config{}))
}

func TestStartFailsLoudlyWhenCaptureStreamIsMissing(t *testing.T) {
	tc, ctx := integrationClient(t)
	telemetry := newIntegrationTelemetry(false)

	stop, err := start(ctx, tc.Client, telemetry)
	require.Nil(t, stop)
	require.Error(t, err)
	require.ErrorIs(t, err, jetstream.ErrStreamNotFound)
}

// TestCaptureBeforeObserverRestart proves why this is a Stream rather than KV
// or a core subscriber: the server occurrence is retained while no observer is
// running, then delivered when the fixed durable binds later.
func TestCaptureBeforeObserverRestart(t *testing.T) {
	tc, ctx := integrationClient(t)
	ensureFrameworkStreams(t, ctx, tc.Client)

	want := forceMaxDeliveryAdvisory(t, ctx, tc.Client, "CAPTURE_BEFORE", "capture.before")
	telemetry := newIntegrationTelemetry(false)
	stopObserver, err := start(ctx, tc.Client, telemetry)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, stopObserver(ctx)) })

	select {
	case got := <-telemetry.events:
		assert.Equal(t, want.Stream, got.Stream)
		assert.Equal(t, want.Consumer, got.Consumer)
		assert.Equal(t, want.StreamSequence, got.StreamSequence)
	case <-ctx.Done():
		t.Fatal("retained MaxDeliver advisory was not delivered after observer bind")
	}

	js, err := tc.Client.JetStream()
	require.NoError(t, err)
	info, err := js.Consumer(ctx, captureStreamName, observerConsumerName)
	require.NoError(t, err)
	consumerInfo, err := info.Info(ctx)
	require.NoError(t, err)
	assert.Equal(t, -1, consumerInfo.Config.MaxDeliver,
		"observer must be unlimited so its own telemetry outage cannot recurse into MaxDeliver exhaustion")
	require.NoError(t, stopObserver(ctx))
	require.NoError(t, stopObserver(ctx), "completed observer Stop is a nil no-op")
	_, err = js.Consumer(ctx, captureStreamName, observerConsumerName)
	require.NoError(t, err, "observer lifecycle must preserve the durable consumer")
}

func TestObserverEmissionFailureRedelivers(t *testing.T) {
	tc, ctx := integrationClient(t)
	ensureFrameworkStreams(t, ctx, tc.Client)
	forceMaxDeliveryAdvisory(t, ctx, tc.Client, "REPORT_RETRY", "report.retry")

	telemetry := newIntegrationTelemetry(true)
	stopObserver, err := start(ctx, tc.Client, telemetry)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, stopObserver(ctx)) })

	select {
	case <-telemetry.events:
	case <-ctx.Done():
		t.Fatal("observer did not redeliver after the injected telemetry failure")
	}
	attempts, successes := telemetry.counts()
	assert.GreaterOrEqual(t, attempts, 2)
	assert.Equal(t, 1, successes)
}

func TestTwoObserversShareOneLogicalDelivery(t *testing.T) {
	tc, ctx := integrationClient(t)
	ensureFrameworkStreams(t, ctx, tc.Client)

	second, err := natsclient.NewClient(tc.URL)
	require.NoError(t, err)
	require.NoError(t, second.Connect(ctx))
	t.Cleanup(func() { _ = second.Close(context.Background()) })

	telemetry := newIntegrationTelemetry(false)
	stopFirst, err := start(ctx, tc.Client, telemetry)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, stopFirst(ctx)) })
	stopSecond, err := start(ctx, second, telemetry)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, stopSecond(ctx)) })

	forceMaxDeliveryAdvisory(t, ctx, tc.Client, "SHARED_DURABLE", "shared.durable")
	select {
	case <-telemetry.events:
	case <-ctx.Done():
		t.Fatal("shared durable did not deliver the MaxDeliver advisory")
	}

	// Give an accidental second independent consumer enough time to surface.
	select {
	case duplicate := <-telemetry.events:
		t.Fatalf("one occurrence was emitted twice across replicas: %+v", duplicate)
	case <-time.After(500 * time.Millisecond):
	}
	attempts, successes := telemetry.counts()
	assert.Equal(t, 1, attempts)
	assert.Equal(t, 1, successes)
}

func TestDuplicateLocalObserverRejectsWithoutStoppingIncumbent(t *testing.T) {
	tc, ctx := integrationClient(t)
	ensureFrameworkStreams(t, ctx, tc.Client)
	telemetry := newIntegrationTelemetry(false)
	stop, err := start(ctx, tc.Client, telemetry)
	require.NoError(t, err)
	duplicateStop, duplicateErr := start(ctx, tc.Client, telemetry)
	require.Nil(t, duplicateStop)
	require.Error(t, duplicateErr)

	want := forceMaxDeliveryAdvisory(t, ctx, tc.Client, "LOCAL_DUPLICATE", "local.duplicate")
	select {
	case got := <-telemetry.events:
		require.Equal(t, want.StreamSequence, got.StreamSequence,
			"duplicate rejection must leave the incumbent observer live")
	case <-ctx.Done():
		t.Fatal("incumbent observer stopped after duplicate rejection")
	}
	require.NoError(t, stop(ctx))
}

func TestCaptureStreamIsBoundedDiscardOldOnFreshServer(t *testing.T) {
	tc, ctx := integrationClient(t)
	ensureFrameworkStreams(t, ctx, tc.Client)

	stream, err := tc.Client.GetStream(ctx, captureStreamName)
	require.NoError(t, err)
	info, err := stream.Info(ctx)
	require.NoError(t, err)
	assert.Equal(t, jetstream.FileStorage, info.Config.Storage)
	assert.Equal(t, jetstream.LimitsPolicy, info.Config.Retention)
	assert.Equal(t, jetstream.DiscardOld, info.Config.Discard)
	assert.Equal(t, 7*24*time.Hour, info.Config.MaxAge)
	assert.Equal(t, int64(64*1024*1024), info.Config.MaxBytes)
}

func TestCentralProvisionerReconcilesCaptureStream(t *testing.T) {
	tc, ctx := integrationClient(t)
	js, err := tc.Client.JetStream()
	require.NoError(t, err)
	_, err = js.CreateStream(ctx, jetstream.StreamConfig{
		Name: captureStreamName, Subjects: []string{advisorySubjectPrefix + ".>"},
		Storage: jetstream.FileStorage, Retention: jetstream.LimitsPolicy,
		Discard: jetstream.DiscardNew, MaxAge: time.Hour, MaxBytes: 1024 * 1024, Replicas: 1,
	})
	require.NoError(t, err)

	ensureFrameworkStreams(t, ctx, tc.Client)
	stream, err := js.Stream(ctx, captureStreamName)
	require.NoError(t, err)
	info, err := stream.Info(ctx)
	require.NoError(t, err)
	assert.Equal(t, jetstream.DiscardOld, info.Config.Discard)
	assert.Equal(t, 7*24*time.Hour, info.Config.MaxAge)
	assert.Equal(t, int64(64*1024*1024), info.Config.MaxBytes)
}

// TestHeldObjectStoreHandleFailsAfterBackingStreamSeal is the forcing-function
// proof needed by the assembled E2E. A test-side administrator can seal the
// shipped bucket's backing stream after the component has acquired its handle;
// the next Put through that held handle fails deterministically without adding
// any production fault-injection setting.
func TestHeldObjectStoreHandleFailsAfterBackingStreamSeal(t *testing.T) {
	tc, ctx := integrationClient(t)
	js, err := tc.Client.JetStream()
	require.NoError(t, err)
	store, err := js.CreateObjectStore(ctx, jetstream.ObjectStoreConfig{Bucket: "E2E_SEAL_PROBE"})
	require.NoError(t, err)
	_, err = store.PutBytes(ctx, "before", []byte("ok"))
	require.NoError(t, err)

	backing, err := js.Stream(ctx, "OBJ_E2E_SEAL_PROBE")
	require.NoError(t, err)
	info, err := backing.Info(ctx)
	require.NoError(t, err)
	sealed := info.Config
	sealed.Sealed = true
	_, err = js.UpdateStream(ctx, sealed)
	require.NoError(t, err)

	_, err = store.PutBytes(ctx, "after", []byte("must fail"))
	require.Error(t, err)
}

// streamAssignmentBudget bounds how long a clustered node may lag the meta
// layer before the fixture treats "stream not found" as a real failure.
const streamAssignmentBudget = 5 * time.Second

// streamAssignmentRetryInterval spaces observations of the server's own state;
// it is not the synchronization mechanism, the observation is.
const streamAssignmentRetryInterval = 10 * time.Millisecond

// retryWhileStreamNotFound runs op until the node serving the request stops
// reporting the stream as absent.
//
// A clustered JetStream answers stream and consumer creation from the meta
// leader, while every other node applies that assignment from the meta Raft log
// asynchronously. A request issued immediately after creation therefore reaches
// a node that may still be behind, which answers 404/10059 for a stream that
// exists. Only the absent classification is retried; any other failure fails the
// test immediately so this never becomes a retry-until-green wrapper.
func retryWhileStreamNotFound[T any](
	ctx context.Context,
	t *testing.T,
	operation string,
	op func(context.Context) (T, error),
) T {
	t.Helper()
	waitCtx, cancel := context.WithTimeout(ctx, streamAssignmentBudget)
	defer cancel()
	retry := time.NewTicker(streamAssignmentRetryInterval)
	defer retry.Stop()
	for {
		value, err := op(waitCtx)
		if err == nil {
			return value
		}
		require.ErrorIs(t, err, jetstream.ErrStreamNotFound,
			"%s failed for a reason other than cluster metadata propagation", operation)
		select {
		case <-waitCtx.Done():
			t.Fatalf("%s still reported the stream absent after %s: %v", operation, streamAssignmentBudget, err)
		case <-retry.C:
		}
	}
}

// TestRetryWhileStreamNotFoundRetriesTheServersAbsentClassification pins the
// classification the retry is built on: a lagging node returns a JetStream API
// error carrying err_code 10059, never the package sentinel value, so the match
// has to be typed rather than by identity or message text.
func TestRetryWhileStreamNotFoundRetriesTheServersAbsentClassification(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), integrationTimeout)
	defer cancel()
	absent := &jetstream.APIError{
		Code: 404, ErrorCode: jetstream.JSErrCodeStreamNotFound, Description: "stream not found",
	}
	attempts := 0
	visible := retryWhileStreamNotFound(ctx, t, "probe", func(context.Context) (string, error) {
		attempts++
		if attempts < 3 {
			return "", absent
		}
		return "applied", nil
	})
	assert.Equal(t, "applied", visible)
	assert.Equal(t, 3, attempts, "the fixture observes the server again instead of accepting the first answer")
}

func forceMaxDeliveryAdvisory(
	t *testing.T,
	ctx context.Context,
	client *natsclient.Client,
	streamName string,
	subject string,
) advisory {
	t.Helper()
	js, err := client.JetStream()
	require.NoError(t, err)
	stream, err := js.CreateStream(ctx, jetstream.StreamConfig{
		Name: streamName, Subjects: []string{subject}, Storage: jetstream.MemoryStorage,
		Retention: jetstream.LimitsPolicy, Discard: jetstream.DiscardOld,
		MaxAge: time.Hour, MaxBytes: 1024 * 1024,
	})
	require.NoError(t, err)
	consumerName := "exhaust-once"
	consumer := retryWhileStreamNotFound(ctx, t, "create consumer "+consumerName+" on "+streamName,
		func(opCtx context.Context) (jetstream.Consumer, error) {
			return stream.CreateOrUpdateConsumer(opCtx, jetstream.ConsumerConfig{
				Durable: consumerName, FilterSubject: subject, AckPolicy: jetstream.AckExplicitPolicy,
				DeliverPolicy: jetstream.DeliverAllPolicy, MaxDeliver: 1, AckWait: 50 * time.Millisecond,
			})
		})
	consumeCtx, err := consumer.Consume(func(msg jetstream.Msg) { _ = msg.Nak() })
	require.NoError(t, err)
	t.Cleanup(consumeCtx.Stop)

	ack, err := js.Publish(ctx, subject, []byte("park-me"))
	require.NoError(t, err)
	require.NotZero(t, ack.Sequence)

	capture, err := js.Stream(ctx, captureStreamName)
	require.NoError(t, err)
	deadline := time.NewTicker(20 * time.Millisecond)
	defer deadline.Stop()
	for {
		info, infoErr := capture.Info(ctx)
		require.NoError(t, infoErr)
		if info.State.Msgs > 0 {
			return advisory{Stream: streamName, Consumer: consumerName, StreamSequence: ack.Sequence}
		}
		select {
		case <-ctx.Done():
			t.Fatalf("server did not publish a MaxDeliver advisory for %s/%s", streamName, consumerName)
		case <-deadline.C:
		}
	}
}
