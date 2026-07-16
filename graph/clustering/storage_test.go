package clustering

import (
	"context"
	"testing"
	"time"

	"github.com/c360studio/semstreams/internal/semantictest"
	"github.com/c360studio/semstreams/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// T066: Test for create_triples config option
func TestCommunityStorageConfig_CreateTriplesOption(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		config            CommunityStorageConfig
		wantCreateTriples bool
		wantPredicate     string
	}{
		{
			name: "CreateTriples enabled with default predicate",
			config: CommunityStorageConfig{
				CreateTriples:   true,
				TriplePredicate: "graph.community.member-of",
			},
			wantCreateTriples: true,
			wantPredicate:     "graph.community.member-of",
		},
		{
			name: "CreateTriples enabled with custom predicate",
			config: CommunityStorageConfig{
				CreateTriples:   true,
				TriplePredicate: "custom.community.belongs-to",
			},
			wantCreateTriples: true,
			wantPredicate:     "custom.community.belongs-to",
		},
		{
			name: "CreateTriples disabled",
			config: CommunityStorageConfig{
				CreateTriples: false,
			},
			wantCreateTriples: false,
			wantPredicate:     "", // predicate-audit:invalid {"kind":"stored-predicate","value":"","reason":"empty"}
		},
		{
			name:              "Default values - CreateTriples false, predicate empty",
			config:            CommunityStorageConfig{},
			wantCreateTriples: false,
			wantPredicate:     "", // predicate-audit:invalid {"kind":"stored-predicate","value":"","reason":"empty"}
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Verify config fields exist and have expected values
			assert.Equal(t, tt.wantCreateTriples, tt.config.CreateTriples, "CreateTriples field mismatch")

			// Only check predicate if CreateTriples is enabled or explicitly set
			if tt.config.CreateTriples || tt.config.TriplePredicate != "" {
				assert.Equal(t, tt.wantPredicate, tt.config.TriplePredicate, "TriplePredicate field mismatch")
			}
		})
	}
}

// T066: Test NewNATSCommunityStorageWithConfig accepts config with CreateTriples
func TestNewNATSCommunityStorageWithConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config CommunityStorageConfig
	}{
		{
			name: "Config with CreateTriples enabled",
			config: CommunityStorageConfig{
				CreateTriples:   true,
				TriplePredicate: "graph.community.member-of",
			},
		},
		{
			name: "Config with CreateTriples disabled",
			config: CommunityStorageConfig{
				CreateTriples: false,
			},
		},
		{
			name: "Config with custom predicate",
			config: CommunityStorageConfig{
				CreateTriples:   true,
				TriplePredicate: "custom.community.predicate",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// This test will fail until we implement the config-based constructor
			// Currently only NewNATSCommunityStorage exists (without config)
			storage := NewNATSCommunityStorageWithConfig(nil, tt.config)
			require.NotNil(t, storage, "NewNATSCommunityStorageWithConfig should return non-nil storage")
		})
	}
}

// T067: Test community triple creation when CreateTriples is enabled
func TestNATSCommunityStorage_SaveCommunity_CreateTriples(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	tests := []struct {
		name              string
		config            CommunityStorageConfig
		community         *Community
		wantTripleCount   int
		wantPredicate     string
		checkTripleFields bool
	}{
		{
			name: "CreateTriples enabled - generates member_of triples",
			config: CommunityStorageConfig{
				CreateTriples:   true,
				TriplePredicate: "graph.community.member-of",
			},
			community: &Community{
				ID:    "comm-0-test",
				Level: 0,
				Members: []string{
					semantictest.EntityID(t, "test", "clustering", "graph", "community", "member", "001"),
					semantictest.EntityID(t, "test", "clustering", "graph", "community", "member", "002"),
					semantictest.EntityID(t, "test", "clustering", "graph", "community", "member", "003"),
				},
			},
			wantTripleCount:   3,
			wantPredicate:     "graph.community.member-of",
			checkTripleFields: true,
		},
		{
			name: "CreateTriples enabled with custom predicate",
			config: CommunityStorageConfig{
				CreateTriples:   true,
				TriplePredicate: "custom.community.belongs-to",
			},
			community: &Community{
				ID:    "comm-1-custom",
				Level: 1,
				Members: []string{
					semantictest.EntityID(t, "test", "clustering", "graph", "community", "member", "004"),
					semantictest.EntityID(t, "test", "clustering", "graph", "community", "member", "005"),
				},
			},
			wantTripleCount:   2,
			wantPredicate:     "custom.community.belongs-to",
			checkTripleFields: true,
		},
		{
			name: "CreateTriples disabled - no triples generated",
			config: CommunityStorageConfig{
				CreateTriples: false,
			},
			community: &Community{
				ID:    "comm-0-no-triples",
				Level: 0,
				Members: []string{
					semantictest.EntityID(t, "test", "clustering", "graph", "community", "member", "001"),
					semantictest.EntityID(t, "test", "clustering", "graph", "community", "member", "002"),
				},
			},
			wantTripleCount:   0,
			wantPredicate:     "", // predicate-audit:invalid {"kind":"stored-predicate","value":"","reason":"empty"}
			checkTripleFields: false,
		},
		{
			name: "CreateTriples enabled - empty members list",
			config: CommunityStorageConfig{
				CreateTriples:   true,
				TriplePredicate: "graph.community.member-of",
			},
			community: &Community{
				ID:      "comm-0-empty",
				Level:   0,
				Members: []string{},
			},
			wantTripleCount:   0,
			wantPredicate:     "graph.community.member-of",
			checkTripleFields: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Mock KV store for testing (will need implementation)
			storage := NewNATSCommunityStorageWithConfig(nil, tt.config)
			require.NotNil(t, storage)

			// This will fail until we implement triple creation
			err := storage.SaveCommunity(ctx, tt.community)
			require.NoError(t, err, "SaveCommunity should succeed")

			// Verify triple creation through storage's internal state or method
			// We need a method to retrieve created triples for verification
			triples := storage.GetCreatedTriples()
			assert.Len(t, triples, tt.wantTripleCount, "Should create correct number of triples")

			if tt.checkTripleFields && len(triples) > 0 {
				// Verify triple structure
				for i, triple := range triples {
					// Subject should be the entity ID
					assert.Equal(t, tt.community.Members[i], triple.Subject,
						"Triple subject should be entity ID")

					// Predicate should match config
					assert.Equal(t, tt.wantPredicate, triple.Predicate,
						"Triple predicate should match config")

					// Object should be the community ID
					assert.Equal(t, tt.community.ID, triple.Object,
						"Triple object should be community ID")

					// Verify triple metadata
					assert.NotEmpty(t, triple.Source, "Triple should have source")
					assert.NotZero(t, triple.Timestamp, "Triple should have timestamp")
					assert.Greater(t, triple.Confidence, 0.0, "Triple should have confidence > 0")
					assert.LessOrEqual(t, triple.Confidence, 1.0, "Triple confidence should be <= 1.0")
				}
			}
		})
	}
}

// T067: Test triple format validation
func TestCommunityTriple_Format(t *testing.T) {
	t.Parallel()

	entityID := "c360.platform1.robotics.mav1.drone.0"
	communityID := "comm-0-robotics"
	predicate := semantictest.Predicate(t, "graph", "community", "member-of")

	// Expected triple format
	triple := message.Triple{
		Subject:    entityID,
		Predicate:  semantictest.Predicate(t, "graph", "community", "member-of"),
		Object:     communityID,
		Source:     "community_detection",
		Timestamp:  time.Now(),
		Confidence: 1.0,
	}

	// Verify triple structure
	assert.Equal(t, entityID, triple.Subject, "Subject should be entity ID")
	assert.Equal(t, predicate, triple.Predicate, "Predicate should be member_of relationship")
	assert.Equal(t, communityID, triple.Object, "Object should be community ID")
	assert.Equal(t, "community_detection", triple.Source, "Source should identify community detection")
	assert.NotZero(t, triple.Timestamp, "Timestamp should be set")
	assert.Equal(t, 1.0, triple.Confidence, "Confidence should be 1.0 for deterministic detection")
}

// T068: Test dual-write to COMMUNITY_INDEX + triples
func TestNATSCommunityStorage_SaveCommunity_DualWrite(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	tests := []struct {
		name             string
		config           CommunityStorageConfig
		community        *Community
		expectTriples    bool
		expectIndexWrite bool
	}{
		{
			name: "Dual-write enabled - both index and triples",
			config: CommunityStorageConfig{
				CreateTriples:   true,
				TriplePredicate: "graph.community.member-of",
			},
			community: &Community{
				ID:    "comm-0-dual",
				Level: 0,
				Members: []string{
					semantictest.EntityID(t, "test", "clustering", "graph", "community", "member", "001"),
					semantictest.EntityID(t, "test", "clustering", "graph", "community", "member", "002"),
				},
			},
			expectTriples:    true,
			expectIndexWrite: true,
		},
		{
			name: "Triples disabled - only index write",
			config: CommunityStorageConfig{
				CreateTriples: false,
			},
			community: &Community{
				ID:    "comm-0-index-only",
				Level: 0,
				Members: []string{
					semantictest.EntityID(t, "test", "clustering", "graph", "community", "member", "003"),
				},
			},
			expectTriples:    false,
			expectIndexWrite: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			storage := NewNATSCommunityStorageWithConfig(nil, tt.config)
			require.NotNil(t, storage)

			// Save community
			err := storage.SaveCommunity(ctx, tt.community)
			require.NoError(t, err, "SaveCommunity should succeed")

			// Verify COMMUNITY_INDEX write (backward compatibility)
			if tt.expectIndexWrite {
				retrieved, err := storage.GetCommunity(ctx, tt.community.ID)
				require.NoError(t, err, "GetCommunity should work after save")
				require.NotNil(t, retrieved, "Community should be retrievable from index")
				assert.Equal(t, tt.community.ID, retrieved.ID, "Retrieved community ID should match")
				assert.ElementsMatch(t, tt.community.Members, retrieved.Members,
					"Retrieved community members should match")
			}

			// Verify triple creation
			triples := storage.GetCreatedTriples()
			if tt.expectTriples {
				assert.NotEmpty(t, triples, "Triples should be created when enabled")
				assert.Len(t, triples, len(tt.community.Members),
					"Should create one triple per member")
			} else {
				assert.Empty(t, triples, "No triples should be created when disabled")
			}
		})
	}
}

// T068: Test backward compatibility - GetCommunity works after dual-write
func TestNATSCommunityStorage_BackwardCompatibility(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	community := &Community{
		ID:    "comm-0-compat",
		Level: 0,
		Members: []string{
			semantictest.EntityID(t, "test", "clustering", "graph", "community", "member", "001"),
			semantictest.EntityID(t, "test", "clustering", "graph", "community", "member", "002"),
			semantictest.EntityID(t, "test", "clustering", "graph", "community", "member", "003"),
		},
		StatisticalSummary: "Test community",
	}

	tests := []struct {
		name   string
		config CommunityStorageConfig
	}{
		{
			name: "With triples enabled",
			config: CommunityStorageConfig{
				CreateTriples:   true,
				TriplePredicate: "graph.community.member-of",
			},
		},
		{
			name: "With triples disabled",
			config: CommunityStorageConfig{
				CreateTriples: false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			storage := NewNATSCommunityStorageWithConfig(nil, tt.config)
			require.NotNil(t, storage)

			// Save with current config
			err := storage.SaveCommunity(ctx, community)
			require.NoError(t, err)

			// Verify GetCommunity still works (backward compatibility)
			retrieved, err := storage.GetCommunity(ctx, community.ID)
			require.NoError(t, err)
			require.NotNil(t, retrieved)

			assert.Equal(t, community.ID, retrieved.ID)
			assert.Equal(t, community.Level, retrieved.Level)
			assert.ElementsMatch(t, community.Members, retrieved.Members)
			assert.Equal(t, community.StatisticalSummary, retrieved.StatisticalSummary)
		})
	}
}

// T069: Test PathRAG traversing community membership via triples
func TestPathRAG_TraverseCommunityMembership(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	isolatedSwitchID := semantictest.EntityID(t, "c360", "platform1", "network", "datacenter", "switch", "0")

	tests := []struct {
		name                 string
		entityID             string
		communityID          string
		membershipTriples    []message.Triple
		wantTraversalSuccess bool
		wantCommunityFound   bool
	}{
		{
			name:        "PathRAG finds community via member_of triple",
			entityID:    "c360.platform1.robotics.mav1.drone.0",
			communityID: "comm-0-robotics",
			membershipTriples: []message.Triple{
				{
					Subject:    "c360.platform1.robotics.mav1.drone.0",
					Predicate:  "graph.community.member-of",
					Object:     "comm-0-robotics",
					Source:     "community_detection",
					Timestamp:  time.Now(),
					Confidence: 1.0,
				},
			},
			wantTraversalSuccess: true,
			wantCommunityFound:   true,
		},
		{
			name:        "PathRAG finds multiple communities for entity",
			entityID:    "c360.platform1.robotics.mav1.sensor.0",
			communityID: "comm-0-sensors",
			membershipTriples: []message.Triple{
				{
					Subject:    "c360.platform1.robotics.mav1.sensor.0",
					Predicate:  "graph.community.member-of",
					Object:     "comm-0-sensors",
					Source:     "community_detection",
					Timestamp:  time.Now(),
					Confidence: 1.0,
				},
				{
					Subject:    "c360.platform1.robotics.mav1.sensor.0",
					Predicate:  "graph.community.member-of",
					Object:     "comm-1-robotics-hierarchy",
					Source:     "community_detection",
					Timestamp:  time.Now(),
					Confidence: 1.0,
				},
			},
			wantTraversalSuccess: true,
			wantCommunityFound:   true,
		},
		{
			name:                 "PathRAG handles entity with no community membership",
			entityID:             isolatedSwitchID,
			communityID:          "",
			membershipTriples:    []message.Triple{},
			wantTraversalSuccess: true,
			wantCommunityFound:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Mock triple store with membership triples
			// This will fail until we integrate with actual triple storage
			tripleStore := NewMockTripleStore(tt.membershipTriples)
			require.NotNil(t, tripleStore)

			// Query for community membership via triple traversal
			// This simulates PathRAG querying: subject=entityID, predicate=member_of
			results, err := tripleStore.QueryBySubjectPredicate(ctx, tt.entityID, "graph.community.member-of")

			if tt.wantTraversalSuccess {
				require.NoError(t, err, "Triple traversal should succeed")
			}

			if tt.wantCommunityFound {
				require.NotEmpty(t, results, "Should find community membership triples")

				// Verify triple content
				found := false
				for _, triple := range results {
					if tt.communityID != "" && triple.Object == tt.communityID {
						found = true
						assert.Equal(t, tt.entityID, triple.Subject, "Subject should be entity ID")
						assert.Equal(t, "graph.community.member-of", triple.Predicate,
							"Predicate should be member_of")
						assert.Equal(t, tt.communityID, triple.Object, "Object should be community ID")
					}
				}

				if tt.communityID != "" {
					assert.True(t, found, "Should find expected community in results")
				}
			} else {
				assert.Empty(t, results, "Should not find community membership for entity")
			}
		})
	}
}

// T069: Test community triples appear in relationship traversal
func TestRelationshipTraversal_IncludesCommunityTriples(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	entityID := "c360.platform1.robotics.mav1.drone.0"

	// Mix of regular relationship triples and community membership triples
	allTriples := []message.Triple{
		{
			Subject:    entityID,
			Predicate:  "robotics.component.powered-by",
			Object:     "c360.platform1.robotics.mav1.battery.0",
			Source:     "telemetry",
			Timestamp:  time.Now(),
			Confidence: 1.0,
		},
		{
			Subject:    entityID,
			Predicate:  "graph.community.member-of",
			Object:     "comm-0-drones",
			Source:     "community_detection",
			Timestamp:  time.Now(),
			Confidence: 1.0,
		},
		{
			Subject:    entityID,
			Predicate:  "robotics.location.near",
			Object:     "c360.platform1.robotics.mav1.drone.1",
			Source:     "proximity",
			Timestamp:  time.Now(),
			Confidence: 0.9,
		},
	}

	tripleStore := NewMockTripleStore(allTriples)
	require.NotNil(t, tripleStore)

	// Query all relationships for entity (PathRAG use case)
	results, err := tripleStore.QueryBySubject(ctx, entityID)
	require.NoError(t, err)
	require.Len(t, results, 3, "Should return all triples including community membership")

	// Verify community triple is included in traversal
	hasCommunityTriple := false
	hasComponentTriple := false
	hasLocationTriple := false

	for _, triple := range results {
		switch triple.Predicate {
		case "graph.community.member-of":
			hasCommunityTriple = true
			assert.Equal(t, "comm-0-drones", triple.Object)
		case "robotics.component.powered-by":
			hasComponentTriple = true
		case "robotics.location.near":
			hasLocationTriple = true
		}
	}

	assert.True(t, hasCommunityTriple, "Community membership should appear in relationship traversal")
	assert.True(t, hasComponentTriple, "Component relationships should be included")
	assert.True(t, hasLocationTriple, "Location relationships should be included")
}

// MockTripleStore simulates triple storage for testing
// This will be replaced with actual triple store integration
type MockTripleStore struct {
	triples []message.Triple
}

func NewMockTripleStore(triples []message.Triple) *MockTripleStore {
	return &MockTripleStore{
		triples: triples,
	}
}

func (m *MockTripleStore) QueryBySubject(_ context.Context, subject string) ([]message.Triple, error) {
	var results []message.Triple
	for _, triple := range m.triples {
		if triple.Subject == subject {
			results = append(results, triple)
		}
	}
	return results, nil
}

func (m *MockTripleStore) QueryBySubjectPredicate(_ context.Context, subject, predicate string) ([]message.Triple, error) {
	var results []message.Triple
	for _, triple := range m.triples {
		if triple.Subject == subject && triple.Predicate == predicate {
			results = append(results, triple)
		}
	}
	return results, nil
}
