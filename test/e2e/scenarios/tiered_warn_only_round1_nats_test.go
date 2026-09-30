package scenarios

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/test/e2e/client"
)

// #1426 review round 1: the KV-seam arms the class sweep found, driven through
// the real NATSValidationClient against an in-process JetStream server (ephemeral
// port) holding the exact key layouts the readers decode.

type kvFixture struct {
	t  *testing.T
	js jetstream.JetStream
	s  *TieredScenario
}

func newKVFixture(t *testing.T, variant string) *kvFixture {
	t.Helper()
	srv, err := natsserver.NewServer(&natsserver.Options{
		Port: -1, NoLog: true, NoSigs: true, JetStream: true, StoreDir: t.TempDir(),
	})
	require.NoError(t, err)
	srv.Start()
	require.True(t, srv.ReadyForConnections(5*time.Second))
	t.Cleanup(func() { srv.Shutdown(); srv.WaitForShutdown() })

	vc, err := client.NewNATSValidationClient(context.Background(), srv.ClientURL())
	require.NoError(t, err)
	t.Cleanup(func() { _ = vc.Close(context.Background()) })
	// Fixtures are written through the same connection the stages read with.
	js, err := vc.Client().JetStream()
	require.NoError(t, err)

	return &kvFixture{t: t, js: js, s: &TieredScenario{
		natsClient:         vc,
		effectiveAuthority: "c360.test-abc123",
		config: &TieredConfig{
			Variant: variant, ValidationTimeout: 10 * time.Millisecond, PollInterval: time.Millisecond,
		},
	}}
}

func (f *kvFixture) put(bucket, key string, value any) {
	f.t.Helper()
	ctx := context.Background()
	kv, err := f.js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: bucket})
	require.NoError(f.t, err)
	raw, err := json.Marshal(value)
	require.NoError(f.t, err)
	_, err = kv.Put(ctx, key, raw)
	require.NoError(f.t, err)
}

func (f *kvFixture) bucket(name string) {
	f.t.Helper()
	_, err := f.js.CreateOrUpdateKeyValue(context.Background(), jetstream.KeyValueConfig{Bucket: name})
	require.NoError(f.t, err)
}

const (
	testContainer = "c360.test-abc123.document.content.group"
	testMember    = "c360.test-abc123.document.content.safety.doc-safety-001"
)

func (f *kvFixture) entities(ids ...string) {
	for _, id := range ids {
		f.put(graph.BucketEntityStates, id, map[string]any{"id": id})
	}
}

func (f *kvFixture) incoming(target, source, predicate string) {
	f.put(graph.BucketIncomingIndex, target+"."+source+"."+graph.EncodePredicateToken(predicate), map[string]any{})
}

func (f *kvFixture) outgoing(source string, entries ...client.OutgoingEntry) {
	if entries == nil {
		entries = []client.OutgoingEntry{}
	}
	f.put(graph.BucketOutgoingIndex, source, entries)
}

func TestIndexPopulation_EmptyRequiredIndexFails(t *testing.T) {
	f := newKVFixture(t, "statistical")
	err := f.s.executeVerifyIndexPopulation(context.Background(), newResult())
	require.Error(t, err)
	require.Contains(t, err.Error(), "required indexes empty or unreadable")

	f.entities(testMember)
	for _, b := range []string{graph.BucketPredicateIndex, graph.BucketIncomingIndex, graph.BucketOutgoingIndex, graph.BucketTemporalIndex} {
		f.put(b, "k", map[string]any{})
	}
	require.NoError(t, f.s.executeVerifyIndexPopulation(context.Background(), newResult()))
}

func TestEntityStructure_EmptySampleFails(t *testing.T) {
	f := newKVFixture(t, "statistical")
	f.bucket(graph.BucketEntityStates)
	err := f.s.executeValidateEntityStructure(context.Background(), newResult())
	require.Error(t, err)
	require.Contains(t, err.Error(), "no entities available for structure validation")
}

func TestEntityRetrieval_MissingFixtureEntityFails(t *testing.T) {
	f := newKVFixture(t, "statistical")
	f.entities(testMember)
	err := f.s.executeVerifyEntityRetrieval(context.Background(), newResult())
	require.Error(t, err)
	require.Contains(t, err.Error(), "retrieved 0/5 test entities")
}

func TestHierarchyInference_TooFewContainersFails(t *testing.T) {
	sources := func(n int) []string {
		ids := make([]string, n)
		for i := range ids {
			ids[i] = "c360.test-abc123.document.content.safety.doc-" + string(rune('a'+i))
		}
		return ids
	}

	f := newKVFixture(t, "statistical")
	f.entities(append(sources(10), testContainer)...) // 1 container < 40% of 10
	err := f.s.validateHierarchyInference(context.Background(), newResult())
	require.Error(t, err)
	require.Contains(t, err.Error(), "hierarchy inference not working: only 1 containers for 10 source entities")

	f.entities("c360.test-abc123.a.b.c.group", "c360.test-abc123.a.b.d.group", "c360.test-abc123.a.b.e.group")
	require.NoError(t, f.s.validateHierarchyInference(context.Background(), newResult()))
}

func TestIncomingIndex_NoContainerFails(t *testing.T) {
	f := newKVFixture(t, "statistical")
	f.entities(testMember)
	err := f.s.validateIncomingIndexPredicates(context.Background(), newResult())
	require.Error(t, err)
	require.Contains(t, err.Error(), "no .group container entity found")
}

func TestBidirectionalTraversal(t *testing.T) {
	t.Run("no container fails", func(t *testing.T) {
		f := newKVFixture(t, "statistical")
		f.entities(testMember)
		err := f.s.validateBidirectionalTraversal(context.Background(), newResult())
		require.Error(t, err)
		require.Contains(t, err.Error(), "no .group container entity found")
	})
	t.Run("no member edge fails", func(t *testing.T) {
		f := newKVFixture(t, "statistical")
		f.entities(testMember, testContainer)
		f.incoming(testContainer, testMember, "hierarchy.type.sibling")
		err := f.s.validateBidirectionalTraversal(context.Background(), newResult())
		require.Error(t, err)
		require.Contains(t, err.Error(), "none is hierarchy.type.member")
	})
	t.Run("member edge passes", func(t *testing.T) {
		f := newKVFixture(t, "statistical")
		f.entities(testMember, testContainer)
		f.incoming(testContainer, testMember, "hierarchy.type.member")
		require.NoError(t, f.s.validateBidirectionalTraversal(context.Background(), newResult()))
	})
}

func TestInverseEdges(t *testing.T) {
	contains := client.OutgoingEntry{Predicate: "hierarchy.type.contains", ToEntityID: testMember}
	setup := func(t *testing.T, variant string, out ...client.OutgoingEntry) *kvFixture {
		f := newKVFixture(t, variant)
		f.entities(testMember, testContainer)
		f.incoming(testContainer, testMember, "hierarchy.type.member")
		f.outgoing(testContainer, out...)
		return f
	}

	t.Run("missing contains edge fails in statistical", func(t *testing.T) {
		err := setup(t, "statistical").s.validateInverseEdgesMaterialized(context.Background(), newResult())
		require.Error(t, err)
		require.Contains(t, err.Error(), "inverse edges asymmetric")
	})
	t.Run("count mismatch fails", func(t *testing.T) {
		err := setup(t, "semantic", contains, contains).s.validateInverseEdgesMaterialized(context.Background(), newResult())
		require.Error(t, err)
		require.Contains(t, err.Error(), "1 member edges vs 2 contains edges")
	})
	t.Run("structural keeps its not-yet-indexed note", func(t *testing.T) {
		require.NoError(t, setup(t, "structural").s.validateInverseEdgesMaterialized(context.Background(), newResult()))
	})
	t.Run("symmetric passes", func(t *testing.T) {
		require.NoError(t, setup(t, "statistical", contains).s.validateInverseEdgesMaterialized(context.Background(), newResult()))
	})
}
