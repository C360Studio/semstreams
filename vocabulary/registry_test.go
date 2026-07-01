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

// TestRegisterAmend_OmittedFieldsRetained is the gh#410 regression: a re-Register
// that adds Description/IRI but OMITS the alias role must NOT strip that role.
// The exact failure semsource hit converging pkg/fusion: re-registering
// dc.terms.title with a description clobbered its label alias, so the NAME_INDEX
// (and thus graph.query.byName + graph.index.query.status readiness) went empty.
func TestRegisterAmend_OmittedFieldsRetained(t *testing.T) {
	defer SnapshotRegistry()()
	ClearRegistry()

	// Framework registers a label predicate.
	Register("dc.terms.title",
		WithDescription("A name given to the resource"),
		WithAlias(AliasTypeLabel, 1))

	// A product re-registers to attach its own description/IRI — WITHOUT
	// re-declaring the alias role (the footgun).
	Register("dc.terms.title",
		WithDescription("Product-specific title"),
		WithIRI("http://purl.org/dc/terms/title"))

	meta := GetPredicateMetadata("dc.terms.title")
	require.NotNil(t, meta)
	// New fields overrode.
	assert.Equal(t, "Product-specific title", meta.Description)
	assert.Equal(t, "http://purl.org/dc/terms/title", meta.StandardIRI)
	// Omitted alias role RETAINED (the fix).
	assert.True(t, meta.IsAlias, "label alias role must survive a role-less re-Register")
	assert.Equal(t, AliasTypeLabel, meta.AliasType)
	assert.Equal(t, 1, meta.AliasPriority)

	// Downstream: DiscoverLabelPredicates (what graph-index keys the NAME_INDEX
	// on) must still return the predicate — the actual signal the bug broke.
	labels := DiscoverLabelPredicates()
	priority, ok := labels["dc.terms.title"]
	assert.True(t, ok, "DiscoverLabelPredicates must still return the re-registered label predicate")
	assert.Equal(t, 1, priority)
}

// TestRegisterAmend_RoleAndWeightRetained guards the same clobber class for the
// increment-5b ranking signals (#408): a re-Register omitting WithRole/WithWeight
// must not strip a previously-declared salience.
func TestRegisterAmend_RoleAndWeightRetained(t *testing.T) {
	defer SnapshotRegistry()()
	ClearRegistry()

	Register("test.identity.serial",
		WithRole(RoleIdentity),
		WithWeight(1.5))

	// Re-register with only a description.
	Register("test.identity.serial",
		WithDescription("Serial number"))

	meta := GetPredicateMetadata("test.identity.serial")
	require.NotNil(t, meta)
	assert.Equal(t, "Serial number", meta.Description)
	assert.Equal(t, RoleIdentity, meta.Role, "role must survive a role-less re-Register")
	assert.Equal(t, 1.5, meta.Weight, "weight must survive a weight-less re-Register")
}

// TestRegisterAmend_OptionsStillOverride confirms amend does not prevent a
// re-Register from CHANGING a field (options win over the retained value).
func TestRegisterAmend_OptionsStillOverride(t *testing.T) {
	defer SnapshotRegistry()()
	ClearRegistry()

	Register("test.rel.parent", WithAlias(AliasTypeLabel, 1), WithRuleOpaque(true))
	// Re-register changing the alias priority and role-opacity is respected.
	Register("test.rel.parent", WithAlias(AliasTypeIdentity, 5))

	meta := GetPredicateMetadata("test.rel.parent")
	require.NotNil(t, meta)
	assert.Equal(t, AliasTypeIdentity, meta.AliasType, "changed alias type must override")
	assert.Equal(t, 5, meta.AliasPriority, "changed priority must override")
	assert.True(t, meta.RuleOpaque, "omitted RuleOpaque retained from prior registration")
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

// TestRegisterWithRoleAndWeight verifies the predicate-salience ranking signal
// (ADR-062 increment 5, gh#396 / semsource ask #2) round-trips through the
// registry: WithRole/WithWeight surface on PredicateMetadata, and undeclared
// predicates default to RoleUnspecified / weight 0.
func TestRegisterWithRoleAndWeight(t *testing.T) {
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

	Register("test.identity.serial",
		WithDescription("Serial number"),
		WithRole(RoleIdentity),
		WithWeight(1.0))

	// A predicate registered without the salience options keeps the zero values.
	Register("test.meta.updated",
		WithDescription("Last update timestamp"))

	meta := GetPredicateMetadata("test.identity.serial")
	require.NotNil(t, meta)
	assert.Equal(t, RoleIdentity, meta.Role)
	assert.Equal(t, 1.0, meta.Weight)

	neutral := GetPredicateMetadata("test.meta.updated")
	require.NotNil(t, neutral)
	assert.Equal(t, RoleUnspecified, neutral.Role, "undeclared role defaults to unspecified")
	assert.Equal(t, 0.0, neutral.Weight, "undeclared weight defaults to 0 (neutral)")

	// Unregistered predicates return nil metadata (no role/weight).
	assert.Nil(t, GetPredicateMetadata("test.unknown.field"))
}
