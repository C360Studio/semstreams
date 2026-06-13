package vocabulary

import (
	"sync"
)

// AliasType defines the semantic meaning of an alias predicate.
//
// Each type corresponds to standard W3C/RDF vocabularies for semantic web interoperability.
// See vocabulary/standards.go for IRI constants to use in PredicateMetadata.StandardIRI.
type AliasType string

const (
	// AliasTypeIdentity represents entity equivalence.
	//
	// Standard Mappings:
	//   - owl:sameAs (OWL_SAME_AS)
	//   - schema:sameAs (SCHEMA_SAME_AS)
	//
	// Used for: Federated entity IDs, external system UUIDs, cross-system identity
	// Resolution: ✅ Can resolve to entity IDs
	// Example: "uuid:abc-123" identifies the same entity as "c360.platform.test.drone.001"
	AliasTypeIdentity AliasType = "identity"

	// AliasTypeLabel represents human-readable display names.
	//
	// Standard Mappings:
	//   - skos:prefLabel (SKOS_PREF_LABEL) - preferred label
	//   - skos:altLabel (SKOS_ALT_LABEL) - alternative label
	//   - rdfs:label (RDFS_LABEL) - generic label
	//   - dc:title (DC_TITLE) - title
	//   - schema:name (SCHEMA_NAME) - name
	//   - foaf:name (FOAF_NAME) - name
	//
	// Used for: Display names, titles, human-readable descriptions
	// Resolution: ❌ NOT used for entity resolution (ambiguous - many entities can share labels)
	// Example: "Alpha Drone", "Battery 1" - for display only
	AliasTypeLabel AliasType = "label"

	// AliasTypeAlternate represents secondary unique identifiers.
	//
	// Standard Mappings:
	//   - schema:alternateName (SCHEMA_ALTERNATE_NAME)
	//   - dc:alternative (DC_ALTERNATIVE)
	//   - skos:notation (SKOS_NOTATION)
	//   - foaf:nick (FOAF_NICK)
	//
	// Used for: Model numbers, registration IDs, alternative unique identifiers
	// Resolution: ✅ Can resolve to entity IDs
	// Example: "MODEL-X1000", "REG-12345"
	AliasTypeAlternate AliasType = "alternate"

	// AliasTypeExternal represents external system identifiers.
	//
	// Standard Mappings:
	//   - dc:identifier (DC_IDENTIFIER)
	//   - schema:identifier (SCHEMA_IDENTIFIER)
	//   - dc:source (DC_SOURCE)
	//
	// Used for: Manufacturer serial numbers, legacy system IDs, third-party references
	// Resolution: ✅ Can resolve to entity IDs
	// Example: "SN-12345", "LEGACY-DB-ID-789", "VENDOR-REF-456"
	AliasTypeExternal AliasType = "external"

	// AliasTypeCommunication represents communication system identifiers.
	//
	// Standard Mappings:
	//   - foaf:accountName (FOAF_ACCOUNT_NAME)
	//
	// Used for: Radio call signs, network hostnames, MQTT client IDs, communication endpoints
	// Resolution: ✅ Can resolve to entity IDs
	// Example: "ALPHA-1" (call sign), "drone.local" (hostname), "mqtt-client-001"
	AliasTypeCommunication AliasType = "communication"
)

// CanResolveToEntityID returns true if this alias type can be used for entity resolution
func (at AliasType) CanResolveToEntityID() bool {
	switch at {
	case AliasTypeIdentity, AliasTypeAlternate, AliasTypeExternal, AliasTypeCommunication:
		return true // These can all resolve to entity IDs
	case AliasTypeLabel:
		return false // Labels are for display, not resolution (ambiguous)
	default:
		return false
	}
}

// String returns the string representation of the alias type
func (at AliasType) String() string {
	return string(at)
}

// Global predicate registry
var (
	registryMu        sync.RWMutex
	predicateRegistry = make(map[string]PredicateMetadata)
)

// Option is a functional option for configuring predicate registration.
type Option func(*PredicateMetadata)

// WithDescription sets the human-readable description of the predicate.
func WithDescription(desc string) Option {
	return func(m *PredicateMetadata) {
		m.Description = desc
	}
}

// WithDataType sets the expected Go type for the object value.
// Examples: "string", "float64", "int", "bool", "time.Time"
func WithDataType(dataType string) Option {
	return func(m *PredicateMetadata) {
		m.DataType = dataType
	}
}

// WithUnits specifies the measurement units (if applicable).
// Examples: "percent", "meters", "celsius", "pascals"
func WithUnits(units string) Option {
	return func(m *PredicateMetadata) {
		m.Units = units
	}
}

// WithRange describes valid value ranges (if applicable).
// Examples: "0-100", "-90 to 90", "positive"
func WithRange(valueRange string) Option {
	return func(m *PredicateMetadata) {
		m.Range = valueRange
	}
}

// WithIRI sets the W3C/RDF equivalent IRI for standards compliance.
// This enables RDF/JSON-LD export and semantic web interoperability.
// Use constants from standards.go for common vocabularies.
//
// Examples:
//   - WithIRI(OWL_SAME_AS)
//   - WithIRI("http://schema.org/batteryLevel")
//   - WithIRI(SKOS_PREF_LABEL)
func WithIRI(iri string) Option {
	return func(m *PredicateMetadata) {
		m.StandardIRI = iri
	}
}

// WithAlias marks this predicate as representing an entity alias.
// Aliases are used for entity resolution and identity correlation.
//
// Parameters:
//   - aliasType: The semantic meaning (identity, alternate, external, communication, label)
//   - priority: Conflict resolution order (lower number = higher priority)
//
// Example:
//
//	Register("robotics.communication.callsign",
//	    WithAlias(AliasTypeCommunication, 0))  // Highest priority
func WithAlias(aliasType AliasType, priority int) Option {
	return func(m *PredicateMetadata) {
		m.IsAlias = true
		m.AliasType = aliasType
		m.AliasPriority = priority
	}
}

// WithInverseOf declares the inverse predicate name.
// The inverse predicate should also be registered with its own metadata,
// pointing back to this predicate as its inverse.
//
// Example:
//
//	Register("hierarchy.type.member",
//	    WithIRI(SkosBroader),
//	    WithInverseOf("hierarchy.type.contains"))
//
//	Register("hierarchy.type.contains",
//	    WithIRI(SkosNarrower),
//	    WithInverseOf("hierarchy.type.member"))
//
// Note: The registry stores the inverse relationship but does not auto-generate
// inverse triples at runtime. Applications can use GetInversePredicate() to
// look up the inverse name for display or reasoning purposes.
func WithInverseOf(inversePredicate string) Option {
	return func(m *PredicateMetadata) {
		m.InverseOf = inversePredicate
	}
}

// WithRuleOpaque marks the predicate as carrying free-form content
// that rules must not predicate on. The rule-validator rejects rule
// conditions whose `field` names a rule-opaque predicate at
// config-load time. See ADR-036 for the asymmetric-ownership rationale.
//
// Example:
//
//	Register("agent.todo.content",
//	    WithDescription("Free-form todo description"),
//	    WithDataType("string"),
//	    WithRuleOpaque(true))
func WithRuleOpaque(opaque bool) Option {
	return func(m *PredicateMetadata) {
		m.RuleOpaque = opaque
	}
}

// WithSymmetric marks the predicate as symmetric (its own inverse).
// Symmetric predicates imply bidirectional relationships: if A relates to B,
// then B relates to A with the same predicate.
//
// Example:
//
//	Register("hierarchy.type.sibling",
//	    WithIRI(SkosRelated),
//	    WithSymmetric(true))
//
// When IsSymmetric is true, GetInversePredicate() returns the predicate itself.
// Do not set both WithSymmetric(true) and WithInverseOf() on the same predicate.
func WithSymmetric(symmetric bool) Option {
	return func(m *PredicateMetadata) {
		m.IsSymmetric = symmetric
	}
}

// Register registers a predicate with its metadata in the global registry.
// This should be called during package initialization (init functions) by domain vocabularies.
//
// The predicate name must follow three-level dotted notation: domain.category.property
//
// If a predicate is already registered, it will be overwritten (enables domain-specific overrides).
//
// Example:
//
//	Register("robotics.battery.level",
//	    WithDescription("Battery charge level percentage"),
//	    WithDataType("float64"),
//	    WithUnits("percent"),
//	    WithRange("0-100"),
//	    WithIRI("http://schema.org/batteryLevel"))
func Register(name string, opts ...Option) {
	// Parse domain and category from name
	domain, category := parseDomainCategory(name)

	// Create base metadata
	meta := PredicateMetadata{
		Name:     name,
		Domain:   domain,
		Category: category,
	}

	// Apply functional options
	for _, opt := range opts {
		opt(&meta)
	}

	// Store in registry (allows overriding framework defaults)
	registryMu.Lock()
	defer registryMu.Unlock()

	predicateRegistry[name] = meta
}

// parseDomainCategory extracts domain and category from dotted predicate name.
// For "sensor.temperature.celsius", returns ("sensor", "temperature").
func parseDomainCategory(name string) (domain, category string) {
	// Split on dots
	parts := []rune(name)
	firstDot := -1
	secondDot := -1

	for i, r := range parts {
		if r == '.' {
			if firstDot == -1 {
				firstDot = i
			} else if secondDot == -1 {
				secondDot = i
				break
			}
		}
	}

	if firstDot == -1 {
		return "", ""
	}

	domain = name[:firstDot]

	if secondDot == -1 {
		// Only two parts - invalid predicate format
		return domain, ""
	}

	category = name[firstDot+1 : secondDot]
	return domain, category
}

// RegisterPredicate registers a predicate using the PredicateMetadata struct directly.
// This function is provided for backward compatibility and testing.
// New code should use Register() with functional options.
// Allows overriding framework defaults.
func RegisterPredicate(meta PredicateMetadata) {
	registryMu.Lock()
	defer registryMu.Unlock()

	predicateRegistry[meta.Name] = meta
}

// GetPredicateMetadata retrieves metadata for a predicate from the registry.
// Returns nil if the predicate is not registered.
// This function is thread-safe and can be called concurrently.
func GetPredicateMetadata(predicate string) *PredicateMetadata {
	registryMu.RLock()
	defer registryMu.RUnlock()

	if meta, exists := predicateRegistry[predicate]; exists {
		// Return a copy to prevent external modification
		metaCopy := meta
		return &metaCopy
	}

	return nil
}

// ListRegisteredPredicates returns a list of all registered predicate names.
// Useful for debugging and introspection.
func ListRegisteredPredicates() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()

	predicates := make([]string, 0, len(predicateRegistry))
	for name := range predicateRegistry {
		predicates = append(predicates, name)
	}
	return predicates
}

// DiscoverAliasPredicates discovers all predicates marked as aliases in the registry.
// Returns a map of predicate name to priority (lower number = higher priority).
// Used by AliasIndex to determine which predicates to index.
//
// If no alias predicates are registered, returns an empty map.
// Applications must register their domain-specific alias predicates using RegisterPredicate().
func DiscoverAliasPredicates() map[string]int {
	registryMu.RLock()
	defer registryMu.RUnlock()

	aliasPredicates := make(map[string]int)

	for name, meta := range predicateRegistry {
		if meta.IsAlias && meta.AliasType.CanResolveToEntityID() {
			aliasPredicates[name] = meta.AliasPriority
		}
	}

	return aliasPredicates
}

// GetInversePredicate returns the inverse predicate name, if defined.
// Returns an empty string if no inverse is defined for the given predicate.
//
// For symmetric predicates (IsSymmetric=true), returns the predicate itself
// since symmetric predicates are their own inverse.
//
// Example:
//
//	GetInversePredicate("hierarchy.type.member")   // Returns "hierarchy.type.contains"
//	GetInversePredicate("hierarchy.type.sibling")  // Returns "hierarchy.type.sibling" (symmetric)
//	GetInversePredicate("sensor.temperature.celsius")  // Returns "" (no inverse)
func GetInversePredicate(predicate string) string {
	registryMu.RLock()
	defer registryMu.RUnlock()

	meta, exists := predicateRegistry[predicate]
	if !exists {
		return ""
	}

	if meta.IsSymmetric {
		return predicate
	}
	return meta.InverseOf
}

// InverseResolver reports a predicate's registered inverse in the (inverse, ok)
// shape the ADR-056 Decision-4 inverse-gate expects (assignable to
// ownership.InverseResolver). ok is false when no inverse is registered. Wiring
// this into ownership.EnsureBuckets keeps pkg/ownership free of the vocabulary
// dependency while the gate consults the real registry.
func InverseResolver(predicate string) (inverse string, ok bool) {
	inv := GetInversePredicate(predicate)
	return inv, inv != ""
}

// IsRuleOpaque reports whether a predicate is marked rule-opaque.
// Returns false for unregistered predicates so that vocabulary
// registration is the only path to opacity — undeclared predicates
// stay rule-matchable by default. See ADR-036.
func IsRuleOpaque(predicate string) bool {
	registryMu.RLock()
	defer registryMu.RUnlock()

	meta, exists := predicateRegistry[predicate]
	if !exists {
		return false
	}
	return meta.RuleOpaque
}

// IsSymmetricPredicate checks if a predicate is symmetric.
// Symmetric predicates represent bidirectional relationships where
// if A relates to B, then B also relates to A with the same predicate.
//
// Returns false if the predicate is not registered or is not symmetric.
func IsSymmetricPredicate(predicate string) bool {
	registryMu.RLock()
	defer registryMu.RUnlock()

	meta, exists := predicateRegistry[predicate]
	if !exists {
		return false
	}
	return meta.IsSymmetric
}

// HasInverse checks if a predicate has an inverse defined (either explicit or symmetric).
// Returns true if the predicate is symmetric or has an InverseOf set.
func HasInverse(predicate string) bool {
	registryMu.RLock()
	defer registryMu.RUnlock()

	meta, exists := predicateRegistry[predicate]
	if !exists {
		return false
	}
	return meta.IsSymmetric || meta.InverseOf != ""
}

// DiscoverInversePredicates returns all predicates that have inverses defined.
// Returns a map where keys are predicate names and values are their inverse predicate names.
// For symmetric predicates, the value equals the key.
//
// This function is useful for:
//   - Debugging and introspection
//   - Generating documentation about predicate relationships
//   - Reasoning systems that need to traverse relationships bidirectionally
//
// Example output:
//
//	{
//	    "hierarchy.type.member": "hierarchy.type.contains",
//	    "hierarchy.type.contains": "hierarchy.type.member",
//	    "hierarchy.type.sibling": "hierarchy.type.sibling",  // symmetric
//	}
func DiscoverInversePredicates() map[string]string {
	registryMu.RLock()
	defer registryMu.RUnlock()

	inverses := make(map[string]string)
	for name, meta := range predicateRegistry {
		if meta.IsSymmetric {
			inverses[name] = name
		} else if meta.InverseOf != "" {
			inverses[name] = meta.InverseOf
		}
	}
	return inverses
}

// ClearRegistry clears all registered predicates.
// This is primarily useful for testing.
func ClearRegistry() {
	registryMu.Lock()
	defer registryMu.Unlock()
	predicateRegistry = make(map[string]PredicateMetadata)
}

// SnapshotRegistry captures the current registry state and returns a
// restore function. Intended for tests that register predicates
// transiently and need to undo without leaving stub entries behind.
//
// Example:
//
//	defer vocabulary.SnapshotRegistry()()
//	vocabulary.Register("test.foo.bar", ...)
//	// test runs; deferred restore reverts the registry to its pre-test state.
func SnapshotRegistry() func() {
	registryMu.RLock()
	saved := make(map[string]PredicateMetadata, len(predicateRegistry))
	for k, v := range predicateRegistry {
		saved[k] = v
	}
	registryMu.RUnlock()

	return func() {
		registryMu.Lock()
		predicateRegistry = saved
		registryMu.Unlock()
	}
}
