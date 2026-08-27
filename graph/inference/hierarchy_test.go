package inference

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/vocabulary"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// hierarchyMockTripleAdder records added triples for verification
type hierarchyMockTripleAdder struct {
	mu      sync.Mutex
	triples []message.Triple
	err     error
}

func (m *hierarchyMockTripleAdder) AddTriple(_ context.Context, triple message.Triple) error {
	if m.err != nil {
		return m.err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.triples = append(m.triples, triple)
	return nil
}

func (m *hierarchyMockTripleAdder) getTriples() []message.Triple {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]message.Triple, len(m.triples))
	copy(result, m.triples)
	return result
}

// mockEntityManager implements EntityManager for testing
type mockEntityManager struct {
	mu       sync.Mutex
	entities map[string]bool // entityID -> exists
	created  []*gtypes.EntityState
	err      error
}

func newMockEntityManager() *mockEntityManager {
	return &mockEntityManager{
		entities: make(map[string]bool),
		created:  make([]*gtypes.EntityState, 0),
	}
}

func (m *mockEntityManager) ExistsEntity(_ context.Context, id string) (bool, error) {
	if m.err != nil {
		return false, m.err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.entities[id], nil
}

func (m *mockEntityManager) CreateEntity(_ context.Context, entity *gtypes.EntityState) (*gtypes.EntityState, error) {
	if m.err != nil {
		return nil, m.err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.entities[entity.ID] {
		return nil, errors.New("entity already exists")
	}

	m.entities[entity.ID] = true
	m.created = append(m.created, entity)
	return entity, nil
}

func (m *mockEntityManager) ListWithPrefix(_ context.Context, prefix string) ([]string, error) {
	if m.err != nil {
		return nil, m.err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	var matched []string
	prefixDot := prefix + "."
	for id := range m.entities {
		if strings.HasPrefix(id, prefixDot) {
			matched = append(matched, id)
		}
	}
	return matched, nil
}

func (m *mockEntityManager) getCreatedEntities() []*gtypes.EntityState {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]*gtypes.EntityState, len(m.created))
	copy(result, m.created)
	return result
}

func (m *mockEntityManager) addExistingEntity(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entities[id] = true
}

// TestHierarchyInference_InverseEdgeWriteFailureIsNonFatal locks the ADR-055 §3
// in-process-bypass safety property. HierarchyInference is the only production
// caller of the in-process Component.AddTriple (via tripleAdderAdapter), and it
// uses it ONLY for inverse edges onto OTHER subjects — sibling back-edges
// (hierarchy.go:313) and container contains-edges (hierarchy.go:368). Those
// targets pre-exist (siblings via ListWithPrefix, containers via
// ensureContainerExists), but the Wave-0 audit must ASSERT — not assume — that a
// write failure on one of them is non-fatal: it degrades to a logged warning +
// edgesFailed metric and NEVER propagates. This is what makes the closing-move
// must-exist flip safe here — if a future must-exist rejection hits an inverse
// write, the entity's own forward hierarchy triples still land and ingest does
// not fail. A refactor that made these propagate would break the flip; this test
// catches that.
func TestHierarchyInference_InverseEdgeWriteFailureIsNonFatal(t *testing.T) {
	// Every in-process inverse-edge write rejects — simulating a must-exist
	// "entity not found" on the sibling/container target.
	failingAdder := &hierarchyMockTripleAdder{err: errors.New("entity not found (simulated must-exist rejection)")}
	entityManager := newMockEntityManager()
	// An existing sibling so createSiblingEdges attempts an inverse back-edge.
	entityManager.addExistingEntity("c360.logistics.sensor.environmental.temperature.temp-002")

	config := HierarchyConfig{
		Enabled:            true,
		CreateTypeEdges:    true,
		CreateSystemEdges:  true,
		CreateDomainEdges:  true,
		CreateTypeSiblings: true,
	}
	hi := NewHierarchyInference(entityManager, failingAdder, config, nil)

	// GetHierarchyTriples is the production entry (graph-ingest MergeEntity calls
	// it on the create branch). Every inverse-edge write inside it hits the
	// failing adder; container creation goes through the (working) entityManager.
	triples, err := hi.GetHierarchyTriples(context.Background(), "c360.logistics.sensor.environmental.temperature.temp-001")

	require.NoError(t, err,
		"inverse-edge write failures must NOT propagate — the forward hierarchy triples must still return")
	assert.NotEmpty(t, triples,
		"the entity's own forward hierarchy/sibling edges must still be produced despite the failing inverse writes")
}

func TestHierarchyInference_OnEntityCreated_Disabled(t *testing.T) {
	tripleAdder := &hierarchyMockTripleAdder{}
	entityManager := newMockEntityManager()

	config := HierarchyConfig{
		Enabled:         false, // Disabled
		CreateTypeEdges: true,
	}

	hi := NewHierarchyInference(entityManager, tripleAdder, config, nil)

	err := hi.OnEntityCreated(context.Background(), "c360.logistics.document.sensor.temperature.sensor-001")
	require.NoError(t, err)

	// No triples should be added when disabled
	assert.Empty(t, tripleAdder.getTriples())
	assert.Empty(t, entityManager.getCreatedEntities())
}

func TestHierarchyInference_OnEntityCreated_InvalidEntityID(t *testing.T) {
	tripleAdder := &hierarchyMockTripleAdder{}
	entityManager := newMockEntityManager()

	config := HierarchyConfig{
		Enabled:         true,
		CreateTypeEdges: true,
	}

	hi := NewHierarchyInference(entityManager, tripleAdder, config, nil)

	// 5-part entity ID should be skipped
	err := hi.OnEntityCreated(context.Background(), "c360.logistics.document.sensor.temperature")
	require.NoError(t, err)
	assert.Empty(t, tripleAdder.getTriples())

	// 7-part entity ID should be skipped
	err = hi.OnEntityCreated(context.Background(), "c360.logistics.document.sensor.temperature.zone.sensor")
	require.NoError(t, err)
	assert.Empty(t, tripleAdder.getTriples())
}

func TestHierarchyInference_OnEntityCreated_TypeEdgeOnly(t *testing.T) {
	tripleAdder := &hierarchyMockTripleAdder{}
	entityManager := newMockEntityManager()

	config := HierarchyConfig{
		Enabled:           true,
		CreateTypeEdges:   true,
		CreateSystemEdges: false,
		CreateDomainEdges: false,
	}

	hi := NewHierarchyInference(entityManager, tripleAdder, config, nil)

	entityID := "c360.logistics.document.sensor.temperature.sensor-001"
	containerID := "c360.logistics.document.sensor.temperature.group"
	err := hi.OnEntityCreated(context.Background(), entityID)
	require.NoError(t, err)

	// Should create 1 container
	createdEntities := entityManager.getCreatedEntities()
	assert.Len(t, createdEntities, 1)
	assert.Equal(t, containerID, createdEntities[0].ID)

	// Should create 2 edges: forward (member) + inverse (contains)
	triples := tripleAdder.getTriples()
	assert.Len(t, triples, 2)

	// Find forward and inverse triples
	var forwardTriple, inverseTriple *message.Triple
	for i := range triples {
		if triples[i].Subject == entityID {
			forwardTriple = &triples[i]
		} else if triples[i].Subject == containerID {
			inverseTriple = &triples[i]
		}
	}

	// Verify forward edge: entity → member → container
	require.NotNil(t, forwardTriple, "forward triple not found")
	assert.Equal(t, vocabulary.HierarchyTypeMember, forwardTriple.Predicate)
	assert.Equal(t, containerID, forwardTriple.Object)
	assert.Equal(t, "inference.hierarchy", forwardTriple.Context)
	assert.Equal(t, 1.0, forwardTriple.Confidence)

	// Verify inverse edge: container → contains → entity
	require.NotNil(t, inverseTriple, "inverse triple not found")
	assert.Equal(t, vocabulary.HierarchyTypeContains, inverseTriple.Predicate)
	assert.Equal(t, entityID, inverseTriple.Object)
	assert.Equal(t, "inference.hierarchy", inverseTriple.Context)
}

func TestHierarchyInference_OnEntityCreated_AllLevels(t *testing.T) {
	tripleAdder := &hierarchyMockTripleAdder{}
	entityManager := newMockEntityManager()

	config := HierarchyConfig{
		Enabled:           true,
		CreateTypeEdges:   true,
		CreateSystemEdges: true,
		CreateDomainEdges: true,
	}

	hi := NewHierarchyInference(entityManager, tripleAdder, config, nil)

	entityID := "c360.logistics.document.sensor.temperature.sensor-001"
	err := hi.OnEntityCreated(context.Background(), entityID)
	require.NoError(t, err)

	// Should create 3 containers
	createdEntities := entityManager.getCreatedEntities()
	assert.Len(t, createdEntities, 3)

	containerIDs := make(map[string]bool)
	for _, e := range createdEntities {
		containerIDs[e.ID] = true
	}
	assert.True(t, containerIDs["c360.logistics.document.sensor.temperature.group"]) // Type
	assert.True(t, containerIDs["c360.logistics.document.sensor.group.container"])   // System
	assert.True(t, containerIDs["c360.logistics.sensor.group.container.level"])      // Domain

	// Should create 6 edges: 3 forward (member) + 3 inverse (contains)
	triples := tripleAdder.getTriples()
	assert.Len(t, triples, 6)

	// Extract forward edges (entity → member → container)
	forwardPredicates := make(map[string]string) // predicate-audit:unrelated {"column":23,"surface":"go-assignment:forwardPredicates","value":"","basis":"reviewed output map populated from inferred triples"}
	for _, tr := range triples {
		if tr.Subject == entityID {
			forwardPredicates[tr.Predicate] = tr.Object.(string)
		}
	}
	assert.Len(t, forwardPredicates, 3)
	assert.Equal(t, "c360.logistics.document.sensor.temperature.group", forwardPredicates[vocabulary.HierarchyTypeMember])
	assert.Equal(t, "c360.logistics.document.sensor.group.container", forwardPredicates[vocabulary.HierarchySystemMember])
	assert.Equal(t, "c360.logistics.sensor.group.container.level", forwardPredicates[vocabulary.HierarchyDomainMember])

	// Extract inverse edges (container → contains → entity)
	inversePredicates := make(map[string]string) // predicate-audit:unrelated {"column":23,"surface":"go-assignment:inversePredicates","value":"","basis":"reviewed output map populated from inferred triples"}
	for _, tr := range triples {
		if tr.Object == entityID {
			inversePredicates[tr.Predicate] = tr.Subject
		}
	}
	assert.Len(t, inversePredicates, 3)
	assert.Equal(t, "c360.logistics.document.sensor.temperature.group", inversePredicates[vocabulary.HierarchyTypeContains])
	assert.Equal(t, "c360.logistics.document.sensor.group.container", inversePredicates[vocabulary.HierarchySystemContains])
	assert.Equal(t, "c360.logistics.sensor.group.container.level", inversePredicates[vocabulary.HierarchyDomainContains])
}

func TestHierarchyInference_ContainerReuse(t *testing.T) {
	tripleAdder := &hierarchyMockTripleAdder{}
	entityManager := newMockEntityManager()

	config := HierarchyConfig{
		Enabled:           true,
		CreateTypeEdges:   true,
		CreateSystemEdges: false,
		CreateDomainEdges: false,
	}

	hi := NewHierarchyInference(entityManager, tripleAdder, config, nil)

	// Create first entity
	err := hi.OnEntityCreated(context.Background(), "c360.logistics.document.sensor.temperature.sensor-001")
	require.NoError(t, err)

	// Create second entity with same type prefix
	err = hi.OnEntityCreated(context.Background(), "c360.logistics.document.sensor.temperature.sensor-002")
	require.NoError(t, err)

	// Should only create 1 container (reused)
	createdEntities := entityManager.getCreatedEntities()
	assert.Len(t, createdEntities, 1)

	// Should have 4 edges: 2 forward (member) + 2 inverse (contains)
	triples := tripleAdder.getTriples()
	assert.Len(t, triples, 4)
}

func TestHierarchyInference_ContainerExistsInStorage(t *testing.T) {
	tripleAdder := &hierarchyMockTripleAdder{}
	entityManager := newMockEntityManager()

	// Pre-existing container in storage
	entityManager.addExistingEntity("c360.logistics.document.sensor.temperature.group")

	config := HierarchyConfig{
		Enabled:           true,
		CreateTypeEdges:   true,
		CreateSystemEdges: false,
		CreateDomainEdges: false,
	}

	hi := NewHierarchyInference(entityManager, tripleAdder, config, nil)

	err := hi.OnEntityCreated(context.Background(), "c360.logistics.document.sensor.temperature.sensor-001")
	require.NoError(t, err)

	// Should NOT create container (already exists)
	createdEntities := entityManager.getCreatedEntities()
	assert.Empty(t, createdEntities)

	// Should create 2 edges: forward (member) + inverse (contains)
	triples := tripleAdder.getTriples()
	assert.Len(t, triples, 2)
}

func TestHierarchyInference_ContainerEntityProperties(t *testing.T) {
	tripleAdder := &hierarchyMockTripleAdder{}
	entityManager := newMockEntityManager()

	config := HierarchyConfig{
		Enabled:           true,
		CreateTypeEdges:   true,
		CreateSystemEdges: false,
		CreateDomainEdges: false,
	}

	hi := NewHierarchyInference(entityManager, tripleAdder, config, nil)

	err := hi.OnEntityCreated(context.Background(), "c360.logistics.document.sensor.temperature.sensor-001")
	require.NoError(t, err)

	// Verify container entity has correct properties
	createdEntities := entityManager.getCreatedEntities()
	require.Len(t, createdEntities, 1)

	container := createdEntities[0]
	assert.Equal(t, "c360.logistics.document.sensor.temperature.group", container.ID)
	require.Len(t, container.Triples, 1)

	triple := container.Triples[0]
	assert.Equal(t, container.ID, triple.Subject)
	assert.Equal(t, "entity.type.class", triple.Predicate)
	assert.Equal(t, "hierarchy.container", triple.Object)
}

func TestHierarchyInference_ClearCache(t *testing.T) {
	tripleAdder := &hierarchyMockTripleAdder{}
	entityManager := newMockEntityManager()

	config := HierarchyConfig{
		Enabled:         true,
		CreateTypeEdges: true,
	}

	hi := NewHierarchyInference(entityManager, tripleAdder, config, nil)

	// Create entity to populate cache
	err := hi.OnEntityCreated(context.Background(), "c360.logistics.document.sensor.temperature.sensor-001")
	require.NoError(t, err)

	assert.Equal(t, 1, hi.GetCacheStats())

	// Clear cache
	hi.ClearCache()

	assert.Equal(t, 0, hi.GetCacheStats())
}

func TestHierarchyInference_GetMetrics(t *testing.T) {
	tripleAdder := &hierarchyMockTripleAdder{}
	entityManager := newMockEntityManager()

	config := HierarchyConfig{
		Enabled:           true,
		CreateTypeEdges:   true,
		CreateSystemEdges: true,
		CreateDomainEdges: true,
	}

	hi := NewHierarchyInference(entityManager, tripleAdder, config, nil)

	// Initial metrics
	containers, edges, failed := hi.GetMetrics()
	assert.Equal(t, int64(0), containers)
	assert.Equal(t, int64(0), edges)
	assert.Equal(t, int64(0), failed)

	// Create entity
	err := hi.OnEntityCreated(context.Background(), "c360.logistics.document.sensor.temperature.sensor-001")
	require.NoError(t, err)

	containers, edges, failed = hi.GetMetrics()
	assert.Equal(t, int64(3), containers) // 3 containers created
	assert.Equal(t, int64(6), edges)      // 6 edges created (3 forward + 3 inverse)
	assert.Equal(t, int64(0), failed)
}

func TestDefaultHierarchyConfig(t *testing.T) {
	config := DefaultHierarchyConfig()

	assert.False(t, config.Enabled) // Opt-in
	assert.True(t, config.CreateTypeEdges)
	assert.True(t, config.CreateSystemEdges)
	assert.True(t, config.CreateDomainEdges)
}

func TestBuildContainerIDs(t *testing.T) {
	tripleAdder := &hierarchyMockTripleAdder{}
	entityManager := newMockEntityManager()

	hi := NewHierarchyInference(entityManager, tripleAdder, DefaultHierarchyConfig(), nil)

	parts := []string{"org", "platform", "domain", "system", "type", "instance"}

	// Test type container ID
	typeID := hi.buildTypeContainerID(parts)
	assert.Equal(t, "org.platform.system.domain.type.group", typeID)

	// Test system container ID
	systemID := hi.buildSystemContainerID(parts)
	assert.Equal(t, "org.platform.domain.system.group.container", systemID)

	// Test domain container ID
	domainID := hi.buildDomainContainerID(parts)
	assert.Equal(t, "org.platform.domain.group.container.level", domainID)
}

func TestHierarchyInference_RaceConditionOnContainerCreate(t *testing.T) {
	tripleAdder := &hierarchyMockTripleAdder{}
	entityManager := newMockEntityManager()

	// Simulate race: container "exists" error during create
	entityManager.addExistingEntity("c360.logistics.document.sensor.temperature.group")

	config := HierarchyConfig{
		Enabled:           true,
		CreateTypeEdges:   true,
		CreateSystemEdges: false,
		CreateDomainEdges: false,
	}

	hi := NewHierarchyInference(entityManager, tripleAdder, config, nil)

	// Even if container exists, edges should still be created
	err := hi.OnEntityCreated(context.Background(), "c360.logistics.document.sensor.temperature.sensor-001")
	require.NoError(t, err)

	// Should create 2 edges: forward (member) + inverse (contains)
	triples := tripleAdder.getTriples()
	assert.Len(t, triples, 2)
}
