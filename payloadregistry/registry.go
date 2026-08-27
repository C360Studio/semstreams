// Package payloadregistry is the single type authority for a binary
// (ADR-103): a message.Type is a type of the deployment if and only if it
// is registered here. A registration carries the factory that decodes the
// type, the ADR-054 indexing-profile floor graph-ingest stamps on entities
// born with it, and the projection contracts bound to it. There is no
// second catalogue of types and no global registry — each binary constructs
// its own and injects it through component.Dependencies.PayloadRegistry.
//
// Import edge: payloadregistry → pkg/projection/contract → {pkg/types,
// vocabulary → pkg/platform}. The genuinely new transitive dependency this
// adds is vocabulary itself — five init()s (hierarchy.go, labels.go,
// lifecycle.go, relationships.go, rulepacks/predicates.go) and a global
// predicate registry; pkg/platform was already reached through message.
// `message` imports this package and therefore inherits the edge. The
// package still reaches neither message nor any component package, so
// message and agentic can both import it without a cycle.
//
// Pattern A (boot-registry) per ADR-029: consumers use the *Registry
// instance form and dependency-inject it like other registries.
package payloadregistry

import (
	"encoding/json"
	"fmt"
	"sort"
	"sync"

	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/pkg/projection/contract"
	"github.com/c360studio/semstreams/pkg/types"
	"github.com/c360studio/semstreams/vocabulary"
)

// Factory creates a payload instance for a specific message type.
// The factory returns an any to avoid import cycles.
// The actual payload should implement the message.Payload interface.
type Factory func() any

// Builder creates a typed payload from field mappings.
// Used by workflow variable interpolation to construct typed payloads
// from step output maps. Returns error if required fields are missing
// or field values cannot be converted to the target type.
// Returns any to avoid import cycles - the actual payload should implement message.Payload.
//
// OPTIONAL: If not provided, BuildPayload falls back to JSON marshal/unmarshal
// using the Factory to create the target type. Custom builders are only needed
// for performance optimization of high-frequency payload types.
type Builder func(fields map[string]any) (any, error)

// Registration holds factory and metadata for a payload type, plus the
// attributes registered with the type (ADR-103): its indexing-profile floor
// and the projection contracts bound to it.
type Registration struct {
	Factory     Factory        `json:"-"`           // Factory function (not serializable)
	Builder     Builder        `json:"-"`           // Builder function (not serializable)
	Domain      string         `json:"domain"`      // Message domain (e.g., "robotics", "sensors")
	Category    string         `json:"category"`    // Message category (e.g., "heartbeat", "gps")
	Version     string         `json:"version"`     // Schema version (e.g., "v1", "v2")
	Description string         `json:"description"` // Human-readable description
	Example     map[string]any `json:"example"`     // Optional example payload data

	// IndexingProfile is the ADR-054 channel-(c) floor graph-ingest stamps on
	// an entity born with this type when the producer declares none. Empty
	// means the type declares no floor: ingest applies control and meters
	// the gap under indexing_profile_default_total{message_type}.
	IndexingProfile string `json:"indexing_profile,omitempty"`

	// Contracts are the projection contracts bound to this type. Each names
	// this registration's key (an empty MessageType is filled at Register);
	// a contract naming another key is a registration error.
	Contracts []contract.Contract `json:"-"`
}

// MessageType returns the formatted message type string for this registration.
// Format: "domain.category.version" (e.g., "robotics.heartbeat.v1")
func (pr *Registration) MessageType() string {
	return fmt.Sprintf("%s.%s.%s", pr.Domain, pr.Category, pr.Version)
}

// Registry manages payload factories for message deserialization.
// It provides thread-safe registration and lookup of payload factories,
// enabling BaseMessage.UnmarshalJSON to recreate typed payloads from JSON.
type Registry struct {
	registrations map[string]*Registration // Registry by message type string
	mu            sync.RWMutex             // Protects the map
}

// New creates a new empty payload registry.
func New() *Registry {
	return &Registry{
		registrations: make(map[string]*Registration),
	}
}

// Register registers a payload factory with validation.
// The message type is derived from the registration's Domain, Category, and Version fields.
// Returns an error if validation fails or the type is already registered.
func (pr *Registry) Register(registration *Registration) error {
	if registration == nil {
		return errs.WrapInvalid(
			errs.ErrInvalidConfig,
			"PayloadRegistry",
			"Register",
			"registration validation",
		)
	}

	if registration.Factory == nil {
		return errs.WrapInvalid(
			errs.ErrInvalidConfig,
			"PayloadRegistry",
			"Register",
			"factory function validation",
		)
	}

	// Builder is optional - see Build for fallback behavior

	if registration.Domain == "" {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "PayloadRegistry", "Register", "domain validation")
	}

	if registration.Category == "" {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "PayloadRegistry", "Register", "category validation")
	}

	if registration.Version == "" {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "PayloadRegistry", "Register", "version validation")
	}

	// Component grammar: the key must round-trip through Key(); a component
	// holding the separator would register a key nothing can bind a contract
	// to, and the error belongs at boot, not at the first Create.
	registeredType := types.Type{Domain: registration.Domain, Category: registration.Category, Version: registration.Version}
	if err := registeredType.Validate(); err != nil {
		return errs.WrapInvalid(err, "PayloadRegistry", "Register", "message type grammar")
	}

	// Verify factory produces payload with matching Schema()
	if err := validateSchemaConsistency(registration); err != nil {
		return err
	}

	if registration.IndexingProfile != "" && !vocabulary.IsValidIndexingProfile(registration.IndexingProfile) {
		return errs.WrapInvalid(
			fmt.Errorf("indexing profile %q is not one of the vocabulary's profiles", registration.IndexingProfile),
			"PayloadRegistry",
			"Register",
			"indexing profile validation",
		)
	}

	msgType := registration.MessageType()

	contracts, err := bindContracts(registeredType, registration.IndexingProfile, registration.Contracts)
	if err != nil {
		return err
	}

	pr.mu.Lock()
	defer pr.mu.Unlock()

	if _, exists := pr.registrations[msgType]; exists {
		return errs.WrapInvalid(
			fmt.Errorf("payload type '%s' is already registered", msgType),
			"PayloadRegistry",
			"Register",
			"duplicate payload check",
		)
	}

	stored := *registration
	stored.Contracts = contracts
	pr.registrations[msgType] = &stored
	return nil
}

// bindContracts returns independent copies of the contracts bound to the
// registered type: a zero contract MessageType is filled with the structured
// type (never a parsed key), a different type is refused, contract names are
// unique within the registration, a contract profile must agree with the
// type's floor when both are set (O-13), and each contract passes shape
// validation. Predicate declaration is not checked here; it stays at
// mutation-client construction.
func bindContracts(registered types.Type, floor string, contracts []contract.Contract) ([]contract.Contract, error) {
	if len(contracts) == 0 {
		return nil, nil
	}
	bound := make([]contract.Contract, 0, len(contracts))
	names := make(map[string]struct{}, len(contracts))
	for _, original := range contracts {
		c := cloneContract(original)
		if c.MessageType == (types.Type{}) {
			c.MessageType = registered
		}
		if !c.MessageType.Equal(registered) {
			return nil, errs.WrapInvalid(
				fmt.Errorf("contract %q names message type %q but is registered with %q", c.Name, c.MessageType.Key(), registered.Key()),
				"PayloadRegistry", "Register", "contract message type check")
		}
		if _, duplicate := names[c.Name]; duplicate {
			return nil, errs.WrapInvalid(
				fmt.Errorf("contract name %q repeats within registration %q", c.Name, registered.Key()),
				"PayloadRegistry", "Register", "contract name check")
		}
		names[c.Name] = struct{}{}
		if floor != "" && c.IndexingProfile != "" && c.IndexingProfile != floor {
			return nil, errs.WrapInvalid(
				fmt.Errorf("contract %q declares indexing profile %q but %q registers floor %q",
					c.Name, c.IndexingProfile, registered.Key(), floor),
				"PayloadRegistry", "Register", "contract profile agreement")
		}
		if err := c.ValidateShape(); err != nil {
			return nil, errs.WrapInvalid(
				fmt.Errorf("contract bound to %q: %w", registered.Key(), err),
				"PayloadRegistry", "Register", "contract shape validation")
		}
		bound = append(bound, c)
	}
	return bound, nil
}

// cloneContract returns a contract whose slices are independent of the input.
func cloneContract(c contract.Contract) contract.Contract {
	clone := c
	clone.BirthPredicates = append([]string(nil), c.BirthPredicates...)
	clone.Groups = make([]contract.PredicateGroup, len(c.Groups))
	for index, group := range c.Groups {
		clone.Groups[index] = group
		clone.Groups[index].Predicates = append([]string(nil), group.Predicates...)
	}
	return clone
}

func cloneContracts(contracts []contract.Contract) []contract.Contract {
	if len(contracts) == 0 {
		return nil
	}
	cloned := make([]contract.Contract, len(contracts))
	for index, c := range contracts {
		cloned[index] = cloneContract(c)
	}
	return cloned
}

// copyWithoutFactory returns the caller-facing view of a registration: every
// attribute with independent contract copies; Factory and Builder are
// intentionally not copied.
func (pr *Registration) copyWithoutFactory() *Registration {
	return &Registration{
		Domain:          pr.Domain,
		Category:        pr.Category,
		Version:         pr.Version,
		Description:     pr.Description,
		Example:         pr.Example,
		IndexingProfile: pr.IndexingProfile,
		Contracts:       cloneContracts(pr.Contracts),
	}
}

// IndexingProfileFor returns the floor registered with key and whether the key
// is registered in this registry. A registered type may declare no floor
// (profile == ""); an unregistered key reports ("", false). Floors exist per
// binary because registrations do.
func (pr *Registry) IndexingProfileFor(key string) (profile string, registered bool) {
	pr.mu.RLock()
	defer pr.mu.RUnlock()

	registration, exists := pr.registrations[key]
	if !exists {
		return "", false
	}
	return registration.IndexingProfile, true
}

// Contracts returns fresh copies of every projection contract registered with
// a type, ordered by message-type key and then by contract name. The
// composition root derives its mutation-client contract set from it; no other
// table of framework contracts exists.
func (pr *Registry) Contracts() []contract.Contract {
	pr.mu.RLock()
	defer pr.mu.RUnlock()

	keys := make([]string, 0, len(pr.registrations))
	for key, registration := range pr.registrations {
		if len(registration.Contracts) != 0 {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)

	var result []contract.Contract
	for _, key := range keys {
		contracts := cloneContracts(pr.registrations[key].Contracts)
		sort.Slice(contracts, func(i, j int) bool { return contracts[i].Name < contracts[j].Name })
		result = append(result, contracts...)
	}
	return result
}

// Create creates a payload instance using the registered factory.
// Returns nil if the message type is not registered; BaseMessage.UnmarshalJSON
// then rejects the payload as unregistered — the fact lane admits only
// registered types.
func (pr *Registry) Create(domain, category, version string) any {
	typeStr := fmt.Sprintf("%s.%s.%s", domain, category, version)

	pr.mu.RLock()
	registration, exists := pr.registrations[typeStr]
	pr.mu.RUnlock()

	if !exists {
		return nil
	}

	return registration.Factory()
}

// Build creates a typed payload from field mappings.
// If a custom Builder is registered, it is used for efficient field mapping.
// Otherwise, falls back to JSON marshal/unmarshal using the Factory.
//
// Returns an error if the message type is not registered or if building fails.
func (pr *Registry) Build(domain, category, version string, fields map[string]any) (any, error) {
	typeStr := fmt.Sprintf("%s.%s.%s", domain, category, version)

	pr.mu.RLock()
	registration, exists := pr.registrations[typeStr]
	pr.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("payload type %q not registered", typeStr)
	}

	// Use custom builder if available (optimization path)
	if registration.Builder != nil {
		return registration.Builder(fields)
	}

	// Fallback: JSON round-trip using Factory
	data, err := json.Marshal(fields)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal fields for %s: %w", typeStr, err)
	}

	payload := registration.Factory()
	if err := json.Unmarshal(data, payload); err != nil {
		return nil, fmt.Errorf("failed to unmarshal into %s: %w", typeStr, err)
	}

	return payload, nil
}

// GetRegistration returns the payload registration for a specific message type.
// Returns the registration and true if found, nil and false otherwise.
func (pr *Registry) GetRegistration(msgType string) (*Registration, bool) {
	pr.mu.RLock()
	defer pr.mu.RUnlock()

	registration, exists := pr.registrations[msgType]
	if !exists {
		return nil, false
	}

	// Return a copy to prevent external modification of the factory function
	return registration.copyWithoutFactory(), true
}

// List returns all registered payload types.
// Returns a copy of the registrations map to prevent external modification.
func (pr *Registry) List() map[string]*Registration {
	pr.mu.RLock()
	defer pr.mu.RUnlock()

	// Return a copy to prevent external modification
	result := make(map[string]*Registration, len(pr.registrations))
	for msgType, registration := range pr.registrations {
		result[msgType] = registration.copyWithoutFactory()
	}

	return result
}

// ListByDomain returns all payload registrations for a specific domain.
// This is useful for discovering what message types are available within
// a particular domain (e.g., "robotics", "sensors").
func (pr *Registry) ListByDomain(domain string) []*Registration {
	pr.mu.RLock()
	defer pr.mu.RUnlock()

	var result []*Registration
	for _, registration := range pr.registrations {
		if registration.Domain == domain {
			result = append(result, registration.copyWithoutFactory())
		}
	}

	return result
}

// schemaProvider is an interface for payloads that provide schema information.
// This matches the message.Payload interface's Schema() method signature.
type schemaProvider interface {
	Schema() types.Type
}

// validateSchemaConsistency checks that a factory-produced payload's Schema()
// method returns values matching the registration. This catches mismatches
// between Schema() implementation and Registration at init() time,
// preventing runtime deserialization failures.
func validateSchemaConsistency(reg *Registration) error {
	testPayload := reg.Factory()
	if testPayload == nil {
		return errs.WrapInvalid(
			errs.ErrInvalidConfig,
			"PayloadRegistry",
			"Register",
			"factory returned nil payload",
		)
	}

	// Check if payload implements Schema() method
	sp, ok := testPayload.(schemaProvider)
	if !ok {
		// Payload doesn't implement Schema() - skip validation
		// This allows non-message.Payload types to be registered
		return nil
	}

	// Verify Schema() returns matching values
	schema := sp.Schema()
	if schema.Domain != reg.Domain || schema.Category != reg.Category || schema.Version != reg.Version {
		return errs.WrapInvalid(
			fmt.Errorf(
				"Schema() returns {Domain:%q Category:%q Version:%q} but registration expects {Domain:%q Category:%q Version:%q}",
				schema.Domain, schema.Category, schema.Version,
				reg.Domain, reg.Category, reg.Version,
			),
			"PayloadRegistry",
			"Register",
			"schema consistency check",
		)
	}

	return nil
}
