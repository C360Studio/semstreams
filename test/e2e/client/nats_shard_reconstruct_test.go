// Package client provides test utilities for E2E NATS validation.
//
// This file pins the composite-key reconstruction the sharded INCOMING_INDEX and
// CONTEXT_INDEX e2e readers depend on (gh#474). The readers replicate the on-disk
// key format written by processor/graph-index rather than importing it (the
// production builders are unexported), so a mechanical round-trip test guards the
// split logic — in particular the dotted-predicate case, where a wrong SplitN
// bound would silently truncate multi-token predicates like "hierarchy.type.member".
package client

import (
	"crypto/sha256"
	"encoding/hex"
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

// buildContextKey mirrors processor/graph-index/context_index.go contextIndexKey
// (gh#474 P1f entity-prefix): key = entityID + "." + hex(sha256(context)) + "." + hex(predicate).
func buildContextKey(contextValue, entityID, predicate string) string {
	sum := sha256.Sum256([]byte(contextValue))
	return entityID + "." + hex.EncodeToString(sum[:]) + "." + graph.EncodePredicateToken(predicate)
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

func TestContextEntryFromCompositeKey(t *testing.T) {
	const entity = "acme.ops.robotics.gcs.drone.001"

	t.Run("dotted predicate round-trips intact", func(t *testing.T) {
		key := buildContextKey("inference.hierarchy", entity, "hierarchy.type.member")
		gotEntity, gotPred, ok := contextEntryFromCompositeKey(key)
		assert.True(t, ok)
		assert.Equal(t, entity, gotEntity)
		assert.Equal(t, "hierarchy.type.member", gotPred)
	})

	t.Run("nested context value does not affect key split", func(t *testing.T) {
		// The context value is hashed, so its own dots never reach the key.
		key := buildContextKey("inference.hierarchy.deep", entity, "contains")
		gotEntity, gotPred, ok := contextEntryFromCompositeKey(key)
		assert.True(t, ok)
		assert.Equal(t, entity, gotEntity)
		assert.Equal(t, "contains", gotPred)
	})

	t.Run("too few tokens rejected", func(t *testing.T) {
		_, _, ok := contextEntryFromCompositeKey("deadbeef.acme.ops.contains")
		assert.False(t, ok)
	})

	t.Run("empty predicate rejected", func(t *testing.T) {
		key := buildContextKey("inference.hierarchy", entity, "") // trailing dot
		_, _, ok := contextEntryFromCompositeKey(key)
		assert.False(t, ok)
	})
}
