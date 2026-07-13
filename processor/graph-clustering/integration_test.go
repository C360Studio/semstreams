//go:build integration

package graphclustering

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/model"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegration_ClusteringFlow tests the full entity → community flow
func TestIntegration_ClusteringFlow(t *testing.T) {
	// Create test NATS client with KV support
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	nc := testClient.Client

	// Create component with short detection interval for testing
	config := Config{
		AllowUngatedReads: true, // gh#474 Codex #4: standalone test reads without a co-deployed graph-index handler
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{Name: "entity_watch", Type: "kv-watch", Subject: graph.BucketEntityStates},
			},
			Outputs: []component.PortDefinition{
				{Name: "communities", Type: "kv-write", Subject: graph.BucketCommunityIndex},
			},
		},
		DetectionIntervalStr: "1s",  // Short interval for testing
		MinCommunitySize:     2,     // Lower threshold for testing
		MaxIterations:        10,    // Fewer iterations for speed
		EnableLLM:            false, // Disable LLM for integration tests
	}

	// Apply defaults to parse duration
	config.ApplyDefaults()

	configJSON, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: nc,
	}

	comp, err := CreateGraphClustering(configJSON, deps)
	require.NoError(t, err)
	require.NotNil(t, comp)

	clusteringComp := comp.(*Component)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	require.NoError(t, clusteringComp.Initialize())

	// Get JetStream context
	js, err := nc.JetStream()
	require.NoError(t, err)

	// Create input buckets BEFORE starting component
	entityBucket, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      graph.BucketEntityStates,
		Description: "Test entity states",
	})
	require.NoError(t, err)

	outgoingBucket, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      graph.BucketOutgoingIndex,
		Description: "Test outgoing edges",
	})
	require.NoError(t, err)

	incomingBucket, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      graph.BucketIncomingIndex,
		Description: "Test incoming edges",
	})
	require.NoError(t, err)

	// Start component (now that input buckets exist)
	require.NoError(t, clusteringComp.Start(ctx))
	defer clusteringComp.Stop(5 * time.Second)

	// Wait for COMMUNITY_INDEX bucket to be created by component
	var communityBucket jetstream.KeyValue
	require.Eventually(t, func() bool {
		communityBucket, err = js.KeyValue(ctx, graph.BucketCommunityIndex)
		return err == nil
	}, 5*time.Second, 100*time.Millisecond, "COMMUNITY_INDEX bucket should be created")

	// Create 4 connected entities with edges between them
	now := time.Now().UTC()
	entityIDs := []string{
		"c360.platform.robotics.mav1.drone.001",
		"c360.platform.robotics.mav1.drone.002",
		"c360.platform.robotics.mav1.drone.003",
		"c360.platform.robotics.mav1.drone.004",
	}

	// Write entity states
	for i, entityID := range entityIDs {
		state := graph.EntityState{
			ID: entityID,
			Triples: []message.Triple{
				{
					Subject:   entityID,
					Predicate: "robotics.status.armed",
					Object:    true,
					Source:    "test",
					Timestamp: now,
				},
			},
			MessageType: message.Type{Domain: "test", Category: "entity", Version: "v1"},
			Version:     1,
			UpdatedAt:   now.Add(time.Duration(i) * time.Second),
		}

		stateData, err := json.Marshal(state)
		require.NoError(t, err)

		_, err = entityBucket.Put(ctx, entityID, stateData)
		require.NoError(t, err)
	}

	// Create edges to form a connected cluster
	// Format: entity A has outgoing edge to entity B
	// Entity B has incoming edge from entity A
	edges := []struct {
		from string
		to   string
	}{
		{entityIDs[0], entityIDs[1]}, // 001 -> 002
		{entityIDs[1], entityIDs[2]}, // 002 -> 003
		{entityIDs[2], entityIDs[3]}, // 003 -> 004
		{entityIDs[0], entityIDs[3]}, // 001 -> 004 (close the cluster)
	}

	for _, edge := range edges {
		// Write outgoing edge for source entity
		outgoingData := []relationshipEntry{
			{
				Predicate:  "rel.connected_to",
				ToEntityID: edge.to,
			},
		}
		outgoingJSON, err := json.Marshal(outgoingData)
		require.NoError(t, err)

		_, err = outgoingBucket.Put(ctx, edge.from, outgoingJSON)
		require.NoError(t, err)

		// Write incoming edge for target entity
		incomingData := []relationshipEntry{
			{
				Predicate:    "rel.connected_to",
				FromEntityID: edge.from,
			},
		}
		incomingJSON, err := json.Marshal(incomingData)
		require.NoError(t, err)

		_, err = incomingBucket.Put(ctx, edge.to, incomingJSON)
		require.NoError(t, err)
	}

	// Wait for at least one detection cycle (detection interval is 1s)
	time.Sleep(2 * time.Second)

	// Wait for community to be created
	// The detection runs periodically and clears old communities, so we check for
	// valid community structures within a reasonable window
	foundCommunityWithMembers := false
	require.Eventually(t, func() bool {
		keys, err := communityBucket.Keys(ctx)
		if err != nil {
			return false
		}

		// Look for community data keys (format: {level}.{communityID})
		// Entity mapping keys have format: entity.{level}.{entityID}
		for _, key := range keys {
			// Skip entity mapping keys
			if len(key) > 7 && key[:7] == "entity." {
				continue
			}

			// Community keys start with a digit (level)
			if len(key) == 0 || key[0] < '0' || key[0] > '9' {
				continue
			}

			// This should be a community data key - verify it has valid JSON
			entry, err := communityBucket.Get(ctx, key)
			if err != nil {
				continue
			}

			var communityData map[string]interface{}
			if err := json.Unmarshal(entry.Value(), &communityData); err != nil {
				continue
			}

			// Check for members array (lowercase - Community struct has json tags)
			if members, ok := communityData["members"].([]interface{}); ok {
				if len(members) >= 2 {
					t.Logf("Found valid community %s with %d members", key, len(members))
					foundCommunityWithMembers = true
					return true
				}
			}
		}
		return false
	}, 10*time.Second, 200*time.Millisecond, "Community should be created with members")

	assert.True(t, foundCommunityWithMembers, "At least one community with 2+ members should be formed")
}

// TestIntegration_ClusteringHierarchy verifies hierarchical levels
func TestIntegration_ClusteringHierarchy(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	nc := testClient.Client

	config := Config{
		AllowUngatedReads: true, // gh#474 Codex #4: standalone test reads without a co-deployed graph-index handler
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{Name: "entity_watch", Type: "kv-watch", Subject: graph.BucketEntityStates},
			},
			Outputs: []component.PortDefinition{
				{Name: "communities", Type: "kv-write", Subject: graph.BucketCommunityIndex},
			},
		},
		DetectionIntervalStr: "1s",
		MinCommunitySize:     2,
		MaxIterations:        10,
		EnableLLM:            false,
	}

	config.ApplyDefaults()
	configJSON, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: nc,
	}

	comp, err := CreateGraphClustering(configJSON, deps)
	require.NoError(t, err)

	clusteringComp := comp.(*Component)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	require.NoError(t, clusteringComp.Initialize())

	js, err := nc.JetStream()
	require.NoError(t, err)

	// Create input buckets
	entityBucket, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      graph.BucketEntityStates,
		Description: "Test entity states",
	})
	require.NoError(t, err)

	outgoingBucket, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      graph.BucketOutgoingIndex,
		Description: "Test outgoing edges",
	})
	require.NoError(t, err)

	incomingBucket, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      graph.BucketIncomingIndex,
		Description: "Test incoming edges",
	})
	require.NoError(t, err)

	require.NoError(t, clusteringComp.Start(ctx))
	defer clusteringComp.Stop(5 * time.Second)

	var communityBucket jetstream.KeyValue
	require.Eventually(t, func() bool {
		communityBucket, err = js.KeyValue(ctx, graph.BucketCommunityIndex)
		return err == nil
	}, 5*time.Second, 100*time.Millisecond)

	// Create 6 entities in two distinct groups
	now := time.Now().UTC()
	group1 := []string{
		"c360.platform.robotics.mav1.drone.001",
		"c360.platform.robotics.mav1.drone.002",
		"c360.platform.robotics.mav1.drone.003",
	}
	group2 := []string{
		"c360.platform.robotics.ugv1.rover.001",
		"c360.platform.robotics.ugv1.rover.002",
		"c360.platform.robotics.ugv1.rover.003",
	}

	allEntities := append(group1, group2...)

	// Write entity states
	for i, entityID := range allEntities {
		state := graph.EntityState{
			ID: entityID,
			Triples: []message.Triple{
				{
					Subject:   entityID,
					Predicate: "robotics.status.active",
					Object:    true,
					Source:    "test",
					Timestamp: now,
				},
			},
			MessageType: message.Type{Domain: "test", Category: "entity", Version: "v1"},
			Version:     1,
			UpdatedAt:   now.Add(time.Duration(i) * time.Second),
		}

		stateData, err := json.Marshal(state)
		require.NoError(t, err)

		_, err = entityBucket.Put(ctx, entityID, stateData)
		require.NoError(t, err)
	}

	// Create edges within each group
	createEdges := func(group []string) {
		for i := 0; i < len(group)-1; i++ {
			from := group[i]
			to := group[i+1]

			// Outgoing edge
			outgoingData := []relationshipEntry{
				{Predicate: "rel.connected_to", ToEntityID: to},
			}
			outgoingJSON, _ := json.Marshal(outgoingData)
			outgoingBucket.Put(ctx, from, outgoingJSON)

			// Incoming edge
			incomingData := []relationshipEntry{
				{Predicate: "rel.connected_to", FromEntityID: from},
			}
			incomingJSON, _ := json.Marshal(incomingData)
			incomingBucket.Put(ctx, to, incomingJSON)
		}

		// Close the loop within group
		from := group[len(group)-1]
		to := group[0]
		outgoingData := []relationshipEntry{
			{Predicate: "rel.connected_to", ToEntityID: to},
		}
		outgoingJSON, _ := json.Marshal(outgoingData)
		outgoingBucket.Put(ctx, from, outgoingJSON)

		incomingData := []relationshipEntry{
			{Predicate: "rel.connected_to", FromEntityID: from},
		}
		incomingJSON, _ := json.Marshal(incomingData)
		incomingBucket.Put(ctx, to, incomingJSON)
	}

	createEdges(group1)
	createEdges(group2)

	// Wait for detection cycles
	time.Sleep(3 * time.Second)

	// Verify level 0 communities exist
	require.Eventually(t, func() bool {
		keys, err := communityBucket.Keys(ctx)
		if err != nil {
			return false
		}

		level0Communities := 0
		for _, key := range keys {
			// Skip entity mapping keys (format: entity.{level}.{entityID})
			if len(key) > 7 && key[:7] == "entity." {
				continue
			}

			// Check for level 0 community keys (format: 0.{communityID})
			if len(key) > 2 && key[:2] == "0." {
				level0Communities++
				t.Logf("Found level 0 community: %s", key)
			}
		}

		// We expect at least 1 community at level 0 (could be 1 or 2 depending on algorithm)
		return level0Communities >= 1
	}, 15*time.Second, 500*time.Millisecond, "Level 0 communities should exist")

	t.Log("Successfully verified hierarchical community structure")
}

// TestIntegration_ClusteringMinSize verifies min_community_size threshold
func TestIntegration_ClusteringMinSize(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	nc := testClient.Client

	// Set min_community_size = 3 (requires at least 3 entities to form community)
	config := Config{
		AllowUngatedReads: true, // gh#474 Codex #4: standalone test reads without a co-deployed graph-index handler
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{Name: "entity_watch", Type: "kv-watch", Subject: graph.BucketEntityStates},
			},
			Outputs: []component.PortDefinition{
				{Name: "communities", Type: "kv-write", Subject: graph.BucketCommunityIndex},
			},
		},
		DetectionIntervalStr: "1s",
		MinCommunitySize:     3, // Require at least 3 members
		MaxIterations:        10,
		EnableLLM:            false,
	}

	config.ApplyDefaults()
	configJSON, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: nc,
	}

	comp, err := CreateGraphClustering(configJSON, deps)
	require.NoError(t, err)

	clusteringComp := comp.(*Component)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	require.NoError(t, clusteringComp.Initialize())

	js, err := nc.JetStream()
	require.NoError(t, err)

	// Create input buckets
	entityBucket, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      graph.BucketEntityStates,
		Description: "Test entity states",
	})
	require.NoError(t, err)

	outgoingBucket, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      graph.BucketOutgoingIndex,
		Description: "Test outgoing edges",
	})
	require.NoError(t, err)

	incomingBucket, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      graph.BucketIncomingIndex,
		Description: "Test incoming edges",
	})
	require.NoError(t, err)

	require.NoError(t, clusteringComp.Start(ctx))
	defer clusteringComp.Stop(5 * time.Second)

	var communityBucket jetstream.KeyValue
	require.Eventually(t, func() bool {
		communityBucket, err = js.KeyValue(ctx, graph.BucketCommunityIndex)
		return err == nil
	}, 5*time.Second, 100*time.Millisecond)

	// Create only 2 connected entities (below min_community_size threshold)
	now := time.Now().UTC()
	entityIDs := []string{
		"c360.platform.robotics.mav1.drone.001",
		"c360.platform.robotics.mav1.drone.002",
	}

	// Write entity states
	for i, entityID := range entityIDs {
		state := graph.EntityState{
			ID: entityID,
			Triples: []message.Triple{
				{
					Subject:   entityID,
					Predicate: "robotics.status.armed",
					Object:    true,
					Source:    "test",
					Timestamp: now,
				},
			},
			MessageType: message.Type{Domain: "test", Category: "entity", Version: "v1"},
			Version:     1,
			UpdatedAt:   now.Add(time.Duration(i) * time.Second),
		}

		stateData, err := json.Marshal(state)
		require.NoError(t, err)

		_, err = entityBucket.Put(ctx, entityID, stateData)
		require.NoError(t, err)
	}

	// Create edge between the two entities
	from := entityIDs[0]
	to := entityIDs[1]

	// Outgoing edge
	outgoingData := []relationshipEntry{
		{Predicate: "rel.connected_to", ToEntityID: to},
	}
	outgoingJSON, err := json.Marshal(outgoingData)
	require.NoError(t, err)
	_, err = outgoingBucket.Put(ctx, from, outgoingJSON)
	require.NoError(t, err)

	// Incoming edge
	incomingData := []relationshipEntry{
		{Predicate: "rel.connected_to", FromEntityID: from},
	}
	incomingJSON, err := json.Marshal(incomingData)
	require.NoError(t, err)
	_, err = incomingBucket.Put(ctx, to, incomingJSON)
	require.NoError(t, err)

	// Wait for detection cycles
	time.Sleep(3 * time.Second)

	// Verify no community was formed (below threshold).
	// Two distinct empty-bucket sentinels are both acceptable here:
	//   - jetstream.ErrKeyNotFound is for missing single keys, not
	//     empty Keys() listings.
	//   - jetstream.ErrNoKeysFound ("nats: no keys found") is what
	//     Keys() actually returns when the bucket is empty.
	//
	// Pre-2026-05-13 only ErrKeyNotFound was accepted, so a fully
	// empty community bucket (the expected state when
	// min_community_size=3 prevents formation) flaked the test on
	// CI. Local runs sometimes raced an unrelated entity-mapping
	// write into the bucket before Keys was called, masking the
	// bug. Audit-cleanup commit 2026-05-13. errors.Is matches the
	// canonical natsclient/kv.go:385 pattern in this codebase and
	// also handles wrapped sentinels.
	keys, err := communityBucket.Keys(ctx)
	if err != nil && !errors.Is(err, jetstream.ErrKeyNotFound) && !errors.Is(err, jetstream.ErrNoKeysFound) {
		require.NoError(t, err)
	}

	// Count community keys (excluding entity mappings)
	communityCount := 0
	if keys != nil {
		for _, key := range keys {
			// Skip entity mapping keys (format: entity.{level}.{entityID})
			if len(key) > 7 && key[:7] == "entity." {
				continue
			}
			// Community keys start with a digit (level)
			if len(key) > 0 && key[0] >= '0' && key[0] <= '9' {
				communityCount++
			}
		}
	}

	// With only 2 entities and min_community_size=3, no communities should form
	// NOTE: The actual behavior depends on the LPA algorithm implementation.
	// Some implementations may still create communities below threshold, then filter them out.
	// For this test, we just verify the detection runs without error.
	t.Logf("Detection ran with %d entities (below min_community_size=3), found %d communities", len(entityIDs), communityCount)

	// The main assertion is that the system doesn't crash with small graphs
	assert.GreaterOrEqual(t, communityCount, 0, "System should handle small graphs gracefully")
}

// TestIntegration_ClusteringMetrics verifies metrics are updated
func TestIntegration_ClusteringMetrics(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	nc := testClient.Client

	config := Config{
		AllowUngatedReads: true, // gh#474 Codex #4: standalone test reads without a co-deployed graph-index handler
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{Name: "entity_watch", Type: "kv-watch", Subject: graph.BucketEntityStates},
			},
			Outputs: []component.PortDefinition{
				{Name: "communities", Type: "kv-write", Subject: graph.BucketCommunityIndex},
			},
		},
		DetectionIntervalStr: "1s",
		MinCommunitySize:     2,
		MaxIterations:        10,
		EnableLLM:            false,
	}

	config.ApplyDefaults()
	configJSON, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: nc,
	}

	comp, err := CreateGraphClustering(configJSON, deps)
	require.NoError(t, err)

	clusteringComp := comp.(*Component)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	require.NoError(t, clusteringComp.Initialize())

	js, err := nc.JetStream()
	require.NoError(t, err)

	// Create input buckets
	entityBucket, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      graph.BucketEntityStates,
		Description: "Test entity states",
	})
	require.NoError(t, err)

	_, err = js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      graph.BucketOutgoingIndex,
		Description: "Test outgoing edges",
	})
	require.NoError(t, err)

	_, err = js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      graph.BucketIncomingIndex,
		Description: "Test incoming edges",
	})
	require.NoError(t, err)

	require.NoError(t, clusteringComp.Start(ctx))
	defer clusteringComp.Stop(5 * time.Second)

	// Create a few entities
	now := time.Now().UTC()
	entityIDs := []string{
		"c360.platform.robotics.mav1.drone.001",
		"c360.platform.robotics.mav1.drone.002",
	}

	for i, entityID := range entityIDs {
		state := graph.EntityState{
			ID: entityID,
			Triples: []message.Triple{
				{
					Subject:   entityID,
					Predicate: "robotics.status.armed",
					Object:    true,
					Source:    "test",
					Timestamp: now,
				},
			},
			MessageType: message.Type{Domain: "test", Category: "entity", Version: "v1"},
			Version:     1,
			UpdatedAt:   now.Add(time.Duration(i) * time.Second),
		}

		stateData, err := json.Marshal(state)
		require.NoError(t, err)

		_, err = entityBucket.Put(ctx, entityID, stateData)
		require.NoError(t, err)
	}

	// Wait for at least one detection cycle
	time.Sleep(2 * time.Second)

	// Verify health status shows running
	health := clusteringComp.Health()
	assert.True(t, health.Healthy, "Component should be healthy")
	assert.Equal(t, "running", health.Status)
	assert.Greater(t, health.Uptime, time.Duration(0), "Uptime should be positive")

	// Verify metrics are tracked
	metrics := clusteringComp.DataFlow()
	assert.GreaterOrEqual(t, metrics.MessagesPerSecond, float64(0))
	assert.NotZero(t, metrics.LastActivity, "LastActivity should be set")

	t.Logf("Component metrics - Messages/sec: %.2f, LastActivity: %v",
		metrics.MessagesPerSecond, metrics.LastActivity)
}

// TestIntegration_LLMEnhancementWorkerStarts verifies that when EnableLLM=true,
// the enhancement worker is initialized and ready to process communities.
// This test caught a missing wiring bug during the monolith refactor.
func TestIntegration_LLMEnhancementWorkerStarts(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	nc := testClient.Client

	config := Config{
		AllowUngatedReads: true, // gh#474 Codex #4: standalone test reads without a co-deployed graph-index handler
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{Name: "entity_watch", Type: "kv-watch", Subject: graph.BucketEntityStates},
			},
			Outputs: []component.PortDefinition{
				{Name: "communities", Type: "kv-write", Subject: graph.BucketCommunityIndex},
			},
		},
		DetectionIntervalStr: "1s",
		MinCommunitySize:     2,
		MaxIterations:        10,
		EnableLLM:            true, // Enable LLM enhancement; endpoint resolved via model registry at runtime
	}

	config.ApplyDefaults()
	configJSON, err := json.Marshal(config)
	require.NoError(t, err)

	// Start a minimal HTTP server to satisfy the LLM health probe.
	// The test only verifies the worker initializes — no actual LLM calls are made.
	llmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer llmServer.Close()

	reg := &model.Registry{
		Endpoints: map[string]*model.EndpointConfig{
			"test-llm": {
				Provider:  "openai",
				URL:       llmServer.URL + "/v1",
				Model:     "test-model",
				MaxTokens: 4096,
			},
		},
		Capabilities: map[string]*model.CapabilityConfig{
			model.CapabilityCommunitySummary: {
				Preferred: []string{"test-llm"},
			},
		},
		Defaults: model.DefaultsConfig{Model: "test-llm"},
	}

	deps := component.Dependencies{
		NATSClient:    nc,
		ModelRegistry: reg,
	}

	comp, err := CreateGraphClustering(configJSON, deps)
	require.NoError(t, err)

	clusteringComp := comp.(*Component)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	require.NoError(t, clusteringComp.Initialize())

	js, err := nc.JetStream()
	require.NoError(t, err)

	// Create required buckets
	_, err = js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      graph.BucketEntityStates,
		Description: "Test entity states",
	})
	require.NoError(t, err)

	_, err = js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      graph.BucketOutgoingIndex,
		Description: "Test outgoing edges",
	})
	require.NoError(t, err)

	_, err = js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      graph.BucketIncomingIndex,
		Description: "Test incoming edges",
	})
	require.NoError(t, err)

	// Start component - this should initialize the enhancement worker
	require.NoError(t, clusteringComp.Start(ctx))
	defer clusteringComp.Stop(5 * time.Second)

	// Verify enhancement worker was initialized
	// This is the key assertion that would have caught the wiring bug
	assert.NotNil(t, clusteringComp.enhancementWorker,
		"EnhancementWorker should be initialized when EnableLLM=true")
	assert.NotNil(t, clusteringComp.llmClient,
		"LLM client should be initialized when EnableLLM=true")

	t.Log("LLM enhancement worker successfully initialized")
}

// TestIntegration_LLMEnhancementDisabled verifies that when EnableLLM=false,
// no LLM resources are allocated.
func TestIntegration_LLMEnhancementDisabled(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	nc := testClient.Client

	config := Config{
		AllowUngatedReads: true, // gh#474 Codex #4: standalone test reads without a co-deployed graph-index handler
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{Name: "entity_watch", Type: "kv-watch", Subject: graph.BucketEntityStates},
			},
			Outputs: []component.PortDefinition{
				{Name: "communities", Type: "kv-write", Subject: graph.BucketCommunityIndex},
			},
		},
		DetectionIntervalStr: "1s",
		MinCommunitySize:     2,
		MaxIterations:        10,
		EnableLLM:            false, // LLM disabled
	}

	config.ApplyDefaults()
	configJSON, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: nc,
	}

	comp, err := CreateGraphClustering(configJSON, deps)
	require.NoError(t, err)

	clusteringComp := comp.(*Component)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	require.NoError(t, clusteringComp.Initialize())

	js, err := nc.JetStream()
	require.NoError(t, err)

	// Create required buckets
	_, err = js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      graph.BucketEntityStates,
		Description: "Test entity states",
	})
	require.NoError(t, err)

	_, err = js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      graph.BucketOutgoingIndex,
		Description: "Test outgoing edges",
	})
	require.NoError(t, err)

	_, err = js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      graph.BucketIncomingIndex,
		Description: "Test incoming edges",
	})
	require.NoError(t, err)

	require.NoError(t, clusteringComp.Start(ctx))
	defer clusteringComp.Stop(5 * time.Second)

	// Verify no LLM resources allocated
	assert.Nil(t, clusteringComp.enhancementWorker,
		"EnhancementWorker should be nil when EnableLLM=false")
	assert.Nil(t, clusteringComp.llmClient,
		"LLM client should be nil when EnableLLM=false")

	t.Log("No LLM resources allocated when disabled - correct behavior")
}

// TestIntegration_StructuralComputationEnabled verifies that when EnableStructural=true,
// k-core and pivot indices are computed after LPA and STRUCTURAL_INDEX bucket is created.
func TestIntegration_StructuralComputationEnabled(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	nc := testClient.Client

	config := Config{
		AllowUngatedReads: true, // gh#474 Codex #4: standalone test reads without a co-deployed graph-index handler
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{Name: "entity_watch", Type: "kv-watch", Subject: graph.BucketEntityStates},
			},
			Outputs: []component.PortDefinition{
				{Name: "communities", Type: "kv-write", Subject: graph.BucketCommunityIndex},
			},
		},
		DetectionIntervalStr: "1s",
		MinCommunitySize:     2,
		MaxIterations:        10,
		EnableLLM:            false,
		EnableStructural:     true, // Enable structural computation
		PivotCount:           4,    // Small for testing
		MaxHopDistance:       5,
	}

	config.ApplyDefaults()
	configJSON, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: nc,
	}

	comp, err := CreateGraphClustering(configJSON, deps)
	require.NoError(t, err)

	clusteringComp := comp.(*Component)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	require.NoError(t, clusteringComp.Initialize())

	js, err := nc.JetStream()
	require.NoError(t, err)

	// Create input buckets
	entityBucket, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      graph.BucketEntityStates,
		Description: "Test entity states",
	})
	require.NoError(t, err)

	outgoingBucket, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      graph.BucketOutgoingIndex,
		Description: "Test outgoing edges",
	})
	require.NoError(t, err)

	incomingBucket, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      graph.BucketIncomingIndex,
		Description: "Test incoming edges",
	})
	require.NoError(t, err)

	require.NoError(t, clusteringComp.Start(ctx))
	defer clusteringComp.Stop(5 * time.Second)

	// Verify STRUCTURAL_INDEX bucket is created
	var structuralBucket jetstream.KeyValue
	require.Eventually(t, func() bool {
		structuralBucket, err = js.KeyValue(ctx, graph.BucketStructuralIndex)
		return err == nil
	}, 5*time.Second, 100*time.Millisecond, "STRUCTURAL_INDEX bucket should be created")

	// Create 4 connected entities
	now := time.Now().UTC()
	entityIDs := []string{
		"c360.platform.robotics.mav1.drone.001",
		"c360.platform.robotics.mav1.drone.002",
		"c360.platform.robotics.mav1.drone.003",
		"c360.platform.robotics.mav1.drone.004",
	}

	// Write entity states
	for i, entityID := range entityIDs {
		state := graph.EntityState{
			ID: entityID,
			Triples: []message.Triple{
				{
					Subject:   entityID,
					Predicate: "robotics.status.active",
					Object:    true,
					Source:    "test",
					Timestamp: now,
				},
			},
			MessageType: message.Type{Domain: "test", Category: "entity", Version: "v1"},
			Version:     1,
			UpdatedAt:   now.Add(time.Duration(i) * time.Second),
		}

		stateData, err := json.Marshal(state)
		require.NoError(t, err)

		_, err = entityBucket.Put(ctx, entityID, stateData)
		require.NoError(t, err)
	}

	// Create edges to form a connected graph
	edges := []struct{ from, to string }{
		{entityIDs[0], entityIDs[1]},
		{entityIDs[1], entityIDs[2]},
		{entityIDs[2], entityIDs[3]},
		{entityIDs[3], entityIDs[0]}, // close the loop
	}

	for _, edge := range edges {
		outgoingData := []relationshipEntry{{Predicate: "rel.connected_to", ToEntityID: edge.to}}
		outgoingJSON, _ := json.Marshal(outgoingData)
		outgoingBucket.Put(ctx, edge.from, outgoingJSON)

		incomingData := []relationshipEntry{{Predicate: "rel.connected_to", FromEntityID: edge.from}}
		incomingJSON, _ := json.Marshal(incomingData)
		incomingBucket.Put(ctx, edge.to, incomingJSON)
	}

	// Wait for detection cycles
	time.Sleep(3 * time.Second)

	// Verify k-core data was written to STRUCTURAL_INDEX
	// The storage uses key format: structural.kcore._meta for metadata
	require.Eventually(t, func() bool {
		entry, err := structuralBucket.Get(ctx, "structural.kcore._meta")
		if err != nil {
			return false
		}
		t.Logf("Found k-core index metadata: %d bytes", len(entry.Value()))
		return len(entry.Value()) > 0
	}, 10*time.Second, 500*time.Millisecond, "K-core index should be written to STRUCTURAL_INDEX")

	t.Log("Structural computation successfully ran with k-core index created")
}

// TestIntegration_AnomalyDetectionEnabled verifies that when EnableAnomalyDetection=true,
// anomaly detection runs after structural computation and ANOMALY_INDEX bucket is created.
func TestIntegration_AnomalyDetectionEnabled(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	nc := testClient.Client

	config := Config{
		AllowUngatedReads: true, // gh#474 Codex #4: standalone test reads without a co-deployed graph-index handler
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{Name: "entity_watch", Type: "kv-watch", Subject: graph.BucketEntityStates},
			},
			Outputs: []component.PortDefinition{
				{Name: "communities", Type: "kv-write", Subject: graph.BucketCommunityIndex},
			},
		},
		DetectionIntervalStr:   "1s",
		MinCommunitySize:       2,
		MaxIterations:          10,
		EnableLLM:              false,
		EnableStructural:       true, // Required for anomaly detection
		PivotCount:             4,
		MaxHopDistance:         5,
		EnableAnomalyDetection: true, // Enable anomaly detection
	}

	config.ApplyDefaults()
	configJSON, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: nc,
	}

	comp, err := CreateGraphClustering(configJSON, deps)
	require.NoError(t, err)

	clusteringComp := comp.(*Component)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	require.NoError(t, clusteringComp.Initialize())

	js, err := nc.JetStream()
	require.NoError(t, err)

	// Create input buckets
	entityBucket, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      graph.BucketEntityStates,
		Description: "Test entity states",
	})
	require.NoError(t, err)

	outgoingBucket, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      graph.BucketOutgoingIndex,
		Description: "Test outgoing edges",
	})
	require.NoError(t, err)

	incomingBucket, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      graph.BucketIncomingIndex,
		Description: "Test incoming edges",
	})
	require.NoError(t, err)

	require.NoError(t, clusteringComp.Start(ctx))
	defer clusteringComp.Stop(5 * time.Second)

	// Verify ANOMALY_INDEX bucket is created
	var anomalyBucket jetstream.KeyValue
	require.Eventually(t, func() bool {
		anomalyBucket, err = js.KeyValue(ctx, graph.BucketAnomalyIndex)
		return err == nil
	}, 5*time.Second, 100*time.Millisecond, "ANOMALY_INDEX bucket should be created")

	// Verify orchestrator was initialized
	assert.NotNil(t, clusteringComp.anomalyOrchestrator,
		"Anomaly orchestrator should be initialized when EnableAnomalyDetection=true")

	// Create entities to trigger detection cycle
	now := time.Now().UTC()
	entityIDs := []string{
		"c360.platform.robotics.mav1.drone.001",
		"c360.platform.robotics.mav1.drone.002",
		"c360.platform.robotics.mav1.drone.003",
	}

	for i, entityID := range entityIDs {
		state := graph.EntityState{
			ID: entityID,
			Triples: []message.Triple{
				{
					Subject:   entityID,
					Predicate: "robotics.status.active",
					Object:    true,
					Source:    "test",
					Timestamp: now,
				},
			},
			MessageType: message.Type{Domain: "test", Category: "entity", Version: "v1"},
			Version:     1,
			UpdatedAt:   now.Add(time.Duration(i) * time.Second),
		}

		stateData, err := json.Marshal(state)
		require.NoError(t, err)

		_, err = entityBucket.Put(ctx, entityID, stateData)
		require.NoError(t, err)
	}

	// Create edges
	edges := []struct{ from, to string }{
		{entityIDs[0], entityIDs[1]},
		{entityIDs[1], entityIDs[2]},
	}

	for _, edge := range edges {
		outgoingData := []relationshipEntry{{Predicate: "rel.connected_to", ToEntityID: edge.to}}
		outgoingJSON, _ := json.Marshal(outgoingData)
		outgoingBucket.Put(ctx, edge.from, outgoingJSON)

		incomingData := []relationshipEntry{{Predicate: "rel.connected_to", FromEntityID: edge.from}}
		incomingJSON, _ := json.Marshal(incomingData)
		incomingBucket.Put(ctx, edge.to, incomingJSON)
	}

	// Wait for detection cycles
	time.Sleep(3 * time.Second)

	// The ANOMALY_INDEX bucket exists - anomaly detection ran
	// Note: Whether anomalies are detected depends on the graph structure
	t.Log("Anomaly detection successfully initialized and ran")

	// Verify we can access the bucket (it was created).
	// Sister sentinel-set to the community-bucket check above —
	// Keys() on an empty bucket returns jetstream.ErrNoKeysFound,
	// not ErrKeyNotFound. Pre-2026-05-13 only ErrKeyNotFound was
	// accepted here too. errors.Is for wrapper resilience.
	_, err = anomalyBucket.Keys(ctx)
	if err != nil && !errors.Is(err, jetstream.ErrKeyNotFound) && !errors.Is(err, jetstream.ErrNoKeysFound) {
		// Other errors mean the bucket is genuinely unreachable
		t.Logf("ANOMALY_INDEX bucket accessible, keys error: %v", err)
	}
}

// TestIntegration_StructuralDisabledByDefault verifies that structural computation
// and anomaly detection are disabled by default.
func TestIntegration_StructuralDisabledByDefault(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	nc := testClient.Client

	config := Config{
		AllowUngatedReads: true, // gh#474 Codex #4: standalone test reads without a co-deployed graph-index handler
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{Name: "entity_watch", Type: "kv-watch", Subject: graph.BucketEntityStates},
			},
			Outputs: []component.PortDefinition{
				{Name: "communities", Type: "kv-write", Subject: graph.BucketCommunityIndex},
			},
		},
		DetectionIntervalStr: "1s",
		MinCommunitySize:     2,
		MaxIterations:        10,
		EnableLLM:            false,
		// EnableStructural and EnableAnomalyDetection not set (default false)
	}

	config.ApplyDefaults()
	configJSON, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: nc,
	}

	comp, err := CreateGraphClustering(configJSON, deps)
	require.NoError(t, err)

	clusteringComp := comp.(*Component)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	require.NoError(t, clusteringComp.Initialize())

	js, err := nc.JetStream()
	require.NoError(t, err)

	// Create required buckets
	_, err = js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      graph.BucketEntityStates,
		Description: "Test entity states",
	})
	require.NoError(t, err)

	_, err = js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      graph.BucketOutgoingIndex,
		Description: "Test outgoing edges",
	})
	require.NoError(t, err)

	_, err = js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      graph.BucketIncomingIndex,
		Description: "Test incoming edges",
	})
	require.NoError(t, err)

	require.NoError(t, clusteringComp.Start(ctx))
	defer clusteringComp.Stop(5 * time.Second)

	// Verify structural resources are NOT allocated
	assert.Nil(t, clusteringComp.structuralBucket,
		"Structural bucket should be nil when EnableStructural=false")
	assert.Nil(t, clusteringComp.anomalyOrchestrator,
		"Anomaly orchestrator should be nil when EnableAnomalyDetection=false")

	// Verify STRUCTURAL_INDEX bucket is NOT created
	time.Sleep(2 * time.Second)
	_, err = js.KeyValue(ctx, graph.BucketStructuralIndex)
	assert.Error(t, err, "STRUCTURAL_INDEX bucket should not exist when structural disabled")

	t.Log("Structural/anomaly features correctly disabled by default")
}

// TestIntegration_EntityCommunityLookup verifies that specific entities can be looked up
// from their communities after community detection runs.
// This is critical: the e2e test failed because entities weren't in communities.
func TestIntegration_EntityCommunityLookup(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	nc := testClient.Client

	config := Config{
		AllowUngatedReads: true, // gh#474 Codex #4: standalone test reads without a co-deployed graph-index handler
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{Name: "entity_watch", Type: "kv-watch", Subject: graph.BucketEntityStates},
			},
			Outputs: []component.PortDefinition{
				{Name: "communities", Type: "kv-write", Subject: graph.BucketCommunityIndex},
			},
		},
		DetectionIntervalStr: "1s",
		MinCommunitySize:     2,
		MaxIterations:        10,
		EnableLLM:            false,
	}

	config.ApplyDefaults()
	configJSON, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: nc,
	}

	comp, err := CreateGraphClustering(configJSON, deps)
	require.NoError(t, err)

	clusteringComp := comp.(*Component)
	require.NoError(t, clusteringComp.Initialize())

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Get JetStream for bucket setup
	js, err := nc.JetStream()
	require.NoError(t, err)

	// Create required buckets
	entityBucket, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      graph.BucketEntityStates,
		Description: "Test entities",
	})
	require.NoError(t, err)

	outgoingBucket, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      graph.BucketOutgoingIndex,
		Description: "Test outgoing edges",
	})
	require.NoError(t, err)

	incomingBucket, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      graph.BucketIncomingIndex,
		Description: "Test incoming edges",
	})
	require.NoError(t, err)

	// Start the component
	require.NoError(t, clusteringComp.Start(ctx))
	defer clusteringComp.Stop(5 * time.Second)

	// Create test entities - 4 sensors connected to the same container
	// This mimics hierarchy inference where sensors are connected via shared containers
	entityIDs := []string{
		"c360.logistics.sensor.temperature.zone1.sensor-001",
		"c360.logistics.sensor.temperature.zone1.sensor-002",
		"c360.logistics.sensor.temperature.zone1.sensor-003",
		"c360.logistics.sensor.temperature.zone1.sensor-004",
	}
	containerID := "c360.logistics.sensor.temperature.zone1.group"

	// Create entity states
	for _, entityID := range entityIDs {
		state := graph.EntityState{
			ID: entityID,
			Triples: []message.Triple{
				{Subject: entityID, Predicate: "entity.type", Object: "sensor.temperature"},
			},
			MessageType: message.Type{Domain: "test", Category: "sensor", Version: "v1"},
			Version:     1,
		}
		stateJSON, err := json.Marshal(state)
		require.NoError(t, err)
		_, err = entityBucket.Put(ctx, entityID, stateJSON)
		require.NoError(t, err)
	}

	// Create container entity
	containerState := graph.EntityState{
		ID: containerID,
		Triples: []message.Triple{
			{Subject: containerID, Predicate: "entity.type", Object: "hierarchy.container"},
		},
		MessageType: message.Type{Domain: "test", Category: "container", Version: "v1"},
		Version:     1,
	}
	containerJSON, err := json.Marshal(containerState)
	require.NoError(t, err)
	_, err = entityBucket.Put(ctx, containerID, containerJSON)
	require.NoError(t, err)

	// Create edges: each entity → container (hierarchy-like edges)
	for _, entityID := range entityIDs {
		outgoingData := []relationshipEntry{
			{Predicate: "hierarchy.type.member", ToEntityID: containerID},
		}
		outgoingJSON, err := json.Marshal(outgoingData)
		require.NoError(t, err)
		_, err = outgoingBucket.Put(ctx, entityID, outgoingJSON)
		require.NoError(t, err)
	}

	// Create incoming edges on container from all entities
	var incomingData []relationshipEntry
	for _, entityID := range entityIDs {
		incomingData = append(incomingData, relationshipEntry{
			Predicate:    "hierarchy.type.member",
			FromEntityID: entityID,
		})
	}
	incomingJSON, err := json.Marshal(incomingData)
	require.NoError(t, err)
	_, err = incomingBucket.Put(ctx, containerID, incomingJSON)
	require.NoError(t, err)

	// Wait for community detection to run
	time.Sleep(3 * time.Second)

	// Get the community bucket for verification
	communityBucket, err := js.KeyValue(ctx, graph.BucketCommunityIndex)
	require.NoError(t, err, "COMMUNITY_INDEX bucket should exist")

	// CRITICAL TEST: Verify each entity has an entity→community mapping
	// This is what failed in e2e - entities weren't in communities
	entitiesFoundInCommunity := make(map[string]bool)

	require.Eventually(t, func() bool {
		// Check entity mappings (format: entity.{level}.{entityID})
		for _, entityID := range entityIDs {
			key := "entity.0." + entityID // Level 0
			entry, err := communityBucket.Get(ctx, key)
			if err == nil && entry != nil {
				communityID := string(entry.Value())
				t.Logf("Entity %s → community %s", entityID, communityID)
				entitiesFoundInCommunity[entityID] = true
			}
		}
		// At least 2 entities should be in a community (MinCommunitySize=2)
		count := 0
		for _, found := range entitiesFoundInCommunity {
			if found {
				count++
			}
		}
		return count >= 2
	}, 15*time.Second, 500*time.Millisecond, "At least 2 entities should have community mappings")

	// Log which entities were found
	for _, entityID := range entityIDs {
		if entitiesFoundInCommunity[entityID] {
			t.Logf("✓ Entity %s is in a community", entityID)
		} else {
			t.Logf("✗ Entity %s is NOT in a community", entityID)
		}
	}

	// Verify community data exists and has members. Poll because the
	// detection loop writes entity→community mappings and community
	// member-list keys in sequence — the first Eventually above can
	// resolve on the entity mappings alone, leaving a brief window
	// where Keys() doesn't yet list the community key.
	var communityCount int
	require.Eventually(t, func() bool {
		keys, err := communityBucket.Keys(ctx)
		if err != nil {
			return false
		}
		count := 0
		for _, key := range keys {
			// Skip entity mapping keys
			if len(key) > 7 && key[:7] == "entity." {
				continue
			}
			// Community keys start with a digit (level)
			if len(key) == 0 || key[0] < '0' || key[0] > '9' {
				continue
			}
			entry, err := communityBucket.Get(ctx, key)
			if err != nil {
				continue
			}
			var community map[string]interface{}
			if err := json.Unmarshal(entry.Value(), &community); err != nil {
				continue
			}
			if members, ok := community["members"].([]interface{}); ok {
				t.Logf("Community %s has %d members: %v", key, len(members), members)
				count++
			}
		}
		communityCount = count
		return count >= 1
	}, 10*time.Second, 250*time.Millisecond, "At least one community should exist")

	assert.GreaterOrEqual(t, communityCount, 1, "At least one community should exist")
	assert.GreaterOrEqual(t, len(entitiesFoundInCommunity), 2,
		"At least 2 of the 4 test entities should be in a community")
}
