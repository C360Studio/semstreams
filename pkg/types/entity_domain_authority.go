package types

import (
	"fmt"
	"strings"
)

// Position indexes (zero-based) used in coded details.
const (
	entityIDDomainSegmentIndex = 3
)

// frameworkEntityDomains is the framework-reserved domain set (ADR-102 d4,
// ruled O-9): every framework identity family lives under one of these and
// they pass authorization for every producer. Products delegate their own
// domains; they never re-delegate a reserved one.
var frameworkEntityDomains = [...]string{"agent", "ops", "graph"}

// reservedInstanceTokens are the hierarchy-container padding tokens
// (graph/inference) reserved in the instance position until gh606 retires
// containers. A producer instance equal to one of them is a corpus finding.
var reservedInstanceTokens = [...]string{"group", "container", "level"}

// FrameworkEntityDomains returns a copy of the framework-reserved domain set.
func FrameworkEntityDomains() []string {
	return append([]string(nil), frameworkEntityDomains[:]...)
}

// IsFrameworkEntityDomain reports whether domain is framework-reserved.
func IsFrameworkEntityDomain(domain string) bool {
	for _, reserved := range frameworkEntityDomains {
		if domain == reserved {
			return true
		}
	}
	return false
}

// ReservedInstanceTokens returns a copy of the reserved instance tokens.
func ReservedInstanceTokens() []string {
	return append([]string(nil), reservedInstanceTokens[:]...)
}

// IsReservedInstanceToken reports whether token is a reserved instance token.
func IsReservedInstanceToken(token string) bool {
	for _, reserved := range reservedInstanceTokens {
		if token == reserved {
			return true
		}
	}
	return false
}

// EntityDomainDelegation grants one named producer authority over an exact
// entity domain (Type empty) or one exact domain.type. Producer identity is
// supplied by the trusted composition boundary; it is never inferred from
// Triple.Source or a payload type. Wildcards are not supported.
type EntityDomainDelegation struct {
	Producer string
	Domain   string
	Type     string
}

// EntityDomainAuthority is an immutable authorization policy over entity
// domains built from explicit delegations, mirroring vocabulary.PredicateAuthority
// for the entity-ID taxonomy positions. A nil authority admits only the
// framework-reserved domains.
type EntityDomainAuthority struct {
	delegations map[string][]EntityDomainDelegation
}

// NewEntityDomainAuthority validates and installs exact domain delegations.
// It is a composition rejection when a producer is empty, a segment is not a
// canonical entity-ID segment, or a framework-reserved domain is delegated.
//
// Two producers MAY delegate the same domain (owner ruling 2026-08-28,
// superseding O-5): the taxonomy vocabulary is shared, and overlap is
// legitimate and sometimes intended. Overlap collides nothing — `system` is
// position 3, so the IDs stay distinct, and ADR-099 level 0 is source x
// taxonomy, so the communities stay distinct. It is reported offline by
// composition.Validate as a non-blocking entity_domain_overlap finding, never
// as a boot refusal and never as a runtime log line.
func NewEntityDomainAuthority(delegations ...EntityDomainDelegation) (*EntityDomainAuthority, error) {
	authority := &EntityDomainAuthority{delegations: make(map[string][]EntityDomainDelegation)}
	for _, delegation := range delegations {
		producer := strings.TrimSpace(delegation.Producer)
		if producer == "" {
			return nil, fmt.Errorf("entity domain delegation for %q has an empty producer", delegation.Domain)
		}
		if err := validateEntityIDSegment(delegation.Domain); err != nil {
			return nil, fmt.Errorf("entity domain delegation for producer %q: domain %q: %w", producer, delegation.Domain, err)
		}
		if delegation.Type != "" {
			if err := validateEntityIDSegment(delegation.Type); err != nil {
				return nil, fmt.Errorf("entity domain delegation for producer %q: type %q: %w", producer, delegation.Type, err)
			}
		}
		if IsFrameworkEntityDomain(delegation.Domain) {
			return nil, fmt.Errorf("entity domain delegation for producer %q: domain %q is framework-reserved and cannot be delegated", producer, delegation.Domain)
		}
		authority.delegations[producer] = append(authority.delegations[producer], EntityDomainDelegation{
			Producer: producer, Domain: delegation.Domain, Type: delegation.Type,
		})
	}
	return authority, nil
}

// Authorize reports whether producer may mint entities in domain/entityType.
// A framework-reserved domain passes for every producer, including an empty
// one. Any other domain requires a non-empty producer holding an exact domain
// or domain.type delegation; the rejection is coded
// entity_id_authority_invalid with reason domain_undelegated.
func (a *EntityDomainAuthority) Authorize(producer, domain, entityType string) error {
	if IsFrameworkEntityDomain(domain) {
		return nil
	}
	producer = strings.TrimSpace(producer)
	if producer == "" || a == nil {
		return newEntityIDAuthorityError(EntityIDReasonDomainUndelegated, entityIDDomainSegmentIndex, "")
	}
	for _, delegation := range a.delegations[producer] {
		if delegation.Domain == domain && (delegation.Type == "" || delegation.Type == entityType) {
			return nil
		}
	}
	return newEntityIDAuthorityError(EntityIDReasonDomainUndelegated, entityIDDomainSegmentIndex, "")
}

// validateEntityIDSegment applies the canonical single-segment grammar: one
// non-empty ASCII token beginning alphanumeric with the remaining bytes
// alphanumeric, '_' or '-'. No dots, no wildcards, no rewriting.
func validateEntityIDSegment(value string) error {
	if value == "" {
		return newEntityIDContractError(ErrorCodeEntityIDInvalid, EntityIDReasonEmptySegment, map[string]any{EntityIDDetailSegmentIndex: 0})
	}
	if !isEntityIDAlphanumeric(value[0]) {
		return newEntityIDContractError(ErrorCodeEntityIDInvalid, EntityIDReasonFirstByte, map[string]any{EntityIDDetailSegmentIndex: 0})
	}
	for position := 1; position < len(value); position++ {
		if !isEntityIDRemainingByte(value[position]) {
			return newEntityIDContractError(ErrorCodeEntityIDInvalid, EntityIDReasonAlphabet, map[string]any{EntityIDDetailSegmentIndex: 0})
		}
	}
	return nil
}
