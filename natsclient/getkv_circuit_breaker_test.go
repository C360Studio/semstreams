package natsclient

// Tests that GetKeyValueBucket existence probes do not trip the circuit breaker.
//
// Background: GetKeyValueBucket called recordFailure() on ANY error from
// js.KeyValue, including the benign jetstream.ErrBucketNotFound. This is the KV
// twin of the gh#248 GetStream regression, which was fixed one function away in
// the same file — an existence probe answered "bucket not found" is a successful
// probe, not a transport failure.
//
// It matters because several callers poll for a legitimately absent bucket:
// graph-query's resource.Watcher rechecks COMMUNITY_INDEX every 60s on any
// deployment without community detection, graph/readiness's watcher rebinds on
// every retry, and WaitForBucket polls at 500ms by design. At circuitThreshold
// (15) those reach it unaided, and the breaker is shared process-wide, so
// unrelated NATS work starts failing with ErrCircuitOpen.
//
// Surfaced by review of PR #624, which made the message-logger KV endpoint
// lookup-only and so newly exposed the not-found path to operator input.

import (
	"context"
	"errors"
	"testing"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetKeyValueBucket_NotFound_DoesNotTripCircuitBreaker(t *testing.T) {
	client := newConnectedClientWithFakeJS(t, &fakeJetStream{kvErr: jetstream.ErrBucketNotFound})

	failuresBefore := client.Failures()

	_, err := client.GetKeyValueBucket(context.Background(), "ABSENT_BUCKET")

	// The sentinel must reach the caller so it can branch on it — the
	// message-logger endpoint returns 404 off exactly this.
	require.Error(t, err)
	assert.True(t, errors.Is(err, jetstream.ErrBucketNotFound),
		"expected ErrBucketNotFound, got: %v", err)

	assert.Equal(t, failuresBefore, client.Failures(),
		"GetKeyValueBucket(ErrBucketNotFound) must NOT increment the failure counter")
	assert.NotEqual(t, StatusCircuitOpen, client.Status(),
		"circuit must stay closed on a bucket-not-found probe")
}

// Genuine faults must still move the breaker; the exemption is for the sentinel
// only, not for every error from this call.
func TestGetKeyValueBucket_TransportError_StillTripsCircuitBreaker(t *testing.T) {
	transportErr := errors.New("nats: no responders")
	client := newConnectedClientWithFakeJS(t, &fakeJetStream{kvErr: transportErr})

	failuresBefore := client.Failures()

	_, err := client.GetKeyValueBucket(context.Background(), "SOME_BUCKET")
	require.Error(t, err)

	assert.Greater(t, client.Failures(), failuresBefore,
		"a transport error from GetKeyValueBucket must still record a failure")
}

// The load-bearing case: a poll loop against an absent bucket must never open
// the shared circuit, however long it runs. circuitThreshold is 15, so 20
// probes would previously have opened it with five to spare.
func TestGetKeyValueBucket_RepeatedNotFound_CircuitDoesNotOpen(t *testing.T) {
	client := newConnectedClientWithFakeJS(t, &fakeJetStream{kvErr: jetstream.ErrBucketNotFound})

	const probes = 20
	require.Greater(t, int32(probes), client.circuitThreshold,
		"this test is only meaningful if it exceeds the circuit threshold")

	for i := 0; i < probes; i++ {
		_, err := client.GetKeyValueBucket(context.Background(), "ABSENT_BUCKET")
		require.Error(t, err)
		require.True(t, errors.Is(err, jetstream.ErrBucketNotFound),
			"probe %d returned %v, want ErrBucketNotFound", i, err)
	}

	assert.Zero(t, client.Failures(),
		"%d absent-bucket probes must record no failures", probes)
	assert.NotEqual(t, StatusCircuitOpen, client.Status(),
		"circuit must remain closed after %d absent-bucket probes", probes)
}
