package weatherstation

import semtypes "github.com/c360studio/semstreams/pkg/types"

// Producer is the trusted producer name this example registers its entity
// domain under. A product's producer identity is supplied by its composition
// root, never inferred from a payload.
const Producer = "weather-station-example"

// EntityDomainDelegations declares the entity domain this example mints under
// (position 4 of the canonical org.platform.system.domain.type.instance order).
// The entity-ID corpus audit AST-scans these literals for the registered set
// its domain_unregistered rule consults, so a position-4 token this example
// mints without declaring here is a finding. Nothing reads them at runtime and
// there is no constructor to call.
//
// Sharing a domain with another product is PERMITTED (owner ruling
// 2026-08-28): `system` at position 3 keeps the entity IDs distinct, and
// ADR-099 level 0 is source x taxonomy, so the communities stay distinct too.
// Picking a token another product already means something else by is a
// vocabulary question, not one the framework decides for you.
func EntityDomainDelegations() []semtypes.EntityDomainDelegation {
	return []semtypes.EntityDomainDelegation{
		{Producer: Producer, Domain: "meteorology"},
	}
}
