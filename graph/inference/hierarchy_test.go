package inference

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	semtypes "github.com/c360studio/semstreams/pkg/types"
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
// hierarchyTestOrg / hierarchyTestPlatform are the deployment authority every
// enabled HierarchyConfig now declares (ADR-102): inference mints containers
// and sibling edges from the ingested entity's own prefix, so it must know
// which prefix is this deployment's. They match positions 1-2 of every entity
// ID in this package's hierarchy fixtures — change one and the other must
// follow, or the fixture becomes an import and mints nothing.
const (
	hierarchyTestOrg      = "c360"
	hierarchyTestPlatform = "logistics"
)

// TestGetHierarchyTriplesSkipsForeignAuthority is the DISCRIMINATING test for
// the ADR-102 skip, and it lives here rather than at the graph-ingest seam for a
// measured reason: at that seam the skip is shadowed. graph-ingest's own
// authority gate refuses every container birth under a peer's pair, which makes
// GetHierarchyTriples return a joined error, and the merge path then discards
// the WHOLE triple set on any error (component.go, "Failed to get hierarchy
// triples") — so an imported entity ends up with no hierarchy triples whether
// this check exists or not. Deleting the check is invisible there and visible
// here.
//
// It also covers the case graph-ingest cannot: this is exported framework
// surface, and a consumer calling GetHierarchyTriples directly has no second
// layer behind it.
func TestGetHierarchyTriplesSkipsForeignAuthority(t *testing.T) {
	entityManager := newMockEntityManager()
	tripleAdder := &hierarchyMockTripleAdder{}
	hi := NewHierarchyInference(entityManager, tripleAdder, HierarchyConfig{
		Org:                hierarchyTestOrg,
		Platform:           hierarchyTestPlatform,
		Enabled:            true,
		CreateTypeEdges:    true,
		CreateSystemEdges:  true,
		CreateDomainEdges:  true,
		CreateTypeSiblings: true,
	}, nil)

	// A peer deployment's entity: same org, different platform, canonical shape.
	const imported = hierarchyTestOrg + ".dep9.sensor.document.temperature.sensor-001"

	triples, err := hi.GetHierarchyTriples(context.Background(), imported)

	require.NoError(t, err, "a foreign entity is skipped, not rejected")
	assert.Empty(t, triples, "no membership or sibling triple may be minted for an imported entity")
	assert.Empty(t, entityManager.getCreatedEntities(),
		"no container entity may be born under a peer's authority")
	assert.Empty(t, tripleAdder.getTriples(),
		"no inverse edge may be written for an imported entity")

	// The same shape under THIS deployment's authority still mints, so the skip
	// is authority-scoped rather than a blanket disable.
	local := hierarchyTestOrg + "." + hierarchyTestPlatform + ".sensor.document.temperature.sensor-001"
	localTriples, err := hi.GetHierarchyTriples(context.Background(), local)
	require.NoError(t, err)
	assert.NotEmpty(t, localTriples, "a local entity still receives hierarchy triples")
}

func TestHierarchyInference_InverseEdgeWriteFailureIsNonFatal(t *testing.T) {
	// Every in-process inverse-edge write rejects — simulating a must-exist
	// "entity not found" on the sibling/container target.
	failingAdder := &hierarchyMockTripleAdder{err: errors.New("entity not found (simulated must-exist rejection)")}
	entityManager := newMockEntityManager()
	// An existing sibling so createSiblingEdges attempts an inverse back-edge.
	entityManager.addExistingEntity("c360.logistics.sensor.environmental.temperature.temp-002")

	config := HierarchyConfig{
		Org:                hierarchyTestOrg,
		Platform:           hierarchyTestPlatform,
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
		Org:             hierarchyTestOrg,
		Platform:        hierarchyTestPlatform,
		Enabled:         false, // Disabled
		CreateTypeEdges: true,
	}

	hi := NewHierarchyInference(entityManager, tripleAdder, config, nil)

	err := hi.OnEntityCreated(context.Background(), "c360.logistics.sensor.document.temperature.sensor-001")
	require.NoError(t, err)

	// No triples should be added when disabled
	assert.Empty(t, tripleAdder.getTriples())
	assert.Empty(t, entityManager.getCreatedEntities())
}

func TestHierarchyInference_OnEntityCreated_InvalidEntityID(t *testing.T) {
	tripleAdder := &hierarchyMockTripleAdder{}
	entityManager := newMockEntityManager()

	config := HierarchyConfig{
		Org:             hierarchyTestOrg,
		Platform:        hierarchyTestPlatform,
		Enabled:         true,
		CreateTypeEdges: true,
	}

	hi := NewHierarchyInference(entityManager, tripleAdder, config, nil)

	// 5-part entity ID should be skipped
	err := hi.OnEntityCreated(context.Background(), "c360.logistics.sensor.document.temperature")
	require.NoError(t, err)
	assert.Empty(t, tripleAdder.getTriples())

	// 7-part entity ID should be skipped
	err = hi.OnEntityCreated(context.Background(), "c360.logistics.sensor.document.temperature.zone.sensor")
	require.NoError(t, err)
	assert.Empty(t, tripleAdder.getTriples())
}

func TestHierarchyInference_OnEntityCreated_TypeEdgeOnly(t *testing.T) {
	tripleAdder := &hierarchyMockTripleAdder{}
	entityManager := newMockEntityManager()

	config := HierarchyConfig{
		Org:               hierarchyTestOrg,
		Platform:          hierarchyTestPlatform,
		Enabled:           true,
		CreateTypeEdges:   true,
		CreateSystemEdges: false,
		CreateDomainEdges: false,
	}

	hi := NewHierarchyInference(entityManager, tripleAdder, config, nil)

	entityID := "c360.logistics.sensor.document.temperature.sensor-001"
	containerID := "c360.logistics.sensor.document.temperature.group"
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
		Org:               hierarchyTestOrg,
		Platform:          hierarchyTestPlatform,
		Enabled:           true,
		CreateTypeEdges:   true,
		CreateSystemEdges: true,
		CreateDomainEdges: true,
	}

	hi := NewHierarchyInference(entityManager, tripleAdder, config, nil)

	entityID := "c360.logistics.sensor.document.temperature.sensor-001"
	err := hi.OnEntityCreated(context.Background(), entityID)
	require.NoError(t, err)

	// Should create 3 containers
	createdEntities := entityManager.getCreatedEntities()
	assert.Len(t, createdEntities, 3)

	containerIDs := make(map[string]bool)
	for _, e := range createdEntities {
		containerIDs[e.ID] = true
	}
	assert.True(t, containerIDs["c360.logistics.sensor.document.temperature.group"]) // Type
	assert.True(t, containerIDs["c360.logistics.sensor.document.group.container"])   // System
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
	assert.Equal(t, "c360.logistics.sensor.document.temperature.group", forwardPredicates[vocabulary.HierarchyTypeMember])
	assert.Equal(t, "c360.logistics.sensor.document.group.container", forwardPredicates[vocabulary.HierarchySystemMember])
	assert.Equal(t, "c360.logistics.sensor.group.container.level", forwardPredicates[vocabulary.HierarchyDomainMember])

	// Extract inverse edges (container → contains → entity)
	inversePredicates := make(map[string]string) // predicate-audit:unrelated {"column":23,"surface":"go-assignment:inversePredicates","value":"","basis":"reviewed output map populated from inferred triples"}
	for _, tr := range triples {
		if tr.Object == entityID {
			inversePredicates[tr.Predicate] = tr.Subject
		}
	}
	assert.Len(t, inversePredicates, 3)
	assert.Equal(t, "c360.logistics.sensor.document.temperature.group", inversePredicates[vocabulary.HierarchyTypeContains])
	assert.Equal(t, "c360.logistics.sensor.document.group.container", inversePredicates[vocabulary.HierarchySystemContains])
	assert.Equal(t, "c360.logistics.sensor.group.container.level", inversePredicates[vocabulary.HierarchyDomainContains])
}

func TestHierarchyInference_ContainerReuse(t *testing.T) {
	tripleAdder := &hierarchyMockTripleAdder{}
	entityManager := newMockEntityManager()

	config := HierarchyConfig{
		Org:               hierarchyTestOrg,
		Platform:          hierarchyTestPlatform,
		Enabled:           true,
		CreateTypeEdges:   true,
		CreateSystemEdges: false,
		CreateDomainEdges: false,
	}

	hi := NewHierarchyInference(entityManager, tripleAdder, config, nil)

	// Create first entity
	err := hi.OnEntityCreated(context.Background(), "c360.logistics.sensor.document.temperature.sensor-001")
	require.NoError(t, err)

	// Create second entity with same type prefix
	err = hi.OnEntityCreated(context.Background(), "c360.logistics.sensor.document.temperature.sensor-002")
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
	entityManager.addExistingEntity("c360.logistics.sensor.document.temperature.group")

	config := HierarchyConfig{
		Org:               hierarchyTestOrg,
		Platform:          hierarchyTestPlatform,
		Enabled:           true,
		CreateTypeEdges:   true,
		CreateSystemEdges: false,
		CreateDomainEdges: false,
	}

	hi := NewHierarchyInference(entityManager, tripleAdder, config, nil)

	err := hi.OnEntityCreated(context.Background(), "c360.logistics.sensor.document.temperature.sensor-001")
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
		Org:               hierarchyTestOrg,
		Platform:          hierarchyTestPlatform,
		Enabled:           true,
		CreateTypeEdges:   true,
		CreateSystemEdges: false,
		CreateDomainEdges: false,
	}

	hi := NewHierarchyInference(entityManager, tripleAdder, config, nil)

	err := hi.OnEntityCreated(context.Background(), "c360.logistics.sensor.document.temperature.sensor-001")
	require.NoError(t, err)

	// Verify container entity has correct properties
	createdEntities := entityManager.getCreatedEntities()
	require.Len(t, createdEntities, 1)

	container := createdEntities[0]
	assert.Equal(t, "c360.logistics.sensor.document.temperature.group", container.ID)
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
		Org:             hierarchyTestOrg,
		Platform:        hierarchyTestPlatform,
		Enabled:         true,
		CreateTypeEdges: true,
	}

	hi := NewHierarchyInference(entityManager, tripleAdder, config, nil)

	// Create entity to populate cache
	err := hi.OnEntityCreated(context.Background(), "c360.logistics.sensor.document.temperature.sensor-001")
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
		Org:               hierarchyTestOrg,
		Platform:          hierarchyTestPlatform,
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
	err := hi.OnEntityCreated(context.Background(), "c360.logistics.sensor.document.temperature.sensor-001")
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

// TestBuildContainerIDs pins each container to its NAMED prefix level under the
// canonical order org.platform.system.domain.type.instance (ADR-102). The
// fixture deliberately gives positions 3 and 4 distinguishable values ("src"
// and "dom") so a builder that read them in the retired order would produce a
// different string — the previous fixture named them "domain"/"system" in
// position order, which made every assertion here order-blind.
func TestBuildContainerIDs(t *testing.T) {
	eid := semtypes.EntityID{
		Org: "org", Platform: "platform",
		System: "src", Domain: "dom", Type: "type", Instance: "instance",
	}
	require.Equal(t, "org.platform.src.dom.type.instance", eid.Key())

	// Level 5 (type prefix) + one padding token.
	assert.Equal(t, "org.platform.src.dom.type.group", buildTypeContainerID(eid))

	// Level 4 (taxonomy prefix) + two padding tokens. Reached through the
	// retired-name CreateSystemEdges field and hierarchy.system.member (H7).
	assert.Equal(t, "org.platform.src.dom.group.container", buildTaxonomyContainerID(eid))

	// Level 3 (source prefix) + three padding tokens. Reached through the
	// retired-name CreateDomainEdges field and hierarchy.domain.member (H7).
	assert.Equal(t, "org.platform.src.group.container.level", buildSourceContainerID(eid))
}

func TestHierarchyInference_RaceConditionOnContainerCreate(t *testing.T) {
	tripleAdder := &hierarchyMockTripleAdder{}
	entityManager := newMockEntityManager()

	// Simulate race: container "exists" error during create
	entityManager.addExistingEntity("c360.logistics.sensor.document.temperature.group")

	config := HierarchyConfig{
		Org:               hierarchyTestOrg,
		Platform:          hierarchyTestPlatform,
		Enabled:           true,
		CreateTypeEdges:   true,
		CreateSystemEdges: false,
		CreateDomainEdges: false,
	}

	hi := NewHierarchyInference(entityManager, tripleAdder, config, nil)

	// Even if container exists, edges should still be created
	err := hi.OnEntityCreated(context.Background(), "c360.logistics.sensor.document.temperature.sensor-001")
	require.NoError(t, err)

	// Should create 2 edges: forward (member) + inverse (contains)
	triples := tripleAdder.getTriples()
	assert.Len(t, triples, 2)
}
