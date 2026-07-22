package graphembedding

import (
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph/embedding"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

// TestCurrentFailedMap_TracksReasonsAndGauge is the D2/D7 statement (#613): the
// current-failed map is a live count net of recovery — a Failed outcome adds (with its
// bounded reason and earliest first-failure time), every other terminal outcome removes
// — and the per-registry `failed` gauge mirrors len(map) on every transition. It uses a
// fresh, UNREGISTERED gauge so the assertion is isolated from any shared registry.
func TestCurrentFailedMap_TracksReasonsAndGauge(t *testing.T) {
	c := &Component{
		failed:      make(map[string]failureInfo),
		failedGauge: newEmbeddingFailedGauge(),
	}

	c.applyTerminalOutcome("e1", 1, embedding.OutcomeFailed, "connection_refused")
	c.applyTerminalOutcome("e2", 1, embedding.OutcomeFailed, "content_error")
	c.applyTerminalOutcome("e3", 1, embedding.OutcomeFailed, "connection_refused")

	count, reasons, firstAt := c.failedSnapshot()
	require.Equal(t, uint64(3), count)
	require.Equal(t, uint64(2), reasons["connection_refused"])
	require.Equal(t, uint64(1), reasons["content_error"])
	require.False(t, firstAt.IsZero(), "first_failure_at must be set while failures exist")
	require.Equal(t, 3.0, testutil.ToFloat64(c.failedGauge), "gauge must reflect len(map)")

	// A generated outcome at the same-or-newer revision clears one; gauge drops, histogram updates.
	c.applyTerminalOutcome("e1", 1, embedding.OutcomeGenerated, "")
	count, reasons, _ = c.failedSnapshot()
	require.Equal(t, uint64(2), count)
	require.Equal(t, uint64(1), reasons["connection_refused"])
	require.Equal(t, 2.0, testutil.ToFloat64(c.failedGauge))

	// Skipped (no-text) and Deleted (tombstone) also clear; back to zero → empty snapshot
	// (the envelope omits the fields entirely).
	c.applyTerminalOutcome("e2", 1, embedding.OutcomeSkipped, "")
	c.applyTerminalOutcome("e3", 1, embedding.OutcomeDeleted, "")
	count, reasons, firstAt = c.failedSnapshot()
	require.Equal(t, uint64(0), count)
	require.Nil(t, reasons, "an empty map yields a nil histogram so the envelope omits failed_reasons")
	require.True(t, firstAt.IsZero(), "no failures → no first_failure_at")
	require.Equal(t, 0.0, testutil.ToFloat64(c.failedGauge))
}

// TestCurrentFailedMap_ReFailurePreservesFirstFailureTime proves a re-failure updates the
// bounded reason to the latest but preserves the EARLIEST first-failure time, so
// first_failure_at reports when the entity first went bad, not the latest retry (#613).
func TestCurrentFailedMap_ReFailurePreservesFirstFailureTime(t *testing.T) {
	c := &Component{failed: make(map[string]failureInfo), failedGauge: newEmbeddingFailedGauge()}

	// Seed a known earliest time directly (single-goroutine test — no lock needed).
	earliest := time.Now().Add(-time.Hour)
	c.failed["ent"] = failureInfo{reason: "timeout", at: earliest, rev: 1}

	c.applyTerminalOutcome("ent", 2, embedding.OutcomeFailed, "connection_refused")

	require.Equal(t, earliest, c.failed["ent"].at, "re-failure must preserve the earliest first-failure time")
	require.Equal(t, "connection_refused", c.failed["ent"].reason, "re-failure must update the reason to the latest")
}
