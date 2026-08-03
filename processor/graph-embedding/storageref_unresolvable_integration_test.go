//go:build integration

package graphembedding

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/graph/embedding"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/storage/objectstore"
	"github.com/c360studio/semstreams/storage/storeregistry"
)

// gh875Config builds a graph-embedding config that OWNS a content store for bucket
// (a store-read port), which is what makes the pre-fix gate answer "some store is
// wired" for every instance.
func gh875Config(t *testing.T, bucket string) []byte {
	t.Helper()
	cfg := Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{Name: "entity_watch", Type: "kv-watch", Subject: graph.BucketEntityStates},
				{Name: "content_store", Type: "store-read", Bucket: bucket},
			},
		},
		EmbedderType: "bm25",
	}
	data, err := json.Marshal(cfg)
	require.NoError(t, err)
	return data
}

// gh875Entity writes an ENTITY_STATES entity whose body is offloaded to instance and
// which also carries one inline text triple.
func gh875Entity(id, instance string) graph.EntityState {
	now := time.Now().UTC()
	return graph.EntityState{
		ID: id,
		Triples: []message.Triple{
			{Subject: id, Predicate: "doc.text.title", Object: "hydraulic pump service report",
				Source: "test", Timestamp: now},
		},
		StorageRef:  &message.StorageReference{StorageInstance: instance, Key: "loops/x/step/1"},
		MessageType: message.Type{Domain: "test", Category: "entity", Version: "v1"},
		Version:     1,
		UpdatedAt:   now,
	}
}

func gh875Record(ctx context.Context, t *testing.T, bucket jetstream.KeyValue, id string) (embedding.Record, bool) {
	t.Helper()
	entry, err := bucket.Get(ctx, id)
	if err != nil {
		return embedding.Record{}, false
	}
	var rec embedding.Record
	if json.Unmarshal(entry.Value(), &rec) != nil {
		return embedding.Record{}, false
	}
	return rec, true
}

// TestIntegration_GH875_UnresolvableInstanceIsExcludedNotFailed is the inversion of
// the defect this change fixes. Against origin/main (2dce8258) the SAME wire produced,
// as observed on 2026-08-03:
//
//	durable record: status="failed" reason="content_error"
//	  err="text extraction failed: failed to open content from instance \"AGENT_CONTENT\":
//	       Store.Open: open object stream failed: nats: object not found"
//	readiness: state="degraded" failed=1 reasons=map[content_error:1]
//
// and both halves of its stickiness reproduced: re-delivering the entity failed again
// at the new revision, and a restart re-seeded the failed map ("seeded current-failed
// map from durable EMBEDDING_INDEX failed_count=1") so the index booted degraded with
// no exit but deleting the entity. Re-delivery cannot repair a deployment wiring gap.
func TestIntegration_GH875_UnresolvableInstanceIsExcludedNotFailed(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	nc := testClient.Client

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// The shared registry is EMPTY: nothing resolves the instance the entity names.
	configJSON := gh875Config(t, "GH875_OWNED")
	comp, err := CreateGraphEmbedding(configJSON, component.Dependencies{
		NATSClient:    nc,
		StoreRegistry: storeregistry.New(),
	})
	require.NoError(t, err)
	c := comp.(*Component)
	require.NoError(t, c.Initialize())

	js, err := nc.JetStream()
	require.NoError(t, err)
	entityBucket, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket: graph.BucketEntityStates, Description: "Test entity states",
	})
	require.NoError(t, err)

	require.NoError(t, c.Start(ctx))
	stopped := false
	defer func() {
		if !stopped {
			_ = c.Stop(5 * time.Second)
		}
	}()

	require.NotNil(t, c.contentStore, "the store-read port must produce an owned content store")
	require.Equal(t, "GH875_OWNED", c.contentStore.InstanceName(),
		"a store built without an instance name stamps the bucket name (gh#400)")

	var embeddingBucket jetstream.KeyValue
	require.Eventually(t, func() bool {
		embeddingBucket, err = js.KeyValue(ctx, graph.BucketEmbeddingIndex)
		return err == nil
	}, 10*time.Second, 100*time.Millisecond, "EMBEDDING_INDEX bucket should be created")

	const entityID = "c360.platform.docs.sys.doc.001"
	const foreignInstance = "AGENT_CONTENT" // never registered; not the owned store's instance

	unresolvedBefore := testutil.ToFloat64(c.metrics.contentUnresolved)

	state := gh875Entity(entityID, foreignInstance)
	data, err := json.Marshal(state)
	require.NoError(t, err)
	_, err = entityBucket.Put(ctx, entityID, data)
	require.NoError(t, err)

	// It reaches a terminal outcome from its INLINE text — the offloaded body is the
	// only thing lost.
	var rec embedding.Record
	require.Eventually(t, func() bool {
		var ok bool
		rec, ok = gh875Record(ctx, t, embeddingBucket, entityID)
		return ok && rec.Status == embedding.StatusGenerated
	}, 20*time.Second, 200*time.Millisecond,
		"an entity whose body is unreachable must still embed the inline text it carries")
	require.NotEqual(t, embedding.StatusFailed, rec.Status)
	// It is a QUALIFIED success, and this is the DOMINANT path (hop 1's gate refuses the
	// offloaded lane, so hop 2 never sees a StorageRef). Enumeration depends on this:
	// "which entities have a vector missing its body?" is answered by scanning for the
	// qualifier, and repair depends on it too — SavePendingGuarded re-queues over a
	// qualified record, so wiring the store heals the entity.
	require.Equal(t, embedding.ReasonContentExcluded, rec.Reason,
		"the record must record what is missing from the vector, not look complete")

	status := c.computeEmbeddingStatus(ctx)
	require.Zero(t, status.FailedCount,
		"a deployment wiring fact must not enter FailedCount")
	require.NotEqual(t, graph.IndexStateDegraded, status.State,
		"FailedCount>0 pins Degraded unconditionally; an unreachable body must not reach it")

	require.Equal(t, float64(1), testutil.ToFloat64(c.metrics.contentUnresolved)-unresolvedBefore,
		"the exclusion must be observable on content_unresolved_total (gh#414), not silent")

	// Re-delivery produces the SAME outcome instead of accumulating failures.
	state.Version = 2
	state.UpdatedAt = time.Now().UTC()
	data, err = json.Marshal(state)
	require.NoError(t, err)
	_, err = entityBucket.Put(ctx, entityID, data)
	require.NoError(t, err)
	require.Never(t, func() bool {
		r, ok := gh875Record(ctx, t, embeddingBucket, entityID)
		return ok && r.Status == embedding.StatusFailed
	}, 3*time.Second, 200*time.Millisecond,
		"re-delivery must not accumulate a failure for a wiring gap it cannot repair")
	require.Zero(t, c.computeEmbeddingStatus(ctx).FailedCount)

	// A restart seeds NO failure: there is no durable failed record to re-seed.
	require.NoError(t, c.Stop(5*time.Second))
	stopped = true

	comp2, err := CreateGraphEmbedding(configJSON, component.Dependencies{
		NATSClient:    nc,
		StoreRegistry: storeregistry.New(),
	})
	require.NoError(t, err)
	c2 := comp2.(*Component)
	require.NoError(t, c2.Initialize())
	require.NoError(t, c2.Start(ctx))
	defer func() { _ = c2.Stop(5 * time.Second) }()

	require.Never(t, func() bool {
		return c2.computeEmbeddingStatus(ctx).FailedCount > 0
	}, 3*time.Second, 200*time.Millisecond,
		"the restart must not boot degraded over an unreachable body")
}

// TestIntegration_GH875_GateResolvableThenDeregisteredExcludes drives the race the
// two-place fix exists for: the instance resolves when the component's hop-1 gate asks
// (asserted through the production gate itself), the owning store is deregistered
// before hop 2 reads, and hop 2 must still exclude rather than latch a durable failure.
// The registry is per-fetch with no caching and Deregister-on-Stop is live, so gating
// alone would leave this producing the original defect through a narrower door.
func TestIntegration_GH875_GateResolvableThenDeregisteredExcludes(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	nc := testClient.Client

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	const instance = "content"
	store, err := objectstore.NewStoreWithConfig(ctx, nc, objectstore.Config{
		BucketName: "GH875_RACE_CONTENT", InstanceName: instance,
	})
	require.NoError(t, err)
	require.NoError(t, store.Put(ctx, "loops/x/step/1", []byte("the offloaded body text")))

	reg := storeregistry.New()
	require.NoError(t, reg.Register(instance, store))

	comp, err := CreateGraphEmbedding(gh875Config(t, "GH875_RACE_OWNED"), component.Dependencies{
		NATSClient:    nc,
		StoreRegistry: reg,
	})
	require.NoError(t, err)
	c := comp.(*Component)
	require.NoError(t, c.Initialize())

	js, err := nc.JetStream()
	require.NoError(t, err)
	_, err = js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket: graph.BucketEntityStates, Description: "Test entity states",
	})
	require.NoError(t, err)

	require.NoError(t, c.Start(ctx))
	defer func() { _ = c.Stop(5 * time.Second) }()

	var embeddingBucket jetstream.KeyValue
	require.Eventually(t, func() bool {
		embeddingBucket, err = js.KeyValue(ctx, graph.BucketEmbeddingIndex)
		return err == nil
	}, 10*time.Second, 100*time.Millisecond, "EMBEDDING_INDEX bucket should be created")

	const entityID = "c360.platform.docs.sys.doc.002"
	state := gh875Entity(entityID, instance)

	// The PRODUCTION gate resolves the instance right now — this is hop 1 deciding to
	// take the offloaded fetch path.
	require.True(t, c.shouldFetchViaStorageRef(&state),
		"precondition: the instance must resolve at the gate")

	// Snapshot the counter BEFORE hop 1's write. This test never writes to
	// ENTITY_STATES, so hop 1's own exclusion path cannot contribute — every increment
	// observed below came from HOP 2, through the adapter wiring in Start. Without this
	// assertion, nilling the reporter argument to newWorkerMetricsAdapter leaves the
	// whole suite green while hop 2's exclusion emits no counter and no warning: the
	// silent skip gh#875's acceptance criteria forbid.
	unresolvedBefore := testutil.ToFloat64(c.metrics.contentUnresolved)

	// hop 1's own write, made here so the deregistration lands deterministically
	// between the gate's decision and hop 2's read.
	require.NoError(t, c.storage.SavePendingWithStorageRef(ctx, entityID, "",
		"hydraulic pump service report",
		&embedding.StorageRef{StorageInstance: instance, Key: "loops/x/step/1"}, nil, 1))

	// The owning storage component Stops: ComponentManager deregisters the instance.
	reg.Deregister(instance)

	require.Eventually(t, func() bool {
		rec, ok := gh875Record(ctx, t, embeddingBucket, entityID)
		return ok && rec.Status == embedding.StatusGenerated
	}, 20*time.Second, 200*time.Millisecond,
		"the entity must reach a terminal outcome from its inline identity text")

	rec, ok := gh875Record(ctx, t, embeddingBucket, entityID)
	require.True(t, ok)
	require.NotEqual(t, embedding.StatusFailed, rec.Status,
		"a store deregistered between the gate and the fetch must not latch a durable failure")
	require.Equal(t, embedding.ReasonContentExcluded, rec.Reason,
		"the race produces a QUALIFIED success: the record must say the body is missing")
	require.Zero(t, c.computeEmbeddingStatus(ctx).FailedCount)

	// The wiring assertion (see the snapshot above): hop 2's exclusion must reach the
	// component's reporter. This is what makes newWorkerMetricsAdapter's third argument
	// load-bearing in a test rather than only in production.
	require.Equal(t, float64(1), testutil.ToFloat64(c.metrics.contentUnresolved)-unresolvedBefore,
		"hop 2's exclusion must increment content_unresolved_total through the wired reporter; "+
			"a silent skip is not acceptable (gh#875 acceptance)")
}

// TestIntegration_GH875_WiringTheStoreSelfHealsTheQualifiedRecord proves the repair
// half of the qualified-success model, end to end, the way an operator experiences it:
// an entity whose body was unreachable holds a QUALIFIED generated record; the operator
// then wires the missing store and restarts; the entity re-embeds WITH its body and the
// qualifier clears — with no write to ENTITY_STATES, so the restart's last-per-subject
// re-delivery at the SAME revision is the only trigger.
//
// Without the qualifier, that re-delivery is swallowed by SavePendingGuarded's skip
// ("a same-or-newer generated vector stands") and the body-less vector is permanent.
// This is the regression review caught: the pre-reframe fix converted a self-healing
// state (today's failed record, which the guard overwrites) into a stuck one.
func TestIntegration_GH875_WiringTheStoreSelfHealsTheQualifiedRecord(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	nc := testClient.Client

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	const instance = "content"
	const bodyTerms = "peregrine falcon telemetry downlink budget"
	store, err := objectstore.NewStoreWithConfig(ctx, nc, objectstore.Config{
		BucketName: "GH875_HEAL_CONTENT", InstanceName: instance,
	})
	require.NoError(t, err)
	require.NoError(t, store.Put(ctx, "loops/x/step/1", []byte(bodyTerms)))

	configJSON := gh875Config(t, "GH875_HEAL_OWNED")

	// Phase 1 — the store is NOT registered: the body is unreachable.
	comp, err := CreateGraphEmbedding(configJSON, component.Dependencies{
		NATSClient:    nc,
		StoreRegistry: storeregistry.New(),
	})
	require.NoError(t, err)
	c := comp.(*Component)
	require.NoError(t, c.Initialize())

	js, err := nc.JetStream()
	require.NoError(t, err)
	entityBucket, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket: graph.BucketEntityStates, Description: "Test entity states",
	})
	require.NoError(t, err)

	require.NoError(t, c.Start(ctx))
	phase1Stopped := false
	defer func() {
		if !phase1Stopped {
			_ = c.Stop(5 * time.Second)
		}
	}()

	var embeddingBucket jetstream.KeyValue
	require.Eventually(t, func() bool {
		embeddingBucket, err = js.KeyValue(ctx, graph.BucketEmbeddingIndex)
		return err == nil
	}, 10*time.Second, 100*time.Millisecond, "EMBEDDING_INDEX bucket should be created")

	const entityID = "c360.platform.docs.sys.doc.004"
	state := gh875Entity(entityID, instance)
	data, err := json.Marshal(state)
	require.NoError(t, err)
	_, err = entityBucket.Put(ctx, entityID, data)
	require.NoError(t, err)

	var qualified embedding.Record
	require.Eventually(t, func() bool {
		var ok bool
		qualified, ok = gh875Record(ctx, t, embeddingBucket, entityID)
		return ok && qualified.Status == embedding.StatusGenerated
	}, 20*time.Second, 200*time.Millisecond, "the entity must embed its inline text")
	require.Equal(t, embedding.ReasonContentExcluded, qualified.Reason,
		"phase 1 must leave an ENUMERABLE record: generated, qualified content_excluded")

	// Phase 2 — the operator wires the store and restarts. NOTHING is written to
	// ENTITY_STATES, so only the restart's re-delivery at the same revision can heal it.
	require.NoError(t, c.Stop(5*time.Second))
	phase1Stopped = true

	reg := storeregistry.New()
	require.NoError(t, reg.Register(instance, store))
	comp2, err := CreateGraphEmbedding(configJSON, component.Dependencies{
		NATSClient:    nc,
		StoreRegistry: reg,
	})
	require.NoError(t, err)
	c2 := comp2.(*Component)
	require.NoError(t, c2.Initialize())
	require.NoError(t, c2.Start(ctx))
	defer func() { _ = c2.Stop(5 * time.Second) }()

	require.Eventually(t, func() bool {
		rec, ok := gh875Record(ctx, t, embeddingBucket, entityID)
		return ok && rec.Status == embedding.StatusGenerated && rec.Reason == ""
	}, 30*time.Second, 250*time.Millisecond,
		"wiring the store and restarting must clear the qualifier — the entity re-embeds WITH its body")

	require.Zero(t, c2.computeEmbeddingStatus(ctx).FailedCount)

	// The body is genuinely in the vector now: a query over body-only terms (absent from
	// the entity's inline title) retrieves it.
	queryVecs, err := c2.embedder.GenerateQuery(ctx, []string{bodyTerms})
	require.NoError(t, err)
	require.Len(t, queryVecs, 1)
	results, err := c2.findSimilarEntities(ctx, "", queryVecs[0], nil, 10)
	require.NoError(t, err)
	require.NotEmpty(t, results, "the healed vector must contain the body terms")
	require.Equal(t, entityID, results[0].EntityID)
}

// TestIntegration_GH875_OwnedStoreStillServesItsOwnInstance is the other half of
// bounding rather than deleting the fallback: a reference naming the owned store's own
// instance (a bare store stamps the BUCKET name, gh#400) still fetches its body, with
// no registry involved. This is the single-bucket deploy shape ADR-063 named, and the
// reason the fallback is bounded instead of removed.
func TestIntegration_GH875_OwnedStoreStillServesItsOwnInstance(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	nc := testClient.Client

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	const bucket = "GH875_SINGLE"
	// A producer writing through a bare store against the same bucket: its refs carry
	// StorageInstance == bucket name.
	producer, err := objectstore.NewStoreWithConfig(ctx, nc, objectstore.Config{BucketName: bucket})
	require.NoError(t, err)
	require.Equal(t, bucket, producer.InstanceName())
	const body = "peregrine falcon telemetry subsystem downlink budget"
	require.NoError(t, producer.Put(ctx, "loops/x/step/1", []byte(body)))

	comp, err := CreateGraphEmbedding(gh875Config(t, bucket), component.Dependencies{
		NATSClient:    nc,
		StoreRegistry: storeregistry.New(), // empty: the fallback is the only path
	})
	require.NoError(t, err)
	c := comp.(*Component)
	require.NoError(t, c.Initialize())

	js, err := nc.JetStream()
	require.NoError(t, err)
	entityBucket, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket: graph.BucketEntityStates, Description: "Test entity states",
	})
	require.NoError(t, err)

	require.NoError(t, c.Start(ctx))
	defer func() { _ = c.Stop(5 * time.Second) }()

	var embeddingBucket jetstream.KeyValue
	require.Eventually(t, func() bool {
		embeddingBucket, err = js.KeyValue(ctx, graph.BucketEmbeddingIndex)
		return err == nil
	}, 10*time.Second, 100*time.Millisecond, "EMBEDDING_INDEX bucket should be created")

	const entityID = "c360.platform.docs.sys.doc.003"
	state := gh875Entity(entityID, bucket)
	require.True(t, c.shouldFetchViaStorageRef(&state),
		"the owned store MUST answer for the instance it actually serves")

	data, err := json.Marshal(state)
	require.NoError(t, err)
	_, err = entityBucket.Put(ctx, entityID, data)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		rec, ok := gh875Record(ctx, t, embeddingBucket, entityID)
		return ok && rec.Status == embedding.StatusGenerated
	}, 20*time.Second, 200*time.Millisecond,
		"the offloaded body must still be fetched through the bounded fallback")
	require.Zero(t, c.computeEmbeddingStatus(ctx).FailedCount)

	// The body actually reached the vector: a query naming body-only terms retrieves it.
	queryVecs, err := c.embedder.GenerateQuery(ctx, []string{"peregrine falcon downlink budget"})
	require.NoError(t, err)
	require.Len(t, queryVecs, 1)
	results, err := c.findSimilarEntities(ctx, "", queryVecs[0], nil, 10)
	require.NoError(t, err)
	require.NotEmpty(t, results, "the fetched body must be part of the stored vector")
	require.Equal(t, entityID, results[0].EntityID)
}
