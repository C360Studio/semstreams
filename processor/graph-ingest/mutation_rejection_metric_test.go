package graphingest

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/graph"
)

// ADR-055 §3: meteredMutation wraps every graph-mutation request handler and
// meters rejections (success=false) by subject + reason (the bounded
// MutationResponse ErrorCode), without ever altering the handler's verdict. The
// metric is process-global, so these tests use UNIQUE per-case subjects to keep
// their label series isolated.

func rejectResponse(t *testing.T, code, msg string) []byte {
	t.Helper()
	data, err := json.Marshal(graph.MutationResponse{Success: false, Error: msg, ErrorCode: code})
	require.NoError(t, err)
	return data
}

func TestMeteredMutation_MetersRejectionByErrorCode(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	const subj = "test.mutation.metric.errorcode"

	wrapped := comp.meteredMutation(subj, func(_ context.Context, _ []byte) ([]byte, error) {
		return rejectResponse(t, graph.ErrorCodeEntityNotFound, "entity does not exist"), nil
	})

	counter := comp.mutationRejections.WithLabelValues(subj, graph.ErrorCodeEntityNotFound)
	before := testutil.ToFloat64(counter)

	resp, err := wrapped(context.Background(), nil)
	require.NoError(t, err, "metering must not introduce an error")

	// Pass-through: the response is returned unchanged.
	var r graph.MutationResponse
	require.NoError(t, json.Unmarshal(resp, &r))
	assert.False(t, r.Success)
	assert.Equal(t, graph.ErrorCodeEntityNotFound, r.ErrorCode)

	assert.InDelta(t, before+1, testutil.ToFloat64(counter), 0.0001,
		"a rejection must increment {subject, error_code} exactly once")
}

func TestMeteredMutation_SuccessDoesNotMeter(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	const subj = "test.mutation.metric.success"

	wrapped := comp.meteredMutation(subj, func(_ context.Context, _ []byte) ([]byte, error) {
		data, _ := json.Marshal(graph.MutationResponse{Success: true})
		return data, nil
	})

	// A successful response must not increment ANY reason for this subject.
	before := testutil.ToFloat64(comp.mutationRejections.WithLabelValues(subj, "unclassified")) +
		testutil.ToFloat64(comp.mutationRejections.WithLabelValues(subj, graph.ErrorCodeInternal))
	_, err := wrapped(context.Background(), nil)
	require.NoError(t, err)
	after := testutil.ToFloat64(comp.mutationRejections.WithLabelValues(subj, "unclassified")) +
		testutil.ToFloat64(comp.mutationRejections.WithLabelValues(subj, graph.ErrorCodeInternal))
	assert.InDelta(t, before, after, 0.0001, "a success must not be metered as a rejection")
}

func TestMeteredMutation_EmptyErrorCodeIsUnclassified(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	const subj = "test.mutation.metric.unclassified"

	// A legacy rejection with no ErrorCode (e.g. today's triple.add error path).
	wrapped := comp.meteredMutation(subj, func(_ context.Context, _ []byte) ([]byte, error) {
		return rejectResponse(t, "", "some legacy error"), nil
	})

	counter := comp.mutationRejections.WithLabelValues(subj, "unclassified")
	before := testutil.ToFloat64(counter)
	_, err := wrapped(context.Background(), nil)
	require.NoError(t, err)
	assert.InDelta(t, before+1, testutil.ToFloat64(counter), 0.0001,
		"a rejection with no ErrorCode must be labelled 'unclassified'")
}

func TestMeteredMutation_HandlerErrorMeteredAsInternal(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	const subj = "test.mutation.metric.internal"

	sentinel := errors.New("handler blew up")
	wrapped := comp.meteredMutation(subj, func(_ context.Context, _ []byte) ([]byte, error) {
		return nil, sentinel
	})

	counter := comp.mutationRejections.WithLabelValues(subj, graph.ErrorCodeInternal)
	before := testutil.ToFloat64(counter)
	_, err := wrapped(context.Background(), nil)
	require.ErrorIs(t, err, sentinel, "the handler error must pass through unchanged")
	assert.InDelta(t, before+1, testutil.ToFloat64(counter), 0.0001,
		"a non-nil handler error must be metered as reason=internal")
}
