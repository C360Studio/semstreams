// Package client provides test utilities for E2E NATS validation.
//
// This file pins the composite-key reconstruction the sharded INCOMING_INDEX
// e2e reader depends on (gh#474). The reader replicates the on-disk
// key format written by processor/graph-index rather than importing it (the
// production builders are unexported), so a mechanical round-trip test guards the
// split logic — in particular the dotted-predicate case, where a wrong SplitN
// bound would silently truncate multi-token predicates like "hierarchy.type.member".
package client

import (
	"testing"

	"github.com/c360studio/semstreams/graph"
	"github.com/stretchr/testify/assert"
)

// buildIncomingKey mirrors processor/graph-index/incoming_index.go incomingIndexKey:
// key = targetID + "." + sourceID + "." + hex(predicate) (gh#474 P1a). Kept in sync by
// the live e2e HARD-FAIL gate and graph/query/incoming_shard_integration_test.go.
func buildIncomingKey(targetID, sourceID, predicate string) string {
	return targetID + "." + sourceID + "." + graph.EncodePredicateToken(predicate)
}

func TestIncomingEntryFromCompositeKey(t *testing.T) {
	const target = "acme.ops.robotics.gcs.drone.001"
	const source = "acme.ops.robotics.gcs.sensor.042"

	t.Run("dotted predicate round-trips intact", func(t *testing.T) {
		key := buildIncomingKey(target, source, "hierarchy.type.member")
		got, ok := incomingEntryFromCompositeKey(key, target)
		assert.True(t, ok)
		assert.Equal(t, source, got.FromEntityID)
		assert.Equal(t, "hierarchy.type.member", got.Predicate)
	})

	t.Run("single-token predicate", func(t *testing.T) {
		key := buildIncomingKey(target, source, "contains")
		got, ok := incomingEntryFromCompositeKey(key, target)
		assert.True(t, ok)
		assert.Equal(t, source, got.FromEntityID)
		assert.Equal(t, "contains", got.Predicate)
	})

	t.Run("KV-unsafe predicate survives via hex (P1a)", func(t *testing.T) {
		// graph-ingest accepts predicates with spaces/wildcards/unicode; a raw key token
		// would make the Put fail. Hex keeps it KV-safe AND reversible (gh#474 P1a).
		const weird = "has space/and*wild>chars☃"
		key := buildIncomingKey(target, source, weird)
		got, ok := incomingEntryFromCompositeKey(key, target)
		assert.True(t, ok)
		assert.Equal(t, source, got.FromEntityID)
		assert.Equal(t, weird, got.Predicate)
	})

	t.Run("wrong prefix rejected", func(t *testing.T) {
		key := buildIncomingKey("acme.ops.robotics.gcs.drone.999", source, "contains")
		_, ok := incomingEntryFromCompositeKey(key, target)
		assert.False(t, ok)
	})

	t.Run("too few tokens rejected", func(t *testing.T) {
		// target prefix present but source is not a full 6-token entity ID.
		_, ok := incomingEntryFromCompositeKey(target+".partial.source.contains", target)
		assert.False(t, ok)
	})

	t.Run("empty predicate rejected", func(t *testing.T) {
		key := buildIncomingKey(target, source, "") // trailing dot
		_, ok := incomingEntryFromCompositeKey(key, target)
		assert.False(t, ok)
	})
}
