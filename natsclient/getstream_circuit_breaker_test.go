package natsclient

// Tests that GetStream existence probes do not trip the circuit breaker.
//
// Background (gh#248): GetStream previously called recordFailure() on ANY
// error, including the benign jetstream.ErrStreamNotFound.  An existence
// probe that gets "stream not found" is a successful probe — the answer is
// "not here, now" — not a transport failure.  Counting it as a failure can
// trip the circuit breaker on deployments that routinely probe for an absent
// stream: an optional-stream check, a diagnostic sweep, a caller deciding
// whether to provision.  (The original exemplar, agentrun's MilestoneSubscriber
// precondition, is gone: gh#1073 moved that decision onto the guarded consumer
// setup, because one probe's answer is not evidence of absence.  The exemption
// stays — it is what keeps a cheap probe cheap.)
//
// Fix: ErrStreamNotFound is now exempt from recordFailure(); all other errors
// still trip the breaker.

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeJetStream implements jetstream.JetStream for unit tests.
// Only Stream() is meaningful; all other methods panic if called.
type fakeJetStream struct {
	streamErr error // returned by Stream()
	kvErr     error // returned by KeyValue()
	// createStreamErr is returned by CreateStream(). Only the failure shapes are
	// modeled: a nil error would have to hand back a nil jetstream.Stream, which
	// is a lie no caller should have to tolerate from a fake.
	createStreamErr error
	// streamCalls counts Stream() observations so a test can assert how many
	// times a caller looked, not how long it took to give up looking.
	streamCalls atomic.Int64
	// createStreamCalls counts CreateStream() attempts.
	createStreamCalls atomic.Int64
}

// Stream is the only method exercised by GetStream.
func (f *fakeJetStream) Stream(_ context.Context, _ string) (jetstream.Stream, error) {
	f.streamCalls.Add(1)
	return nil, f.streamErr
}

// --- AccountInfo / Conn / Options ---

func (f *fakeJetStream) AccountInfo(_ context.Context) (*jetstream.AccountInfo, error) {
	panic("fakeJetStream: AccountInfo not implemented")
}

func (f *fakeJetStream) Conn() *nats.Conn {
	panic("fakeJetStream: Conn not implemented")
}

func (f *fakeJetStream) Options() jetstream.JetStreamOptions {
	panic("fakeJetStream: Options not implemented")
}

// --- Publisher ---

func (f *fakeJetStream) Publish(_ context.Context, _ string, _ []byte, _ ...jetstream.PublishOpt) (*jetstream.PubAck, error) {
	panic("fakeJetStream: Publish not implemented")
}

func (f *fakeJetStream) PublishMsg(_ context.Context, _ *nats.Msg, _ ...jetstream.PublishOpt) (*jetstream.PubAck, error) {
	panic("fakeJetStream: PublishMsg not implemented")
}

func (f *fakeJetStream) PublishAsync(_ string, _ []byte, _ ...jetstream.PublishOpt) (jetstream.PubAckFuture, error) {
	panic("fakeJetStream: PublishAsync not implemented")
}

func (f *fakeJetStream) PublishMsgAsync(_ *nats.Msg, _ ...jetstream.PublishOpt) (jetstream.PubAckFuture, error) {
	panic("fakeJetStream: PublishMsgAsync not implemented")
}

func (f *fakeJetStream) PublishAsyncPending() int {
	panic("fakeJetStream: PublishAsyncPending not implemented")
}

func (f *fakeJetStream) PublishAsyncComplete() <-chan struct{} {
	panic("fakeJetStream: PublishAsyncComplete not implemented")
}

func (f *fakeJetStream) CleanupPublisher() {
	panic("fakeJetStream: CleanupPublisher not implemented")
}

// --- StreamManager ---

func (f *fakeJetStream) CreateStream(_ context.Context, _ jetstream.StreamConfig) (jetstream.Stream, error) {
	if f.createStreamErr == nil {
		panic("fakeJetStream: CreateStream success is not modeled; set createStreamErr")
	}
	f.createStreamCalls.Add(1)
	return nil, f.createStreamErr
}

func (f *fakeJetStream) UpdateStream(_ context.Context, _ jetstream.StreamConfig) (jetstream.Stream, error) {
	panic("fakeJetStream: UpdateStream not implemented")
}

func (f *fakeJetStream) CreateOrUpdateStream(_ context.Context, _ jetstream.StreamConfig) (jetstream.Stream, error) {
	panic("fakeJetStream: CreateOrUpdateStream not implemented")
}

func (f *fakeJetStream) StreamNameBySubject(_ context.Context, _ string) (string, error) {
	panic("fakeJetStream: StreamNameBySubject not implemented")
}

func (f *fakeJetStream) DeleteStream(_ context.Context, _ string) error {
	panic("fakeJetStream: DeleteStream not implemented")
}

func (f *fakeJetStream) ListStreams(_ context.Context, _ ...jetstream.StreamListOpt) jetstream.StreamInfoLister {
	panic("fakeJetStream: ListStreams not implemented")
}

func (f *fakeJetStream) StreamNames(_ context.Context, _ ...jetstream.StreamListOpt) jetstream.StreamNameLister {
	panic("fakeJetStream: StreamNames not implemented")
}

// --- StreamConsumerManager ---

func (f *fakeJetStream) CreateOrUpdateConsumer(_ context.Context, _ string, _ jetstream.ConsumerConfig) (jetstream.Consumer, error) {
	panic("fakeJetStream: CreateOrUpdateConsumer not implemented")
}

func (f *fakeJetStream) CreateConsumer(_ context.Context, _ string, _ jetstream.ConsumerConfig) (jetstream.Consumer, error) {
	panic("fakeJetStream: CreateConsumer not implemented")
}

func (f *fakeJetStream) UpdateConsumer(_ context.Context, _ string, _ jetstream.ConsumerConfig) (jetstream.Consumer, error) {
	panic("fakeJetStream: UpdateConsumer not implemented")
}

func (f *fakeJetStream) OrderedConsumer(_ context.Context, _ string, _ jetstream.OrderedConsumerConfig) (jetstream.Consumer, error) {
	panic("fakeJetStream: OrderedConsumer not implemented")
}

func (f *fakeJetStream) Consumer(_ context.Context, _ string, _ string) (jetstream.Consumer, error) {
	panic("fakeJetStream: Consumer not implemented")
}

func (f *fakeJetStream) DeleteConsumer(_ context.Context, _ string, _ string) error {
	panic("fakeJetStream: DeleteConsumer not implemented")
}

func (f *fakeJetStream) PauseConsumer(_ context.Context, _ string, _ string, _ time.Time) (*jetstream.ConsumerPauseResponse, error) {
	panic("fakeJetStream: PauseConsumer not implemented")
}

func (f *fakeJetStream) ResumeConsumer(_ context.Context, _ string, _ string) (*jetstream.ConsumerPauseResponse, error) {
	panic("fakeJetStream: ResumeConsumer not implemented")
}

func (f *fakeJetStream) CreateOrUpdatePushConsumer(_ context.Context, _ string, _ jetstream.ConsumerConfig) (jetstream.PushConsumer, error) {
	panic("fakeJetStream: CreateOrUpdatePushConsumer not implemented")
}

func (f *fakeJetStream) CreatePushConsumer(_ context.Context, _ string, _ jetstream.ConsumerConfig) (jetstream.PushConsumer, error) {
	panic("fakeJetStream: CreatePushConsumer not implemented")
}

func (f *fakeJetStream) UpdatePushConsumer(_ context.Context, _ string, _ jetstream.ConsumerConfig) (jetstream.PushConsumer, error) {
	panic("fakeJetStream: UpdatePushConsumer not implemented")
}

func (f *fakeJetStream) PushConsumer(_ context.Context, _ string, _ string) (jetstream.PushConsumer, error) {
	panic("fakeJetStream: PushConsumer not implemented")
}

// --- KeyValueManager ---

// KeyValue is the only method exercised by GetKeyValueBucket.
func (f *fakeJetStream) KeyValue(_ context.Context, _ string) (jetstream.KeyValue, error) {
	return nil, f.kvErr
}

func (f *fakeJetStream) CreateKeyValue(_ context.Context, _ jetstream.KeyValueConfig) (jetstream.KeyValue, error) {
	panic("fakeJetStream: CreateKeyValue not implemented")
}

func (f *fakeJetStream) UpdateKeyValue(_ context.Context, _ jetstream.KeyValueConfig) (jetstream.KeyValue, error) {
	panic("fakeJetStream: UpdateKeyValue not implemented")
}

func (f *fakeJetStream) CreateOrUpdateKeyValue(_ context.Context, _ jetstream.KeyValueConfig) (jetstream.KeyValue, error) {
	panic("fakeJetStream: CreateOrUpdateKeyValue not implemented")
}

func (f *fakeJetStream) DeleteKeyValue(_ context.Context, _ string) error {
	panic("fakeJetStream: DeleteKeyValue not implemented")
}

func (f *fakeJetStream) KeyValueStoreNames(_ context.Context) jetstream.KeyValueNamesLister {
	panic("fakeJetStream: KeyValueStoreNames not implemented")
}

func (f *fakeJetStream) KeyValueStores(_ context.Context) jetstream.KeyValueLister {
	panic("fakeJetStream: KeyValueStores not implemented")
}

// ResetConsumer was added to jetstream.JetStream in nats.go v1.49+ (present at
// v1.52.0). This fake panics like every other unexercised method: the circuit
// breaker under test never resets a consumer, so a call here means the test
// drifted into a path it does not model, and a silent zero-value return would
// hide that.
func (f *fakeJetStream) ResetConsumer(_ context.Context, _, _ string) (*jetstream.ConsumerResetResponse, error) {
	panic("fakeJetStream: ResetConsumer not implemented")
}

func (f *fakeJetStream) ResetConsumerToSequence(
	_ context.Context, _, _ string, _ uint64,
) (*jetstream.ConsumerResetResponse, error) {
	panic("fakeJetStream: ResetConsumerToSequence not implemented")
}

// --- ObjectStoreManager ---

func (f *fakeJetStream) ObjectStore(_ context.Context, _ string) (jetstream.ObjectStore, error) {
	panic("fakeJetStream: ObjectStore not implemented")
}

func (f *fakeJetStream) CreateObjectStore(_ context.Context, _ jetstream.ObjectStoreConfig) (jetstream.ObjectStore, error) {
	panic("fakeJetStream: CreateObjectStore not implemented")
}

func (f *fakeJetStream) UpdateObjectStore(_ context.Context, _ jetstream.ObjectStoreConfig) (jetstream.ObjectStore, error) {
	panic("fakeJetStream: UpdateObjectStore not implemented")
}

func (f *fakeJetStream) CreateOrUpdateObjectStore(_ context.Context, _ jetstream.ObjectStoreConfig) (jetstream.ObjectStore, error) {
	panic("fakeJetStream: CreateOrUpdateObjectStore not implemented")
}

func (f *fakeJetStream) DeleteObjectStore(_ context.Context, _ string) error {
	panic("fakeJetStream: DeleteObjectStore not implemented")
}

func (f *fakeJetStream) ObjectStoreNames(_ context.Context) jetstream.ObjectStoreNamesLister {
	panic("fakeJetStream: ObjectStoreNames not implemented")
}

func (f *fakeJetStream) ObjectStores(_ context.Context) jetstream.ObjectStoresLister {
	panic("fakeJetStream: ObjectStores not implemented")
}

// compile-time interface assertion
var _ jetstream.JetStream = (*fakeJetStream)(nil)

// newConnectedClientWithFakeJS creates a Client in StatusConnected state with
// the provided fakeJetStream injected, bypassing the NATS dial.
func newConnectedClientWithFakeJS(t *testing.T, fakeJS jetstream.JetStream) *Client {
	t.Helper()
	client, err := NewClient("nats://localhost:4222")
	require.NoError(t, err)

	// Inject the fake JetStream and set connected status.
	// Both fields are unexported but we're in package natsclient (same package).
	client.mu.Lock()
	client.js = fakeJS
	client.mu.Unlock()
	client.setStatus(StatusConnected)

	return client
}

// TestGetStream_NotFound_DoesNotTripCircuitBreaker verifies that probing for
// an absent stream (ErrStreamNotFound) is treated as a successful probe, not
// a failure.  The circuit breaker failure counter must not advance.
func TestGetStream_NotFound_DoesNotTripCircuitBreaker(t *testing.T) {
	client := newConnectedClientWithFakeJS(t, &fakeJetStream{streamErr: jetstream.ErrStreamNotFound})

	failuresBefore := client.Failures()

	ctx := context.Background()
	_, err := client.GetStream(ctx, "ABSENT_STREAM")

	// The sentinel must be propagated to the caller so it can branch on it.
	require.Error(t, err)
	assert.True(t, errors.Is(err, jetstream.ErrStreamNotFound),
		"expected ErrStreamNotFound, got: %v", err)

	// No failure must be recorded — the probe answered "stream not found",
	// which is a valid, non-failure response.
	assert.Equal(t, failuresBefore, client.Failures(),
		"GetStream(ErrStreamNotFound) must NOT increment the failure counter")

	// The circuit must remain closed.
	assert.NotEqual(t, StatusCircuitOpen, client.Status(),
		"circuit must stay closed on a stream-not-found probe")
}

// TestGetStream_TransportError_StillTripsCircuitBreaker verifies that genuine
// errors (e.g. context cancellation, no-responders) still advance the failure
// counter, preserving circuit-breaker coverage.
func TestGetStream_TransportError_StillTripsCircuitBreaker(t *testing.T) {
	transportErr := errors.New("nats: no responders")
	client := newConnectedClientWithFakeJS(t, &fakeJetStream{streamErr: transportErr})

	failuresBefore := client.Failures()

	ctx := context.Background()
	_, err := client.GetStream(ctx, "ANY_STREAM")

	require.Error(t, err)
	assert.False(t, errors.Is(err, jetstream.ErrStreamNotFound),
		"transport error must not be confused with ErrStreamNotFound")

	assert.Greater(t, client.Failures(), failuresBefore,
		"a genuine transport error must increment the failure counter")
}

// TestGetStream_RepeatedNotFound_CircuitBreakersDoNotOpen confirms that
// probing for an absent stream many times — enough to exceed the circuit
// threshold if counted — does not open the circuit.  This is the direct
// regression guard for gh#248: a deployment that routinely probes for an
// absent stream (like agentrun MilestoneSubscriber on a graph-only deploy)
// must continue to operate normally.
func TestGetStream_RepeatedNotFound_CircuitBreakersDoNotOpen(t *testing.T) {
	client := newConnectedClientWithFakeJS(t, &fakeJetStream{streamErr: jetstream.ErrStreamNotFound})

	ctx := context.Background()
	// circuitThreshold is 15; probe well beyond it.
	for i := 0; i < 20; i++ {
		_, _ = client.GetStream(ctx, "ABSENT_STREAM")
	}

	assert.NotEqual(t, StatusCircuitOpen, client.Status(),
		"repeated ErrStreamNotFound probes must never open the circuit breaker")
	assert.Equal(t, int32(0), client.Failures(),
		"failure counter must remain zero after repeated not-found probes")
}
