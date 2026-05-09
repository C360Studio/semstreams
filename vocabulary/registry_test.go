package vocabulary

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegisterWithInverseOf(t *testing.T) {
	// Save and restore registry state
	originalRegistry := make(map[string]PredicateMetadata)
	registryMu.RLock()
	for k, v := range predicateRegistry {
		originalRegistry[k] = v
	}
	registryMu.RUnlock()
	defer func() {
		registryMu.Lock()
		predicateRegistry = originalRegistry
		registryMu.Unlock()
	}()

	// Clear registry for isolated test
	ClearRegistry()

	// Register a predicate with inverse
	Register("test.rel.parent",
		WithDescription("Parent relationship"),
		WithInverseOf("test.rel.child"))

	// Verify registration
	meta := GetPredicateMetadata("test.rel.parent")
	require.NotNil(t, meta)
	assert.Equal(t, "test.rel.child", meta.InverseOf)
	assert.False(t, meta.IsSymmetric)
}

func TestRegisterWithSymmetric(t *testing.T) {
	originalRegistry := make(map[string]PredicateMetadata)
	registryMu.RLock()
	for k, v := range predicateRegistry {
		originalRegistry[k] = v
	}
	registryMu.RUnlock()
	defer func() {
		registryMu.Lock()
		predicateRegistry = originalRegistry
		registryMu.Unlock()
	}()

	ClearRegistry()

	// Register a symmetric predicate
	Register("test.rel.sibling",
		WithDescription("Sibling relationship"),
		WithSymmetric(true))

	meta := GetPredicateMetadata("test.rel.sibling")
	require.NotNil(t, meta)
	assert.True(t, meta.IsSymmetric)
	assert.Empty(t, meta.InverseOf) // Symmetric predicates don't need InverseOf
}

func TestGetInversePredicateWithSymmetric(t *testing.T) {
	originalRegistry := make(map[string]PredicateMetadata)
	registryMu.RLock()
	for k, v := range predicateRegistry {
		originalRegistry[k] = v
	}
	registryMu.RUnlock()
	defer func() {
		registryMu.Lock()
		predicateRegistry = originalRegistry
		registryMu.Unlock()
	}()

	ClearRegistry()

	// Register a symmetric predicate
	Register("test.rel.sibling", WithSymmetric(true))

	// GetInversePredicate should return the predicate itself for symmetric
	inverse := GetInversePredicate("test.rel.sibling")
	assert.Equal(t, "test.rel.sibling", inverse)
}

func TestGetInversePredicateWithExplicitInverse(t *testing.T) {
	originalRegistry := make(map[string]PredicateMetadata)
	registryMu.RLock()
	for k, v := range predicateRegistry {
		originalRegistry[k] = v
	}
	registryMu.RUnlock()
	defer func() {
		registryMu.Lock()
		predicateRegistry = originalRegistry
		registryMu.Unlock()
	}()

	ClearRegistry()

	// Register predicates with explicit inverse
	Register("test.rel.member", WithInverseOf("test.rel.contains"))
	Register("test.rel.contains", WithInverseOf("test.rel.member"))

	// Test GetInversePredicate
	assert.Equal(t, "test.rel.contains", GetInversePredicate("test.rel.member"))
	assert.Equal(t, "test.rel.member", GetInversePredicate("test.rel.contains"))
}

func TestDiscoverInversePredicatesIsolated(t *testing.T) {
	originalRegistry := make(map[string]PredicateMetadata)
	registryMu.RLock()
	for k, v := range predicateRegistry {
		originalRegistry[k] = v
	}
	registryMu.RUnlock()
	defer func() {
		registryMu.Lock()
		predicateRegistry = originalRegistry
		registryMu.Unlock()
	}()

	ClearRegistry()

	// Register various predicates
	Register("test.rel.member", WithInverseOf("test.rel.contains"))
	Register("test.rel.contains", WithInverseOf("test.rel.member"))
	Register("test.rel.sibling", WithSymmetric(true))
	Register("test.data.value") // No inverse

	inverses := DiscoverInversePredicates()

	// Should have 3 predicates with inverses
	assert.Len(t, inverses, 3)
	assert.Equal(t, "test.rel.contains", inverses["test.rel.member"])
	assert.Equal(t, "test.rel.member", inverses["test.rel.contains"])
	assert.Equal(t, "test.rel.sibling", inverses["test.rel.sibling"])

	// test.data.value should not be in the map
	_, exists := inverses["test.data.value"]
	assert.False(t, exists)
}

func TestHasInverseFunction(t *testing.T) {
	originalRegistry := make(map[string]PredicateMetadata)
	registryMu.RLock()
	for k, v := range predicateRegistry {
		originalRegistry[k] = v
	}
	registryMu.RUnlock()
	defer func() {
		registryMu.Lock()
		predicateRegistry = originalRegistry
		registryMu.Unlock()
	}()

	ClearRegistry()

	Register("test.rel.member", WithInverseOf("test.rel.contains"))
	Register("test.rel.sibling", WithSymmetric(true))
	Register("test.data.value") // No inverse

	assert.True(t, HasInverse("test.rel.member"))
	assert.True(t, HasInverse("test.rel.sibling"))
	assert.False(t, HasInverse("test.data.value"))
	assert.False(t, HasInverse("nonexistent.predicate.name"))
}

func TestIsSymmetricPredicateFunction(t *testing.T) {
	originalRegistry := make(map[string]PredicateMetadata)
	registryMu.RLock()
	for k, v := range predicateRegistry {
		originalRegistry[k] = v
	}
	registryMu.RUnlock()
	defer func() {
		registryMu.Lock()
		predicateRegistry = originalRegistry
		registryMu.Unlock()
	}()

	ClearRegistry()

	Register("test.rel.member", WithInverseOf("test.rel.contains"))
	Register("test.rel.sibling", WithSymmetric(true))

	assert.False(t, IsSymmetricPredicate("test.rel.member"))
	assert.True(t, IsSymmetricPredicate("test.rel.sibling"))
	assert.False(t, IsSymmetricPredicate("nonexistent.predicate.name"))
}

func TestCombineMultipleOptions(t *testing.T) {
	originalRegistry := make(map[string]PredicateMetadata)
	registryMu.RLock()
	for k, v := range predicateRegistry {
		originalRegistry[k] = v
	}
	registryMu.RUnlock()
	defer func() {
		registryMu.Lock()
		predicateRegistry = originalRegistry
		registryMu.Unlock()
	}()

	ClearRegistry()

	// Register with multiple options including inverse
	Register("test.rel.parent",
		WithDescription("Parent-child relationship"),
		WithDataType("string"),
		WithIRI("http://example.org/parent"),
		WithInverseOf("test.rel.child"))

	meta := GetPredicateMetadata("test.rel.parent")
	require.NotNil(t, meta)
	assert.Equal(t, "Parent-child relationship", meta.Description)
	assert.Equal(t, "string", meta.DataType)
	assert.Equal(t, "http://example.org/parent", meta.StandardIRI)
	assert.Equal(t, "test.rel.child", meta.InverseOf)
	assert.Equal(t, "test", meta.Domain)
	assert.Equal(t, "rel", meta.Category)
}

func TestRegisterOverwrite(t *testing.T) {
	originalRegistry := make(map[string]PredicateMetadata)
	registryMu.RLock()
	for k, v := range predicateRegistry {
		originalRegistry[k] = v
	}
	registryMu.RUnlock()
	defer func() {
		registryMu.Lock()
		predicateRegistry = originalRegistry
		registryMu.Unlock()
	}()

	ClearRegistry()

	// First registration
	Register("test.rel.member",
		WithDescription("Original description"),
		WithInverseOf("test.rel.contains"))

	// Overwrite with new registration
	Register("test.rel.member",
		WithDescription("Updated description"),
		WithInverseOf("test.rel.includes"))

	meta := GetPredicateMetadata("test.rel.member")
	require.NotNil(t, meta)
	assert.Equal(t, "Updated description", meta.Description)
	assert.Equal(t, "test.rel.includes", meta.InverseOf)
}

func TestSymmetricWithIRI(t *testing.T) {
	originalRegistry := make(map[string]PredicateMetadata)
	registryMu.RLock()
	for k, v := range predicateRegistry {
		originalRegistry[k] = v
	}
	registryMu.RUnlock()
	defer func() {
		registryMu.Lock()
		predicateRegistry = originalRegistry
		registryMu.Unlock()
	}()

	ClearRegistry()

	// Register symmetric predicate with SKOS related IRI
	Register("test.rel.sibling",
		WithDescription("Sibling entities"),
		WithIRI(SkosRelated),
		WithSymmetric(true))

	meta := GetPredicateMetadata("test.rel.sibling")
	require.NotNil(t, meta)
	assert.Equal(t, SkosRelated, meta.StandardIRI)
	assert.True(t, meta.IsSymmetric)

	// GetInversePredicate should return itself
	assert.Equal(t, "test.rel.sibling", GetInversePredicate("test.rel.sibling"))
}

func TestRegisterWithRuleOpaque(t *testing.T) {
	originalRegistry := make(map[string]PredicateMetadata)
	registryMu.RLock()
	for k, v := range predicateRegistry {
		originalRegistry[k] = v
	}
	registryMu.RUnlock()
	defer func() {
		registryMu.Lock()
		predicateRegistry = originalRegistry
		registryMu.Unlock()
	}()

	ClearRegistry()

	// Register an opaque predicate (free-form content, ADR-036)
	Register("test.todo.content",
		WithDescription("Free-form content"),
		WithDataType("string"),
		WithRuleOpaque(true))

	// Register a structural predicate (rule-matchable)
	Register("test.todo.status",
		WithDescription("Status enum"),
		WithDataType("string"))

	meta := GetPredicateMetadata("test.todo.content")
	require.NotNil(t, meta)
	assert.True(t, meta.RuleOpaque)

	meta2 := GetPredicateMetadata("test.todo.status")
	require.NotNil(t, meta2)
	assert.False(t, meta2.RuleOpaque)

	// IsRuleOpaque convenience query
	assert.True(t, IsRuleOpaque("test.todo.content"))
	assert.False(t, IsRuleOpaque("test.todo.status"))

	// Unregistered predicates default to non-opaque (opacity is opt-in
	// at registration time per ADR-036)
	assert.False(t, IsRuleOpaque("test.unknown.field"))
}
