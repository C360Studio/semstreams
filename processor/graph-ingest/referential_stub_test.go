package graphingest

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/vocabulary"
)

func stringObject(es *graph.EntityState, predicate string) string {
	for _, t := range es.Triples {
		if t.Predicate == predicate {
			if s, ok := t.Object.(string); ok {
				return s
			}
		}
	}
	return ""
}

// ADR-056 Decision-4 lane-ii fold: the referential-integrity stub is now a
// first-class ENVELOPE-BEARING artifact — it carries a valid stubMessageType (so
// "no envelope-less ownerless births" is literally true) + records the
// referencing producer in core.identity.stub_owner — while staying PROFILE-LESS.
func TestReferentialStub_EnvelopeBearing(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	const target = "c360.platform.test.sys.child.001"
	srcType := message.Type{Domain: "test", Category: "fixture", Version: "v1"}

	require.NoError(t, comp.ensureReferencedEntityExists(context.Background(), target, "c360.platform.test.sys.parent.001", srcType))

	es := storedEntity(t, comp, target)
	assert.True(t, es.MessageType.IsValid(), "stub must carry a valid MessageType (envelope-bearing)")
	assert.Equal(t, stubMessageType, es.MessageType)
	assert.True(t, hasPredicate(es, predStubMarker), "core.identity.stub marker retained")
	assert.True(t, hasPredicate(es, predStubReferencedBy))
	assert.Equal(t, srcType.Key(), stringObject(es, predStubOwner),
		"stub_owner records the reachable referencing-producer MessageType")
	assert.False(t, hasPredicate(es, vocabulary.EntityIndexingProfile),
		"stub MUST stay profile-less so the real merge is detected as the true birth")
}

// An untyped source (a gateway that does not stamp Entity.MessageType, e.g.
// cs-api today) attributes the stub to the framework referential producer rather
// than fabricating an invalid producer key.
func TestReferentialStub_UntypedSourceAttributesToFramework(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	const target = "c360.platform.test.sys.child.002"

	require.NoError(t, comp.ensureReferencedEntityExists(context.Background(), target, "c360.platform.test.sys.parent.001", message.Type{}))

	es := storedEntity(t, comp, target)
	assert.Equal(t, stubReferentialSource, stringObject(es, predStubOwner),
		"untyped source → framework referential producer, not an invalid key")
}

// THE no-regression guard (the architect's flagged risk): promoting the stub to
// envelope-bearing must NOT break MergeEntity's "real producer merging into a
// profile-less stub is the true birth" detection — it keys on profile-absence,
// not MessageType. The real merge must overwrite MessageType, increment Version,
// and stamp the indexing profile.
func TestReferentialStub_RealMergeStillDetectsTrueBirth(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	ctx := context.Background()
	const target = "c360.platform.test.sys.child.003"

	require.NoError(t, comp.ensureReferencedEntityExists(ctx, target, "c360.platform.test.sys.parent.001",
		message.Type{Domain: "test", Category: "fixture", Version: "v1"}))
	stub := storedEntity(t, comp, target)
	require.Equal(t, uint64(1), stub.Version)
	require.False(t, hasPredicate(stub, vocabulary.EntityIndexingProfile))

	// The real producer is born by merging onto the stub.
	realType := message.Type{Domain: "test", Category: "real", Version: "v1"}
	require.NoError(t, comp.MergeEntity(ctx, &graph.EntityState{
		ID: target, MessageType: realType, Version: 1,
		Triples: []message.Triple{{Subject: target, Predicate: "entity.identity.label", Object: "Born", Timestamp: time.Now(), Confidence: 1.0}},
	}))

	born := storedEntity(t, comp, target)
	assert.Equal(t, realType, born.MessageType, "real merge overwrites the stub's MessageType")
	assert.Greater(t, born.Version, uint64(1), "real merge increments Version past the stub's 1")
	assert.True(t, hasPredicate(born, "entity.identity.label"))
	assert.True(t, hasPredicate(born, vocabulary.EntityIndexingProfile),
		"merging a real producer onto the profile-less stub is the true birth → profile stamped")
}
