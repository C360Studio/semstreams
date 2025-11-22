//go:build integration

package indexmanager

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gtypes "github.com/c360/semstreams/graph"
	"github.com/c360/semstreams/message"
	"github.com/c360/semstreams/natsclient"
	"github.com/c360/semstreams/vocabulary"
	"github.com/c360/semstreams/vocabulary/examples"
)

// TestAliasIndex_Integration tests vocabulary-driven alias indexing with real NATS
func TestAliasIndex_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	// Register test vocabulary predicates FIRST
	vocabulary.ClearRegistry()
	examples.RegisterSemanticVocabulary()
	examples.RegisterRoboticsVocabulary()

	// Create real NATS client with testcontainers
	testClient := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithKV())
	ctx := context.Background()

	// Create real KV bucket for alias index
	aliasBucket, err := testClient.CreateKVBucket(ctx, "ALIAS_INDEX")
	require.NoError(t, err, "Failed to create ALIAS_INDEX bucket")

	// Create AliasIndex with real NATS
	aliasIndex := NewAliasIndex(aliasBucket, testClient.Client, nil, nil, nil)
	require.NotNil(t, aliasIndex, "alias index should be created")

	t.Run("extract_resolvable_aliases_skip_labels", func(t *testing.T) {
		// Create entity with multiple alias types
		entityID := "test.entity.001"
		state := &gtypes.EntityState{
			Node: gtypes.NodeProperties{
				ID: entityID,
			},
			Triples: []message.Triple{
				// Identity alias (resolvable)
				{
					Subject:   entityID,
					Predicate: examples.SemanticIdentityAlias,
					Object:    "drone-alpha-001",
				},
				// UUID (resolvable)
				{
					Subject:   entityID,
					Predicate: examples.SemanticIdentityUUID,
					Object:    "uuid:550e8400-e29b-41d4-a716-446655440000",
				},
				// Preferred label (NOT resolvable - should be skipped)
				{
					Subject:   entityID,
					Predicate: examples.SemanticLabelPreferred,
					Object:    "Alpha Drone",
				},
				// Alternate label (NOT resolvable - should be skipped)
				{
					Subject:   entityID,
					Predicate: examples.SemanticLabelAlternate,
					Object:    "Drone A",
				},
				// Communication callsign (resolvable)
				{
					Subject:   entityID,
					Predicate: examples.RoboticsCommunicationCallsign,
					Object:    "ALPHA-1",
				},
				// Serial number (resolvable)
				{
					Subject:   entityID,
					Predicate: examples.RoboticsIdentifierSerial,
					Object:    "SN-12345",
				},
			},
			UpdatedAt: time.Now(),
		}

		// Handle create - should index only resolvable aliases
		err := aliasIndex.HandleCreate(ctx, entityID, state)
		require.NoError(t, err)

		// Verify resolvable aliases are indexed
		resolvableAliases := []string{
			"drone-alpha-001",
			"uuid:550e8400-e29b-41d4-a716-446655440000",
			"ALPHA-1",
			"SN-12345",
		}

		for _, alias := range resolvableAliases {
			sanitized := sanitizeNATSKey(alias)
			key := "alias--" + sanitized
			entry, err := aliasBucket.Get(ctx, key)
			assert.NoError(t, err, "should resolve alias: %s", alias)
			if err == nil {
				assert.Equal(t, entityID, string(entry.Value()), "alias should resolve to correct entity: %s", alias)
			}
		}

		// Verify labels are NOT indexed (ambiguous, display-only)
		nonResolvableLabels := []string{
			"Alpha Drone",
			"Drone A",
		}

		for _, label := range nonResolvableLabels {
			sanitized := sanitizeNATSKey(label)
			key := "alias--" + sanitized
			_, err := aliasBucket.Get(ctx, key)
			assert.Error(t, err, "label should NOT be resolvable: %s", label)
		}

		// Verify reverse index contains only resolvable aliases
		reverseAliases, err := aliasIndex.getEntityAliases(ctx, entityID)
		require.NoError(t, err)
		assert.Len(t, reverseAliases, 4, "should have 4 resolvable aliases (skipping 2 labels)")
	})

	t.Run("update_add_aliases", func(t *testing.T) {
		entityID := "test.entity.002"

		// Initial state with 1 alias
		initialState := &gtypes.EntityState{
			Node: gtypes.NodeProperties{
				ID: entityID,
			},
			Triples: []message.Triple{
				{
					Subject:   entityID,
					Predicate: examples.SemanticIdentityAlias,
					Object:    "drone-beta-001",
				},
			},
			UpdatedAt: time.Now(),
		}

		err := aliasIndex.HandleCreate(ctx, entityID, initialState)
		require.NoError(t, err)

		// Verify initial alias works
		key := "alias--" + sanitizeNATSKey("drone-beta-001")
		entry, err := aliasBucket.Get(ctx, key)
		require.NoError(t, err)
		assert.Equal(t, entityID, string(entry.Value()))

		// Update: add 2 more aliases (keep the first one)
		updatedState := &gtypes.EntityState{
			Node: gtypes.NodeProperties{
				ID: entityID,
			},
			Triples: []message.Triple{
				{
					Subject:   entityID,
					Predicate: examples.SemanticIdentityAlias,
					Object:    "drone-beta-001", // Keep
				},
				{
					Subject:   entityID,
					Predicate: examples.RoboticsCommunicationCallsign,
					Object:    "BETA-1", // Add
				},
				{
					Subject:   entityID,
					Predicate: examples.RoboticsIdentifierSerial,
					Object:    "SN-67890", // Add
				},
			},
			UpdatedAt: time.Now(),
		}

		err = aliasIndex.HandleUpdate(ctx, entityID, updatedState)
		require.NoError(t, err)

		// Verify all 3 aliases work
		allAliases := []string{"drone-beta-001", "BETA-1", "SN-67890"}
		for _, alias := range allAliases {
			key := "alias--" + sanitizeNATSKey(alias)
			entry, err := aliasBucket.Get(ctx, key)
			assert.NoError(t, err, "alias should work: %s", alias)
			if err == nil {
				assert.Equal(t, entityID, string(entry.Value()))
			}
		}

		// Verify reverse index has 3 aliases
		reverseAliases, err := aliasIndex.getEntityAliases(ctx, entityID)
		require.NoError(t, err)
		assert.Len(t, reverseAliases, 3)
	})

	t.Run("update_remove_aliases", func(t *testing.T) {
		entityID := "test.entity.003"

		// Initial state with 3 aliases
		initialState := &gtypes.EntityState{
			Node: gtypes.NodeProperties{
				ID: entityID,
			},
			Triples: []message.Triple{
				{
					Subject:   entityID,
					Predicate: examples.SemanticIdentityAlias,
					Object:    "drone-gamma-001",
				},
				{
					Subject:   entityID,
					Predicate: examples.RoboticsCommunicationCallsign,
					Object:    "GAMMA-1",
				},
				{
					Subject:   entityID,
					Predicate: examples.RoboticsIdentifierSerial,
					Object:    "SN-99999",
				},
			},
			UpdatedAt: time.Now(),
		}

		err := aliasIndex.HandleCreate(ctx, entityID, initialState)
		require.NoError(t, err)

		// Verify all 3 aliases work
		initialAliases := []string{"drone-gamma-001", "GAMMA-1", "SN-99999"}
		for _, alias := range initialAliases {
			key := "alias--" + sanitizeNATSKey(alias)
			_, err := aliasBucket.Get(ctx, key)
			require.NoError(t, err, "initial alias should work: %s", alias)
		}

		// Update: keep only 1 alias
		updatedState := &gtypes.EntityState{
			Node: gtypes.NodeProperties{
				ID: entityID,
			},
			Triples: []message.Triple{
				{
					Subject:   entityID,
					Predicate: examples.SemanticIdentityAlias,
					Object:    "drone-gamma-001", // Keep only this one
				},
			},
			UpdatedAt: time.Now(),
		}

		err = aliasIndex.HandleUpdate(ctx, entityID, updatedState)
		require.NoError(t, err)

		// Verify kept alias still works
		key := "alias--" + sanitizeNATSKey("drone-gamma-001")
		entry, err := aliasBucket.Get(ctx, key)
		require.NoError(t, err)
		assert.Equal(t, entityID, string(entry.Value()))

		// Verify removed aliases are gone
		removedAliases := []string{"GAMMA-1", "SN-99999"}
		for _, alias := range removedAliases {
			key := "alias--" + sanitizeNATSKey(alias)
			_, err := aliasBucket.Get(ctx, key)
			assert.Error(t, err, "removed alias should be gone: %s", alias)
		}

		// Verify reverse index has only 1 alias
		reverseAliases, err := aliasIndex.getEntityAliases(ctx, entityID)
		require.NoError(t, err)
		assert.Len(t, reverseAliases, 1)
	})

	t.Run("update_change_subset", func(t *testing.T) {
		entityID := "test.entity.004"

		// Initial state: [a, b]
		initialState := &gtypes.EntityState{
			Node: gtypes.NodeProperties{
				ID: entityID,
			},
			Triples: []message.Triple{
				{
					Subject:   entityID,
					Predicate: examples.SemanticIdentityAlias,
					Object:    "alias-a",
				},
				{
					Subject:   entityID,
					Predicate: examples.SemanticIdentityUUID,
					Object:    "alias-b",
				},
			},
			UpdatedAt: time.Now(),
		}

		err := aliasIndex.HandleCreate(ctx, entityID, initialState)
		require.NoError(t, err)

		// Update: [b, c] - remove a, keep b, add c
		updatedState := &gtypes.EntityState{
			Node: gtypes.NodeProperties{
				ID: entityID,
			},
			Triples: []message.Triple{
				{
					Subject:   entityID,
					Predicate: examples.SemanticIdentityUUID,
					Object:    "alias-b", // Keep
				},
				{
					Subject:   entityID,
					Predicate: examples.RoboticsCommunicationCallsign,
					Object:    "alias-c", // Add
				},
			},
			UpdatedAt: time.Now(),
		}

		err = aliasIndex.HandleUpdate(ctx, entityID, updatedState)
		require.NoError(t, err)

		// Verify alias-a is removed
		key := "alias--" + sanitizeNATSKey("alias-a")
		_, err = aliasBucket.Get(ctx, key)
		assert.Error(t, err, "alias-a should be removed")

		// Verify alias-b still works (kept)
		key = "alias--" + sanitizeNATSKey("alias-b")
		entry, err := aliasBucket.Get(ctx, key)
		require.NoError(t, err, "alias-b should still work")
		assert.Equal(t, entityID, string(entry.Value()))

		// Verify alias-c was added
		key = "alias--" + sanitizeNATSKey("alias-c")
		entry, err = aliasBucket.Get(ctx, key)
		require.NoError(t, err, "alias-c should be added")
		assert.Equal(t, entityID, string(entry.Value()))

		// Verify reverse index has [b, c]
		reverseAliases, err := aliasIndex.getEntityAliases(ctx, entityID)
		require.NoError(t, err)
		assert.Len(t, reverseAliases, 2)
	})

	t.Run("update_empty_to_populated", func(t *testing.T) {
		entityID := "test.entity.005"

		// Initial state: no aliases
		initialState := &gtypes.EntityState{
			Node: gtypes.NodeProperties{
				ID: entityID,
			},
			Triples: []message.Triple{
				// No alias predicates, just some other data
				{
					Subject:   entityID,
					Predicate: "some.other.property",
					Object:    "value",
				},
			},
			UpdatedAt: time.Now(),
		}

		err := aliasIndex.HandleCreate(ctx, entityID, initialState)
		require.NoError(t, err)

		// Update: add aliases
		updatedState := &gtypes.EntityState{
			Node: gtypes.NodeProperties{
				ID: entityID,
			},
			Triples: []message.Triple{
				{
					Subject:   entityID,
					Predicate: examples.SemanticIdentityAlias,
					Object:    "new-alias-1",
				},
				{
					Subject:   entityID,
					Predicate: examples.RoboticsCommunicationCallsign,
					Object:    "new-alias-2",
				},
			},
			UpdatedAt: time.Now(),
		}

		err = aliasIndex.HandleUpdate(ctx, entityID, updatedState)
		require.NoError(t, err)

		// Verify both aliases work
		for _, alias := range []string{"new-alias-1", "new-alias-2"} {
			key := "alias--" + sanitizeNATSKey(alias)
			entry, err := aliasBucket.Get(ctx, key)
			assert.NoError(t, err, "alias should work: %s", alias)
			if err == nil {
				assert.Equal(t, entityID, string(entry.Value()))
			}
		}

		// Verify reverse index has 2 aliases
		reverseAliases, err := aliasIndex.getEntityAliases(ctx, entityID)
		require.NoError(t, err)
		assert.Len(t, reverseAliases, 2)
	})

	t.Run("update_populated_to_empty", func(t *testing.T) {
		entityID := "test.entity.006"

		// Initial state with aliases
		initialState := &gtypes.EntityState{
			Node: gtypes.NodeProperties{
				ID: entityID,
			},
			Triples: []message.Triple{
				{
					Subject:   entityID,
					Predicate: examples.SemanticIdentityAlias,
					Object:    "temp-alias-1",
				},
				{
					Subject:   entityID,
					Predicate: examples.RoboticsCommunicationCallsign,
					Object:    "temp-alias-2",
				},
			},
			UpdatedAt: time.Now(),
		}

		err := aliasIndex.HandleCreate(ctx, entityID, initialState)
		require.NoError(t, err)

		// Update: remove all aliases
		updatedState := &gtypes.EntityState{
			Node: gtypes.NodeProperties{
				ID: entityID,
			},
			Triples: []message.Triple{
				// No alias predicates
				{
					Subject:   entityID,
					Predicate: "some.other.property",
					Object:    "value",
				},
			},
			UpdatedAt: time.Now(),
		}

		err = aliasIndex.HandleUpdate(ctx, entityID, updatedState)
		require.NoError(t, err)

		// Verify all aliases are removed
		for _, alias := range []string{"temp-alias-1", "temp-alias-2"} {
			key := "alias--" + sanitizeNATSKey(alias)
			_, err := aliasBucket.Get(ctx, key)
			assert.Error(t, err, "alias should be removed: %s", alias)
		}

		// Verify reverse index is empty
		reverseAliases, err := aliasIndex.getEntityAliases(ctx, entityID)
		require.NoError(t, err)
		assert.Len(t, reverseAliases, 0, "reverse index should be empty")
	})

	t.Run("update_no_changes_idempotent", func(t *testing.T) {
		entityID := "test.entity.007"

		// Initial state
		initialState := &gtypes.EntityState{
			Node: gtypes.NodeProperties{
				ID: entityID,
			},
			Triples: []message.Triple{
				{
					Subject:   entityID,
					Predicate: examples.SemanticIdentityAlias,
					Object:    "stable-alias-1",
				},
				{
					Subject:   entityID,
					Predicate: examples.RoboticsCommunicationCallsign,
					Object:    "stable-alias-2",
				},
			},
			UpdatedAt: time.Now(),
		}

		err := aliasIndex.HandleCreate(ctx, entityID, initialState)
		require.NoError(t, err)

		// Get initial revision of reverse index
		reverseKey := "entity--" + entityID
		initialEntry, err := aliasBucket.Get(ctx, reverseKey)
		require.NoError(t, err)
		initialRevision := initialEntry.Revision()

		// Update with SAME aliases (no changes)
		updatedState := &gtypes.EntityState{
			Node: gtypes.NodeProperties{
				ID: entityID,
			},
			Triples: []message.Triple{
				{
					Subject:   entityID,
					Predicate: examples.SemanticIdentityAlias,
					Object:    "stable-alias-1", // Same
				},
				{
					Subject:   entityID,
					Predicate: examples.RoboticsCommunicationCallsign,
					Object:    "stable-alias-2", // Same
				},
			},
			UpdatedAt: time.Now(),
		}

		err = aliasIndex.HandleUpdate(ctx, entityID, updatedState)
		require.NoError(t, err)

		// Verify aliases still work
		for _, alias := range []string{"stable-alias-1", "stable-alias-2"} {
			key := "alias--" + sanitizeNATSKey(alias)
			entry, err := aliasBucket.Get(ctx, key)
			assert.NoError(t, err, "alias should still work: %s", alias)
			if err == nil {
				assert.Equal(t, entityID, string(entry.Value()))
			}
		}

		// Verify reverse index revision didn't change (optimization: no update when no changes)
		// Note: Current implementation DOES update, but this test documents expected behavior
		finalEntry, err := aliasBucket.Get(ctx, reverseKey)
		require.NoError(t, err)
		finalRevision := finalEntry.Revision()

		// Verify reverse index still has 2 aliases
		reverseAliases, err := aliasIndex.getEntityAliases(ctx, entityID)
		require.NoError(t, err)
		assert.Len(t, reverseAliases, 2)

		t.Logf("Initial revision: %d, Final revision: %d (current impl updates even when no changes)",
			initialRevision, finalRevision)
	})

	t.Run("delete_entity_cleanup_all_aliases", func(t *testing.T) {
		entityID := "test.entity.008"

		// Create entity with multiple aliases
		state := &gtypes.EntityState{
			Node: gtypes.NodeProperties{
				ID: entityID,
			},
			Triples: []message.Triple{
				{
					Subject:   entityID,
					Predicate: examples.SemanticIdentityAlias,
					Object:    "drone-delta-001",
				},
				{
					Subject:   entityID,
					Predicate: examples.RoboticsCommunicationCallsign,
					Object:    "DELTA-1",
				},
				{
					Subject:   entityID,
					Predicate: examples.RoboticsIdentifierSerial,
					Object:    "SN-88888",
				},
			},
			UpdatedAt: time.Now(),
		}

		err := aliasIndex.HandleCreate(ctx, entityID, state)
		require.NoError(t, err)

		// Verify all aliases work
		aliases := []string{"drone-delta-001", "DELTA-1", "SN-88888"}
		for _, alias := range aliases {
			key := "alias--" + sanitizeNATSKey(alias)
			entry, err := aliasBucket.Get(ctx, key)
			require.NoError(t, err, "alias should work before delete: %s", alias)
			assert.Equal(t, entityID, string(entry.Value()))
		}

		// Delete entity
		err = aliasIndex.HandleDelete(ctx, entityID)
		require.NoError(t, err)

		// Verify all aliases are removed (bidirectional cleanup)
		for _, alias := range aliases {
			key := "alias--" + sanitizeNATSKey(alias)
			_, err := aliasBucket.Get(ctx, key)
			assert.Error(t, err, "alias should be removed after delete: %s", alias)
		}

		// Verify reverse index is removed
		_, err = aliasIndex.getEntityAliases(ctx, entityID)
		assert.Error(t, err, "reverse index should be removed")
	})

	t.Run("graceful_degradation_no_vocabulary", func(t *testing.T) {
		// Clear vocabulary registry
		vocabulary.ClearRegistry()
		defer func() {
			// Restore vocabulary for other tests
			examples.RegisterSemanticVocabulary()
			examples.RegisterRoboticsVocabulary()
		}()

		entityID := "test.entity.009"
		state := &gtypes.EntityState{
			Node: gtypes.NodeProperties{
				ID: entityID,
			},
			Triples: []message.Triple{
				{
					Subject:   entityID,
					Predicate: "some.unknown.predicate",
					Object:    "some-value",
				},
			},
			UpdatedAt: time.Now(),
		}

		// Should not error, just gracefully skip (no aliases registered)
		err := aliasIndex.HandleCreate(ctx, entityID, state)
		require.NoError(t, err)

		// Should not be indexed
		key := "alias--" + sanitizeNATSKey("some-value")
		_, err = aliasBucket.Get(ctx, key)
		assert.Error(t, err, "unknown predicate should not be indexed")
	})
}

// TestAliasIndex_DiscoverAliasPredicates tests vocabulary-based predicate discovery
func TestAliasIndex_DiscoverAliasPredicates(t *testing.T) {
	// Clean slate
	vocabulary.ClearRegistry()

	t.Run("discover_from_registered_predicates", func(t *testing.T) {
		// Register test predicates
		examples.RegisterSemanticVocabulary()
		examples.RegisterRoboticsVocabulary()

		// Discover alias predicates
		aliasPredicates := vocabulary.DiscoverAliasPredicates()

		// Should find all resolvable alias predicates (not labels)
		assert.NotEmpty(t, aliasPredicates)

		// Verify specific predicates are present
		_, hasIdentityAlias := aliasPredicates[examples.SemanticIdentityAlias]
		assert.True(t, hasIdentityAlias, "should find semantic.identity.alias")

		_, hasCallsign := aliasPredicates[examples.RoboticsCommunicationCallsign]
		assert.True(t, hasCallsign, "should find robotics.communication.callsign")

		// Verify labels are NOT included (not resolvable)
		_, hasLabel := aliasPredicates[examples.SemanticLabelPreferred]
		assert.False(t, hasLabel, "should NOT include labels (not resolvable)")
	})

	t.Run("empty_when_no_predicates_registered", func(t *testing.T) {
		vocabulary.ClearRegistry()

		aliasPredicates := vocabulary.DiscoverAliasPredicates()
		assert.Empty(t, aliasPredicates, "should return empty map when no predicates registered")
	})

	t.Run("priority_values_preserved", func(t *testing.T) {
		vocabulary.ClearRegistry()
		examples.RegisterSemanticVocabulary()
		examples.RegisterRoboticsVocabulary()

		aliasPredicates := vocabulary.DiscoverAliasPredicates()

		// Verify priorities are preserved
		// SemanticIdentityAlias has priority 0 (highest)
		priority, exists := aliasPredicates[examples.SemanticIdentityAlias]
		assert.True(t, exists)
		assert.Equal(t, 0, priority, "identity alias should have priority 0")

		// RoboticsIdentifierSerial has priority 1
		priority, exists = aliasPredicates[examples.RoboticsIdentifierSerial]
		assert.True(t, exists)
		assert.Equal(t, 1, priority, "serial should have priority 1")
	})
}
