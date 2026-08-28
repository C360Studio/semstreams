package document

import semtypes "github.com/c360studio/semstreams/pkg/types"

// Producer is the trusted producer name this example registers its entity
// domains under. A product's producer identity is supplied by its composition
// root, never inferred from a payload.
const Producer = "document-example"

// Entity-ID positions this example owns. Under the canonical order
// org.platform.system.domain.type.instance (ADR-102) position 3 is the SOURCE
// and position 4 the delegated taxonomy. Positions 1-2 are never named here:
// they are the composition root's platform.org / platform.id and are supplied
// at mint time (ADR-102 d2).
const (
	documentSystem    = "document"
	contentDomain     = "content"
	sensorDomain      = "sensor"
	workSystem        = "work"
	maintenanceDomain = "maintenance"
	recordSystem      = "record"
	observationDomain = "observation"
)

// Type-position defaults applied when the incoming record leaves the field
// blank. Stated once, beside the domains, so the minting functions and the
// documented examples cannot drift apart.
const (
	defaultDocumentCategory    = "general"
	defaultMaintenanceStatus   = "pending"
	defaultObservationSeverity = "medium"
)

// EntityDomainDelegations declares the entity domains this example mints under
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
		{Producer: Producer, Domain: contentDomain},
		{Producer: Producer, Domain: sensorDomain},
		{Producer: Producer, Domain: maintenanceDomain},
		{Producer: Producer, Domain: observationDomain},
	}
}
