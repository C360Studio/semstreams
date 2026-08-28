package clustering

import (
	"context"
	"testing"

	"github.com/c360studio/semstreams/internal/semantictest"
)

// entityIDTestProvider implements Provider for EntityID provider testing
type entityIDTestProvider struct {
	entities  []string
	neighbors map[string][]string
	weights   map[string]float64 // key: "from->to"
}

func (m *entityIDTestProvider) GetAllEntityIDs(_ context.Context) ([]string, error) {
	return m.entities, nil
}

func (m *entityIDTestProvider) GetNeighbors(_ context.Context, entityID string, _ string) ([]string, error) {
	return m.neighbors[entityID], nil
}

func (m *entityIDTestProvider) GetEdgeWeight(_ context.Context, fromID, toID string) (float64, error) {
	key := fromID + "->" + toID
	if w, ok := m.weights[key]; ok {
		return w, nil
	}
	return 0.0, nil
}

func TestGetTypePrefix(t *testing.T) {
	tests := []struct {
		name     string
		entityID string
		want     string
	}{
		{
			name:     "valid 6-part EntityID",
			entityID: semantictest.EntityID(t, "c360", "logistics", "sensor", "environmental", "temperature", "temp-sensor-001"),
			want:     "c360.logistics.sensor.environmental.temperature",
		},
		{
			name:     "another valid 6-part EntityID",
			entityID: semantictest.EntityID(t, "c360", "logistics", "work", "maintenance", "completed", "maint-001"),
			want:     "c360.logistics.work.maintenance.completed",
		},
		{
			name:     "5-part EntityID - invalid",
			entityID: "c360.logistics.sensor.environmental.temperature",
			want:     "",
		},
		{
			name:     "7-part EntityID - invalid",
			entityID: "c360.logistics.sensor.environmental.temperature.temp.001",
			want:     "",
		},
		{
			name:     "empty string",
			entityID: "",
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getTypePrefix(tt.entityID)
			if got != tt.want {
				t.Errorf("getTypePrefix(%q) = %q, want %q", tt.entityID, got, tt.want)
			}
		})
	}
}

func TestEntityIDProvider_GetNeighbors_IncludesSiblings(t *testing.T) {
	// Setup: Create entities with same type prefix (siblings)
	entities := []string{
		"c360.logistics.sensor.environmental.temperature.temp-sensor-001",
		"c360.logistics.sensor.environmental.temperature.temp-sensor-002",
		"c360.logistics.sensor.environmental.temperature.temp-sensor-003",
		"c360.logistics.sensor.environmental.humidity.humid-001", // Different type
		"c360.logistics.work.maintenance.completed.maint-001",    // Different domain
	}

	base := &entityIDTestProvider{
		entities:  entities,
		neighbors: make(map[string][]string),
		weights:   make(map[string]float64),
	}

	config := EntityIDProviderConfig{
		SiblingWeight:   0.7,
		MaxSiblings:     10,
		IncludeSiblings: true,
	}

	provider := NewEntityIDProvider(base, config, nil)

	ctx := context.Background()

	// Get neighbors for temp-sensor-001
	neighbors, err := provider.GetNeighbors(ctx, entities[0], "both")
	if err != nil {
		t.Fatalf("GetNeighbors failed: %v", err)
	}

	// Should include temp-sensor-002 and temp-sensor-003 as siblings
	expectedSiblings := map[string]bool{
		"c360.logistics.sensor.environmental.temperature.temp-sensor-002": true,
		"c360.logistics.sensor.environmental.temperature.temp-sensor-003": true,
	}

	for _, n := range neighbors {
		delete(expectedSiblings, n)
	}

	if len(expectedSiblings) > 0 {
		t.Errorf("Missing expected siblings: %v", expectedSiblings)
	}

	// Should NOT include entities with different type prefix
	for _, n := range neighbors {
		if n == "c360.logistics.sensor.environmental.humidity.humid-001" {
			t.Error("Should not include humidity sensor (different type)")
		}
		if n == "c360.logistics.work.maintenance.completed.maint-001" {
			t.Error("Should not include maintenance record (different system)")
		}
	}
}

func TestEntityIDProvider_GetNeighbors_ExcludesSelf(t *testing.T) {
	entities := []string{
		"c360.logistics.sensor.environmental.temperature.temp-sensor-001",
		"c360.logistics.sensor.environmental.temperature.temp-sensor-002",
	}

	base := &entityIDTestProvider{
		entities:  entities,
		neighbors: make(map[string][]string),
		weights:   make(map[string]float64),
	}

	config := DefaultEntityIDProviderConfig()
	provider := NewEntityIDProvider(base, config, nil)

	ctx := context.Background()

	neighbors, err := provider.GetNeighbors(ctx, entities[0], "both")
	if err != nil {
		t.Fatalf("GetNeighbors failed: %v", err)
	}

	// Should NOT include self
	for _, n := range neighbors {
		if n == entities[0] {
			t.Error("Should not include self in neighbors")
		}
	}
}

func TestEntityIDProvider_GetNeighbors_DisabledSiblings(t *testing.T) {
	entities := []string{
		"c360.logistics.sensor.environmental.temperature.temp-sensor-001",
		"c360.logistics.sensor.environmental.temperature.temp-sensor-002",
	}

	base := &entityIDTestProvider{
		entities:  entities,
		neighbors: make(map[string][]string),
		weights:   make(map[string]float64),
	}

	config := EntityIDProviderConfig{
		IncludeSiblings: false, // Disabled
	}
	provider := NewEntityIDProvider(base, config, nil)

	ctx := context.Background()

	neighbors, err := provider.GetNeighbors(ctx, entities[0], "both")
	if err != nil {
		t.Fatalf("GetNeighbors failed: %v", err)
	}

	// With siblings disabled, should return no neighbors (base has none)
	if len(neighbors) != 0 {
		t.Errorf("Expected 0 neighbors with siblings disabled, got %d", len(neighbors))
	}
}

func TestEntityIDProvider_GetEdgeWeight_Siblings(t *testing.T) {
	entities := []string{
		"c360.logistics.sensor.environmental.temperature.temp-sensor-001",
		"c360.logistics.sensor.environmental.temperature.temp-sensor-002",
		"c360.logistics.sensor.environmental.humidity.humid-001",
	}

	base := &entityIDTestProvider{
		entities:  entities,
		neighbors: make(map[string][]string),
		weights:   make(map[string]float64),
	}

	config := EntityIDProviderConfig{
		SiblingWeight:   0.7,
		IncludeSiblings: true,
	}
	provider := NewEntityIDProvider(base, config, nil)

	ctx := context.Background()

	// Siblings should have configured weight
	weight, err := provider.GetEdgeWeight(ctx, entities[0], entities[1])
	if err != nil {
		t.Fatalf("GetEdgeWeight failed: %v", err)
	}
	if weight != 0.7 {
		t.Errorf("Expected sibling weight 0.7, got %f", weight)
	}

	// Non-siblings should have weight 0
	weight, err = provider.GetEdgeWeight(ctx, entities[0], entities[2])
	if err != nil {
		t.Fatalf("GetEdgeWeight failed: %v", err)
	}
	if weight != 0.0 {
		t.Errorf("Expected non-sibling weight 0.0, got %f", weight)
	}
}

func TestEntityIDProvider_GetEdgeWeight_ExplicitTakesPrecedence(t *testing.T) {
	entities := []string{
		"c360.logistics.sensor.environmental.temperature.temp-sensor-001",
		"c360.logistics.sensor.environmental.temperature.temp-sensor-002",
	}

	base := &entityIDTestProvider{
		entities:  entities,
		neighbors: make(map[string][]string),
		weights: map[string]float64{
			// Explicit edge with weight 1.0
			"c360.logistics.sensor.environmental.temperature.temp-sensor-001->c360.logistics.sensor.environmental.temperature.temp-sensor-002": 1.0,
		},
	}

	config := EntityIDProviderConfig{
		SiblingWeight:   0.7,
		IncludeSiblings: true,
	}
	provider := NewEntityIDProvider(base, config, nil)

	ctx := context.Background()

	// Explicit edge weight should take precedence over sibling weight
	weight, err := provider.GetEdgeWeight(ctx, entities[0], entities[1])
	if err != nil {
		t.Fatalf("GetEdgeWeight failed: %v", err)
	}
	if weight != 1.0 {
		t.Errorf("Expected explicit weight 1.0 to take precedence, got %f", weight)
	}
}

func TestEntityIDProvider_AreSiblings(t *testing.T) {
	provider := &EntityIDProvider{includeSiblings: true}

	tests := []struct {
		name     string
		entityA  string
		entityB  string
		expected bool
	}{
		{
			name:     "same type prefix - siblings",
			entityA:  "c360.logistics.sensor.environmental.temperature.temp-001",
			entityB:  "c360.logistics.sensor.environmental.temperature.temp-002",
			expected: true,
		},
		{
			name:     "different type - not siblings",
			entityA:  "c360.logistics.sensor.environmental.temperature.temp-001",
			entityB:  "c360.logistics.sensor.environmental.humidity.humid-001",
			expected: false,
		},
		{
			name:     "different system - not siblings",
			entityA:  "c360.logistics.sensor.environmental.temperature.temp-001",
			entityB:  "c360.logistics.work.maintenance.completed.maint-001",
			expected: false,
		},
		{
			name:     "invalid EntityID - not siblings",
			entityA:  "c360.logistics.sensor.environmental.temperature.temp-001",
			entityB:  "invalid-entity-id",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := provider.areSiblings(tt.entityA, tt.entityB)
			if got != tt.expected {
				t.Errorf("areSiblings(%q, %q) = %v, want %v",
					tt.entityA, tt.entityB, got, tt.expected)
			}
		})
	}
}

func TestEntityIDProvider_MaxSiblings(t *testing.T) {
	// Create many entities with same type prefix
	var entities []string
	for i := 0; i < 20; i++ {
		entities = append(entities, "c360.logistics.sensor.environmental.temperature.temp-"+string(rune('a'+i)))
	}

	base := &entityIDTestProvider{
		entities:  entities,
		neighbors: make(map[string][]string),
		weights:   make(map[string]float64),
	}

	config := EntityIDProviderConfig{
		SiblingWeight:   0.7,
		MaxSiblings:     5, // Limit to 5
		IncludeSiblings: true,
	}
	provider := NewEntityIDProvider(base, config, nil)

	ctx := context.Background()

	neighbors, err := provider.GetNeighbors(ctx, entities[0], "both")
	if err != nil {
		t.Fatalf("GetNeighbors failed: %v", err)
	}

	// Should be limited to MaxSiblings
	if len(neighbors) > 5 {
		t.Errorf("Expected max 5 sibling neighbors, got %d", len(neighbors))
	}
}

// --- Domain Peer Tests ---

func TestGetSystem(t *testing.T) {
	tests := []struct {
		entityID string
		want     string
	}{
		// Position 3 of org.platform.system.domain.type.instance is the source.
		{"acme.ops.gcs.robotics.drone.001", "gcs"},
		{"acme.ops.repo.git.commit.abc", "repo"},
		{"short.id", ""},
		{"a.b.board1.game.quest.x", "board1"},
		{"a.b.c.d.e.f.g", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := getSystem(tt.entityID); got != tt.want {
			t.Errorf("getSystem(%q) = %q, want %q", tt.entityID, got, tt.want)
		}
	}
}

func TestSystemPeers_SameDomain(t *testing.T) {
	base := &entityIDTestProvider{
		entities: []string{
			"acme.ops.robotics.gcs.drone.001",
			"acme.ops.robotics.gcs.sensor.002",
			"acme.ops.robotics.gcs.drone.003",
			"acme.ops.git.repo.commit.abc",
		},
		neighbors: map[string][]string{},
		weights:   map[string]float64{},
	}

	provider := NewEntityIDProvider(base, EntityIDProviderConfig{
		IncludeSiblings:    false, // disable siblings to isolate system peer behavior
		IncludeSystemPeers: true,
		SystemPeerWeight:   0.3,
		MaxSystemPeers:     15,
	}, nil)

	ctx := context.Background()
	neighbors, err := provider.GetNeighbors(ctx, "acme.ops.robotics.gcs.drone.001", "both")
	if err != nil {
		t.Fatalf("GetNeighbors failed: %v", err)
	}

	// Should include sensor.002 and drone.003 (same system "gcs") but NOT commit.abc (system "repo")
	neighborSet := make(map[string]bool)
	for _, id := range neighbors {
		neighborSet[id] = true
	}

	if !neighborSet["acme.ops.robotics.gcs.sensor.002"] {
		t.Error("expected sensor.002 as system peer (same system: gcs)")
	}
	if !neighborSet["acme.ops.robotics.gcs.drone.003"] {
		t.Error("expected drone.003 as system peer (same system: gcs)")
	}
	if neighborSet["acme.ops.git.repo.commit.abc"] {
		t.Error("commit.abc should NOT be a system peer (different system: repo)")
	}
}

func TestSystemPeers_CrossDomain_NotIncluded(t *testing.T) {
	base := &entityIDTestProvider{
		entities: []string{
			"acme.ops.robotics.gcs.drone.001",
			"acme.ops.git.repo.commit.abc",
		},
		neighbors: map[string][]string{},
		weights:   map[string]float64{},
	}

	provider := NewEntityIDProvider(base, EntityIDProviderConfig{
		IncludeSiblings:    false,
		IncludeSystemPeers: true,
		SystemPeerWeight:   0.3,
		MaxSystemPeers:     15,
	}, nil)

	ctx := context.Background()
	neighbors, err := provider.GetNeighbors(ctx, "acme.ops.robotics.gcs.drone.001", "both")
	if err != nil {
		t.Fatalf("GetNeighbors failed: %v", err)
	}

	if len(neighbors) != 0 {
		t.Errorf("expected 0 system peers (only cross-system entity exists), got %d: %v", len(neighbors), neighbors)
	}
}

func TestSystemPeers_MaxLimit(t *testing.T) {
	entities := make([]string, 25)
	for i := range entities {
		entities[i] = "acme.ops.robotics.gcs.drone." + string(rune('a'+i))
	}

	base := &entityIDTestProvider{
		entities:  entities,
		neighbors: map[string][]string{},
		weights:   map[string]float64{},
	}

	provider := NewEntityIDProvider(base, EntityIDProviderConfig{
		IncludeSiblings:    false,
		IncludeSystemPeers: true,
		SystemPeerWeight:   0.3,
		MaxSystemPeers:     5,
	}, nil)

	ctx := context.Background()
	neighbors, err := provider.GetNeighbors(ctx, entities[0], "both")
	if err != nil {
		t.Fatalf("GetNeighbors failed: %v", err)
	}

	if len(neighbors) > 5 {
		t.Errorf("expected max 5 system peers, got %d", len(neighbors))
	}
}

func TestSystemPeers_Weight(t *testing.T) {
	base := &entityIDTestProvider{
		entities: []string{
			"acme.ops.robotics.gcs.drone.001",
			"acme.ops.robotics.gcs.sensor.002",
			"acme.ops.git.repo.commit.abc",
		},
		neighbors: map[string][]string{},
		weights:   map[string]float64{},
	}

	provider := NewEntityIDProvider(base, EntityIDProviderConfig{
		IncludeSiblings:    false,
		IncludeSystemPeers: true,
		SystemPeerWeight:   0.3,
		MaxSystemPeers:     15,
	}, nil)

	ctx := context.Background()

	// Same domain → system peer weight
	weight, err := provider.GetEdgeWeight(ctx, "acme.ops.robotics.gcs.drone.001", "acme.ops.robotics.gcs.sensor.002")
	if err != nil {
		t.Fatalf("GetEdgeWeight failed: %v", err)
	}
	if weight != 0.3 {
		t.Errorf("same-domain weight = %v, want 0.3", weight)
	}

	// Cross domain → no edge
	weight, err = provider.GetEdgeWeight(ctx, "acme.ops.robotics.gcs.drone.001", "acme.ops.git.repo.commit.abc")
	if err != nil {
		t.Fatalf("GetEdgeWeight failed: %v", err)
	}
	if weight != 0.0 {
		t.Errorf("cross-system weight = %v, want 0.0", weight)
	}
}

func TestSystemPeers_Disabled(t *testing.T) {
	base := &entityIDTestProvider{
		entities: []string{
			"acme.ops.robotics.gcs.drone.001",
			"acme.ops.robotics.gcs.sensor.002",
		},
		neighbors: map[string][]string{},
		weights:   map[string]float64{},
	}

	provider := NewEntityIDProvider(base, EntityIDProviderConfig{
		IncludeSiblings:    false,
		IncludeSystemPeers: false,
	}, nil)

	ctx := context.Background()
	neighbors, err := provider.GetNeighbors(ctx, "acme.ops.robotics.gcs.drone.001", "both")
	if err != nil {
		t.Fatalf("GetNeighbors failed: %v", err)
	}

	if len(neighbors) != 0 {
		t.Errorf("expected 0 neighbors with system peers disabled, got %d", len(neighbors))
	}
}

func TestSystemPeers_NoDuplicateWithSiblings(t *testing.T) {
	base := &entityIDTestProvider{
		entities: []string{
			"acme.ops.robotics.gcs.drone.001",
			"acme.ops.robotics.gcs.drone.002",  // same type prefix = sibling AND same system
			"acme.ops.robotics.gcs.sensor.003", // same system only
		},
		neighbors: map[string][]string{},
		weights:   map[string]float64{},
	}

	provider := NewEntityIDProvider(base, EntityIDProviderConfig{
		IncludeSiblings:    true,
		SiblingWeight:      0.7,
		MaxSiblings:        10,
		IncludeSystemPeers: true,
		SystemPeerWeight:   0.3,
		MaxSystemPeers:     15,
	}, nil)

	ctx := context.Background()
	neighbors, err := provider.GetNeighbors(ctx, "acme.ops.robotics.gcs.drone.001", "both")
	if err != nil {
		t.Fatalf("GetNeighbors failed: %v", err)
	}

	// drone.002 should appear once (as sibling), sensor.003 as system peer
	seen := make(map[string]int)
	for _, id := range neighbors {
		seen[id]++
	}

	if seen["acme.ops.robotics.gcs.drone.002"] != 1 {
		t.Errorf("drone.002 should appear exactly once (sibling), appeared %d times", seen["acme.ops.robotics.gcs.drone.002"])
	}
	if seen["acme.ops.robotics.gcs.sensor.003"] != 1 {
		t.Errorf("sensor.003 should appear exactly once (system peer), appeared %d times", seen["acme.ops.robotics.gcs.sensor.003"])
	}
	if len(neighbors) != 2 {
		t.Errorf("expected 2 total neighbors, got %d: %v", len(neighbors), neighbors)
	}
}

// entity-id-audit:classify intentional-malformed "c360.logistics.sensor.environmental.temperature" line=51 column=14 surface=go-field:.entityID entity_id_invalid:arity verifies a five-position ID has no type prefix
// entity-id-audit:classify intentional-malformed "c360.logistics.sensor.environmental.temperature.temp.001" line=56 column=14 surface=go-field:.entityID entity_id_invalid:arity verifies a seven-position ID has no type prefix
// entity-id-audit:classify intentional-malformed "" line=61 column=14 surface=go-field:.entityID entity_id_invalid:empty verifies empty input has no type prefix

// TestEntityIDEdgesReadPositionsByName pins the EntityID edge synthesis
// against the canonical order org.platform.system.domain.type.instance: sibling
// edges share the five-position type prefix and source-peer edges share the
// named System field (position 3), never a raw index (graph-clustering delta
// "sibling and source-peer synthesis reads positions by name"; inventory C3).
func TestEntityIDEdgesReadPositionsByName(t *testing.T) {
	base := &entityIDTestProvider{
		entities: []string{
			"acme.dep1.src.git.commit.a1",
			"acme.dep1.src.git.commit.a2",   // sibling: same type prefix
			"acme.dep1.src.media.video.v1",  // source peer: same source, other taxonomy
			"acme.dep1.other.git.commit.b1", // same taxonomy, OTHER source: not a peer
		},
		neighbors: map[string][]string{},
		weights:   map[string]float64{},
	}
	provider := NewEntityIDProvider(base, EntityIDProviderConfig{
		IncludeSiblings:    true,
		SiblingWeight:      0.7,
		MaxSiblings:        10,
		IncludeSystemPeers: true,
		SystemPeerWeight:   0.3,
		MaxSystemPeers:     15,
	}, nil)

	neighbors, err := provider.GetNeighbors(context.Background(), "acme.dep1.src.git.commit.a1", "both")
	if err != nil {
		t.Fatalf("GetNeighbors: %v", err)
	}
	got := map[string]bool{}
	for _, id := range neighbors {
		got[id] = true
	}
	if !got["acme.dep1.src.git.commit.a2"] {
		t.Errorf("neighbors = %v, want the sibling a2 (five-position type prefix)", neighbors)
	}
	if !got["acme.dep1.src.media.video.v1"] {
		t.Errorf("neighbors = %v, want the source peer v1 (named System = src)", neighbors)
	}
	if got["acme.dep1.other.git.commit.b1"] {
		t.Errorf("neighbors = %v, b1 shares the taxonomy git but not the source and must not be a peer", neighbors)
	}
	if source := getSystem("acme.dep1.src.git.commit.a1"); source != "src" {
		t.Errorf("getSystem = %q, want src (position 3 by name)", source)
	}
	if prefix := getTypePrefix("acme.dep1.src.git.commit.a1"); prefix != "acme.dep1.src.git.commit" {
		t.Errorf("getTypePrefix = %q, want acme.dep1.src.git.commit", prefix)
	}
}
