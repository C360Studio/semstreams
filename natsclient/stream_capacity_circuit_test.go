package natsclient

import (
	"errors"
	"fmt"
	"testing"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

func TestIsCircuitNeutralStreamCapacityError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "maximum bytes", err: capacityAPIError("maximum bytes exceeded"), want: true},
		{name: "maximum messages", err: capacityAPIError("maximum messages exceeded"), want: true},
		{name: "maximum messages per subject", err: capacityAPIError("maximum messages per subject exceeded"), want: true},
		{name: "wrapped capacity", err: fmt.Errorf("outer: %w", capacityAPIError("maximum bytes exceeded")), want: true},
		{name: "untyped capacity text", err: errors.New("maximum bytes exceeded")},
		{name: "10077 message too large", err: capacityAPIError("message size exceeds max bytes")},
		{name: "unknown 10077", err: capacityAPIError("some future limit")},
		{name: "10054", err: &jetstream.APIError{Code: 503, ErrorCode: 10054, Description: "cluster not available"}},
		{name: "10002", err: &jetstream.APIError{Code: 500, ErrorCode: 10002, Description: "account resources exceeded"}},
		{name: "10023", err: &jetstream.APIError{Code: 503, ErrorCode: 10023, Description: "insufficient resources"}},
		{name: "10028", err: &jetstream.APIError{Code: 503, ErrorCode: 10028, Description: "insufficient storage resources"}},
		{name: "10047", err: &jetstream.APIError{Code: 503, ErrorCode: 10047, Description: "insufficient memory resources"}},
		{name: "max payload", err: nats.ErrMaxPayload},
		{name: "generic", err: errors.New("transport failed")},
		{name: "nil", err: nil},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, test.want, isCircuitNeutralStreamCapacityError(test.err))
		})
	}
}

func TestAsyncPublishErrHandlerCapacityNackIsCircuitNeutral(t *testing.T) {
	t.Parallel()

	client, err := NewClient("nats://localhost:4222")
	require.NoError(t, err)
	client.setStatus(StatusConnected)
	for range 14 {
		client.recordFailure()
	}
	require.Equal(t, int32(14), client.Failures())

	client.asyncPublishErrHandler(nil, &nats.Msg{Subject: "full.stream"},
		fmt.Errorf("future failed: %w", capacityAPIError("maximum messages exceeded")))

	require.Equal(t, int32(14), client.Failures())
	require.Equal(t, int32(14), client.circuitFailures.Load())
	require.Equal(t, StatusConnected, client.Status())
}

func TestTypedNilAPIErrorIsNotNeutralAndDoesNotPanic(t *testing.T) {
	t.Parallel()

	var typedNil *jetstream.APIError
	var err error = typedNil
	require.NotPanics(t, func() {
		require.False(t, isCircuitNeutralStreamCapacityError(err))
	})

	client, clientErr := NewClient("nats://localhost:4222")
	require.NoError(t, clientErr)
	client.setStatus(StatusConnected)
	require.NotPanics(t, func() { client.recordStreamPublishFailure(err) })
	require.Equal(t, int32(1), client.Failures())
	require.Equal(t, int32(1), client.circuitFailures.Load())
	require.Equal(t, StatusConnected, client.Status())
}

func capacityAPIError(description string) *jetstream.APIError {
	return &jetstream.APIError{Code: 503, ErrorCode: 10077, Description: description}
}
