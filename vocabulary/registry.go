package vocabulary

import (
	"fmt"
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
//	Register("agent.todo.record",
//	    WithDescription("Free-form private todo record"),
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

// WithRole declares the predicate's semantic role — a stored, first-class
// ranking signal (ADR-062 increment 5, gh#396 / semsource ask #2). Consumers
// read PredicateMetadata.Role instead of pattern-matching predicate names.
//
// Example:
//
//	Register("robotics.identity.serial",
//	    WithRole(RoleIdentity),
//	    WithWeight(1.0))
func WithRole(role PredicateRole) Option {
	return func(m *PredicateMetadata) {
		m.Role = role
	}
}

// WithWeight declares the predicate's salience weight, SIGNED: positive = more
// salient (boost), 0 = neutral, NEGATIVE = down-rank (demote). Lets a ranker
// prefer entities carrying salient facts — or push down structurally-
// identifiable noise (tests, generated code, mocks) that carries the same
// boosted predicates as the real thing — without a per-product weight table
// (ADR-062 increment 5, semsource ask #2; negative weights gh#441). The fusion
// ranker folds an entity's strongest boost and strongest demotion together, so
// a negative weight is a bounded reordering, never an exclusion.
//
// Example (demote tests below their impl, both carrying the same doc-comment
// salience):
//
//	Register("code.artifact.test", WithWeight(-1.5))
func WithWeight(weight float64) Option {
	return func(m *PredicateMetadata) {
		m.Weight = weight
	}
}

// Register registers (or amends) a predicate's metadata in the global registry.
// This should be called during package initialization (init functions) by domain vocabularies.
//
// The predicate name must follow three-level dotted notation: domain.category.property
//
// AMEND semantics (gh#410): a re-Register of an already-registered predicate
// starts from the EXISTING metadata, then applies the given options. Options
// override every field they set; fields the re-Register OMITS are RETAINED, not
// cleared. This lets a product attach its own Description/IRI to a
// framework-registered predicate WITHOUT silently stripping the framework's
// declared roles (e.g. a WithAlias(AliasTypeLabel, …) on dc.terms.title, which
// the NAME_INDEX / graph.query.byName / fusion-readiness path depends on). A
// first registration starts from a zero struct, so single-registration behavior
// is unchanged.
//
// Amend cannot CLEAR a field by omitting its option; pass the explicit zero
// (WithRuleOpaque(false), WithWeight(0), WithDescription(""), …) to clear one.
// The alias flag has no un-alias option; to fully REPLACE a registration
// (including removing an alias role), use RegisterPredicate, which overwrites.
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
	parts, err := ParsePredicate(name)
	if err != nil {
		panic(fmt.Sprintf("register predicate %q: %v", name, err))
	}

	registryMu.Lock()
	defer registryMu.Unlock()

	// Seed from the existing registration when present (zero value if absent), so
	// options amend rather than replace — a re-Register that omits a field keeps
	// the previously-declared value instead of silently dropping it (gh#410).
	meta := predicateRegistry[name]
	meta.Name = name
	meta.Domain = parts.Domain
	meta.Category = parts.Category

	// Apply functional options (each overrides the field it sets).
	for _, opt := range opts {
		opt(&meta)
	}
	if err := validatePredicateMetadataLocked(meta); err != nil {
		panic(fmt.Sprintf("register predicate %q: %v", name, err))
	}

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
	parts, err := ParsePredicate(meta.Name)
	if err != nil {
		panic(fmt.Sprintf("register predicate %q: %v", meta.Name, err))
	}
	if meta.Domain != "" && meta.Domain != parts.Domain {
		panic(fmt.Sprintf("register predicate %q: metadata domain %q does not match predicate domain %q",
			meta.Name, meta.Domain, parts.Domain))
	}
	if meta.Category != "" && meta.Category != parts.Category {
		panic(fmt.Sprintf("register predicate %q: metadata category %q does not match predicate category %q",
			meta.Name, meta.Category, parts.Category))
	}
	meta.Domain = parts.Domain
	meta.Category = parts.Category

	registryMu.Lock()
	defer registryMu.Unlock()
	if err := validatePredicateMetadataLocked(meta); err != nil {
		panic(fmt.Sprintf("register predicate %q: %v", meta.Name, err))
	}

	predicateRegistry[meta.Name] = meta
}

// validatePredicateMetadataLocked validates relationships between metadata
// fields. The caller holds registryMu so an already-declared inverse can be
// checked without racing another registration.
func validatePredicateMetadataLocked(meta PredicateMetadata) error {
	if meta.InverseOf != "" {
		if _, err := ParsePredicate(meta.InverseOf); err != nil {
			return fmt.Errorf("inverse predicate %q: %w", meta.InverseOf, err)
		}
	}
	if meta.IsSymmetric && meta.InverseOf != "" {
		return fmt.Errorf("symmetric predicate cannot also declare inverse %q", meta.InverseOf)
	}
	if meta.IsAlias {
		if !validAliasType(meta.AliasType) {
			return fmt.Errorf("invalid alias type %q", meta.AliasType)
		}
	} else if meta.AliasType != "" || meta.AliasPriority != 0 {
		return fmt.Errorf("alias metadata requires IsAlias=true")
	}

	if inverse, ok := predicateRegistry[meta.InverseOf]; meta.InverseOf != "" && ok &&
		inverse.InverseOf != "" && inverse.InverseOf != meta.Name {
		return fmt.Errorf("inverse predicate %q points to %q, not %q",
			meta.InverseOf, inverse.InverseOf, meta.Name)
	}
	return nil
}

func validAliasType(aliasType AliasType) bool {
	switch aliasType {
	case AliasTypeIdentity, AliasTypeLabel, AliasTypeAlternate, AliasTypeExternal, AliasTypeCommunication:
		return true
	default:
		return false
	}
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

// DiscoverLabelPredicates discovers all predicates marked as human-readable
// labels (AliasTypeLabel) in the registry. Returns predicate name → priority
// (lower number = higher salience).
//
// These are the display-name predicates the alias index deliberately EXCLUDES
// (labels are ambiguous for ID resolution — CanResolveToEntityID is false for
// AliasTypeLabel), and are exactly what the name index keys on for
// name→ranked-IDs lookup. Applications register their own label predicates via
// Register(pred, WithAlias(AliasTypeLabel, priority)), the same mechanism as
// aliases. Empty when none are registered.
func DiscoverLabelPredicates() map[string]int {
	registryMu.RLock()
	defer registryMu.RUnlock()

	labelPredicates := make(map[string]int)

	for name, meta := range predicateRegistry {
		if meta.IsAlias && meta.AliasType == AliasTypeLabel {
			labelPredicates[name] = meta.AliasPriority
		}
	}

	return labelPredicates
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
