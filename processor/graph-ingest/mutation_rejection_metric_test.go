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
	"github.com/c360studio/semstreams/pkg/errs"
)

// ADR-060: meteredMutation wraps every graph-mutation request handler and
// meters rejections by subject + reason. Under ADR-060, a hard rejection
// surfaces as (nil body, *errs.ClassifiedError) on the error channel —
// there is no longer an in-body Success/ErrorCode path for single-entity
// mutations. Partial-batch success is still a (body, nil) reply with
// FailedSubjects populated. The metric is process-global, so these tests
// use UNIQUE per-case subjects to keep their label series isolated.

func TestMeteredMutation_MetersRejectionByErrorCode(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	const subj = "test.mutation.metric.errorcode"

	// ADR-060: a hard rejection is (nil, *ClassifiedError); the handler
	// returns the typed error, not an in-body Success=false.
	wrapped := comp.meteredMutation(subj, func(_ context.Context, _ []byte) ([]byte, error) {
		return nil, errs.ClassifiedCode(errs.ErrorInvalid, graph.ErrorCodeEntityNotFound, errors.New("entity does not exist"))
	})

	counter := comp.mutationRejections.WithLabelValues(subj, graph.ErrorCodeEntityNotFound)
	before := testutil.ToFloat64(counter)

	_, err := wrapped(context.Background(), nil)
	// The handler error passes through unchanged — metering does not suppress it.
	require.Error(t, err, "a hard rejection must surface as a non-nil error")
	var ce *errs.ClassifiedError
	require.ErrorAs(t, err, &ce)
	assert.Equal(t, graph.ErrorCodeEntityNotFound, ce.Code)

	assert.InDelta(t, before+1, testutil.ToFloat64(counter), 0.0001,
		"a rejection must increment {subject, error_code} exactly once")
}

func TestMeteredMutation_SuccessDoesNotMeter(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	const subj = "test.mutation.metric.success"

	wrapped := comp.meteredMutation(subj, func(_ context.Context, _ []byte) ([]byte, error) {
		// ADR-060: any (body, nil) reply is a success body.
		data, _ := json.Marshal(graph.MutationResponse{Timestamp: 1})
		return data, nil
	})

	// A successful response must not increment ANY reason for this subject.
	before := testutil.ToFloat64(comp.mutationRejections.WithLabelValues(subj, "partial_batch")) +
		testutil.ToFloat64(comp.mutationRejections.WithLabelValues(subj, graph.ErrorCodeInternal))
	_, err := wrapped(context.Background(), nil)
	require.NoError(t, err)
	after := testutil.ToFloat64(comp.mutationRejections.WithLabelValues(subj, "partial_batch")) +
		testutil.ToFloat64(comp.mutationRejections.WithLabelValues(subj, graph.ErrorCodeInternal))
	assert.InDelta(t, before, after, 0.0001, "a success must not be metered as a rejection")
}

// TestMeteredMutation_EmptyCodeIsInternal verifies that a non-nil handler error
// with an empty ClassifiedError.Code (or a plain error) falls through to the
// "internal" label — the ADR-060 equivalent of the retired "unclassified" path
// (the old in-body ErrorCode="" route no longer exists).
func TestMeteredMutation_EmptyCodeIsInternal(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	const subj = "test.mutation.metric.emptycode"

	// A ClassifiedError with empty Code (edge case: caller omitted the code).
	wrapped := comp.meteredMutation(subj, func(_ context.Context, _ []byte) ([]byte, error) {
		return nil, errs.ClassifiedCode(errs.ErrorInvalid, "", errors.New("some error without code"))
	})

	counter := comp.mutationRejections.WithLabelValues(subj, graph.ErrorCodeInternal)
	before := testutil.ToFloat64(counter)
	_, err := wrapped(context.Background(), nil)
	require.Error(t, err)
	assert.InDelta(t, before+1, testutil.ToFloat64(counter), 0.0001,
		"a rejection with empty Code must fall through to reason='internal'")
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
