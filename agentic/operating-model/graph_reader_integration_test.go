//go:build integration

package operatingmodel

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
)

// TestIntegration_ReadOperatingModel_MultiUserIsolation is the real-NATS
// counterpart to the unit test in graph_reader_test.go. It writes triples
// for two users into a real KV bucket using the same APIs production
// graph-ingest uses (KVStore.Put with appended triples on the entity state)
// and asserts that each ReadOperatingModel call returns only that user's
// entries.
//
// This is the regression test for issue #14: a multi-user data-isolation
// bug in GraphProfileReader.ReadOperatingModel.
func TestIntegration_ReadOperatingModel_MultiUserIsolation(t *testing.T) {
	const bucketName = "ENTITY_STATES_OM_MULTIUSER"

	tc := natsclient.NewTestClient(t,
		natsclient.WithKV(),
		natsclient.WithKVBuckets(bucketName),
	)
	t.Cleanup(func() { _ = tc.Terminate() })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	bucket, err := tc.Client.GetKeyValueBucket(ctx, bucketName)
	require.NoError(t, err)
	kv := tc.Client.NewKVStore(bucket)

	aliceRef := ProfileRef{Org: "acme", Platform: "ops", UserID: "alice", Version: 1}
	aliceEntries := []Entry{
		mkEntry(LayerOperatingRhythms, "a1", "alice planning", "alice Mondays"),
	}
	writeProfileToBucket(t, ctx, kv, aliceRef, LayerOperatingRhythms, aliceEntries)

	bobRef := ProfileRef{Org: "acme", Platform: "ops", UserID: "bob", Version: 5}
	bobEntries := []Entry{
		mkEntry(LayerDependencies, "b1", "bob dependencies", "bob upstream"),
		mkEntry(LayerDependencies, "b2", "bob systems", "bob CRM"),
	}
	writeProfileToBucket(t, ctx, kv, bobRef, LayerDependencies, bobEntries)

	r := &GraphProfileReader{kv: kv, logger: discardLogger()}

	t.Run("alice gets only alice", func(t *testing.T) {
		got, err := r.ReadOperatingModel(ctx, aliceRef.Org, aliceRef.Platform, aliceRef.UserID)
		require.NoError(t, err)
		require.NotNil(t, got)
		require.Equal(t, aliceRef.Version, got.Version)
		require.Len(t, got.Entries, len(aliceEntries))
		gotIDs := entryIDs(got.Entries)
		for _, b := range bobEntries {
			require.NotContainsf(t, gotIDs, b.EntryID,
				"LEAK: alice's result contains bob's entry %q", b.EntryID)
		}
	})

	t.Run("bob gets only bob", func(t *testing.T) {
		got, err := r.ReadOperatingModel(ctx, bobRef.Org, bobRef.Platform, bobRef.UserID)
		require.NoError(t, err)
		require.NotNil(t, got)
		require.Equal(t, bobRef.Version, got.Version)
		require.Len(t, got.Entries, len(bobEntries))
		gotIDs := entryIDs(got.Entries)
		for _, a := range aliceEntries {
			require.NotContainsf(t, gotIDs, a.EntryID,
				"LEAK: bob's result contains alice's entry %q", a.EntryID)
		}
	})
}

// writeProfileToBucket writes one layer's worth of triples to a real KV
// bucket using append semantics — mirrors graph-ingest's AddTriple behaviour
// so the integration test exercises the same data shape the reader sees in
// production.
func writeProfileToBucket(t *testing.T, ctx context.Context, kv *natsclient.KVStore, ref ProfileRef, layer string, entries []Entry) {
	t.Helper()
	now := time.Now().UTC()
	triples := LayerTriples(ref, layer, "checkpoint", entries, now)

	bySubject := make(map[string][]message.Triple)
	for _, tr := range triples {
		bySubject[tr.Subject] = append(bySubject[tr.Subject], tr)
	}
	for id, ts := range bySubject {
		var state graph.EntityState
		existing, err := kv.Get(ctx, id)
		switch {
		case err == nil:
			require.NoError(t, json.Unmarshal(existing.Value, &state))
		case natsclient.IsKVNotFoundError(err):
			state = graph.EntityState{ID: id, UpdatedAt: now}
		default:
			t.Fatalf("kv get %s: %v", id, err)
		}
		state.Triples = append(state.Triples, ts...)
		state.UpdatedAt = now

		data, err := json.Marshal(state)
		require.NoError(t, err)
		_, err = kv.Put(ctx, id, data)
		require.NoError(t, err)
	}
}
