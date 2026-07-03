//go:build integration

package graphindex

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

// TestPredicateIndex_LoadScale is the ADR-065-mandated load test: it must
// demonstrate that handleQueryPredicateListNATS's unfiltered path stays a
// small, fixed number of round trips (2: one membership Keys(), one
// catalog Keys()) rather than degenerating into a per-predicate fan-out,
// at a corpus shape proportioned to GH #430's real-world trigger (~21k
// entities, one predicate carried by nearly all of them).
//
// Scaled down from the full 21k-entity corpus to bound CI runtime: 5,000
// members on the hot predicate is enough to make an accidental N-way
// fan-out or a real full-bucket-scan-per-predicate design show up
// immediately in the elapsed time (each fan-out round trip is a bound
// ephemeral JetStream consumer, not a cheap Get — see ADR-065's Risks
// section), while keeping the test itself fast. What this test proves:
// listAllPredicates completes well inside the handler's 10s timeout at
// this scale. What it does NOT prove: exact wall-clock at the full 21k
// scale — that's a claim about production ingest throughput, not about
// this one read handler's round-trip count, which is what was actually
// in question.
func TestPredicateIndex_LoadScale(t *testing.T) {
	const hotPredicateMembers = 5000
	const otherPredicates = 20

	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	nc := testClient.Client

	config := DefaultConfig()
	configJSON, err := json.Marshal(config)
	require.NoError(t, err)

	comp, err := CreateGraphIndex(configJSON, component.Dependencies{NATSClient: nc})
	require.NoError(t, err)
	graphIndex := comp.(*Component)
	require.NoError(t, graphIndex.Initialize())

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	js, err := nc.JetStream()
	require.NoError(t, err)
	_, err = js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      graph.BucketEntityStates,
		Description: "Test entity states",
	})
	require.NoError(t, err)

	require.NoError(t, graphIndex.Start(ctx))
	defer graphIndex.Stop(5 * time.Second)

	// Seed directly through the production write path, concurrently —
	// this test benchmarks the READ side; the write side's own cost is
	// already covered by the O(N)-vs-O(N²) fix itself (this is exactly
	// the pattern the old design made catastrophically slow at this
	// scale — writes here are the unconditional-Put fast path, so
	// seeding 5k+20 entries should itself be fast, not just the read).
	seedStart := time.Now()
	var wg sync.WaitGroup
	sem := make(chan struct{}, 32)
	for i := 0; i < hotPredicateMembers; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			entityID := fmt.Sprintf("c360.load.test.corpus.entity.e%d", i)
			require.NoError(t, graphIndex.UpdatePredicateIndex(ctx, entityID, "code.artifact.type"))
		}(i)
	}
	for i := 0; i < otherPredicates; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			entityID := fmt.Sprintf("c360.load.test.corpus.other.o%d", i)
			predicate := fmt.Sprintf("code.artifact.tag%d", i)
			require.NoError(t, graphIndex.UpdatePredicateIndex(ctx, entityID, predicate))
		}(i)
	}
	wg.Wait()
	t.Logf("seeded %d composite keys across %d predicates in %v", hotPredicateMembers+otherPredicates, otherPredicates+1, time.Since(seedStart))

	// This is the call under test: the unfiltered predicateList path.
	readStart := time.Now()
	respData, err := graphIndex.handleQueryPredicateListNATS(ctx, nil)
	readElapsed := time.Since(readStart)
	require.NoError(t, err)

	var resp graph.PredicateListQueryResponse
	require.NoError(t, json.Unmarshal(respData, &resp))

	t.Logf("handleQueryPredicateListNATS over %d composite keys took %v", hotPredicateMembers+otherPredicates, readElapsed)

	require.Len(t, resp.Data.Predicates, otherPredicates+1, "must report every predicate, not silently truncate")
	for _, p := range resp.Data.Predicates {
		if p.Predicate == "code.artifact.type" {
			require.Equal(t, hotPredicateMembers, p.EntityCount, "hot predicate's count must reflect all its members, not just a sample")
		}
	}

	// The handler's own timeout is 10s (query.go). Requiring well inside
	// that at 5k+ members is the actual regression signal: an N-way
	// per-predicate fan-out design would burn most of that budget on
	// ephemeral-consumer setup alone across even 21 predicates, let alone
	// scanning 5k members serially.
	require.Less(t, readElapsed, 3*time.Second,
		"unfiltered predicateList must stay well inside its 10s timeout at this scale — if this regresses, check for a reintroduced per-predicate KeysByPrefix fan-out (ADR-065)")
}
