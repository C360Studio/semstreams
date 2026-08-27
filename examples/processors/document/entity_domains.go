package document

import semtypes "github.com/c360studio/semstreams/pkg/types"

// Producer is the trusted producer name this example registers its entity
// domains under. A product's producer identity is supplied by its composition
// root, never inferred from a payload.
const Producer = "document-example"

// EntityDomainDelegations declares the entity domains this example mints under
// (position 4 of the canonical org.platform.system.domain.type.instance order).
// A composition root passes them to semtypes.NewEntityDomainAuthority so a
// collision with another product's domain is a boot-time rejection, and the
// entity-ID corpus audit reads them as the registered set for its
// domain_unregistered rule.
func EntityDomainDelegations() []semtypes.EntityDomainDelegation {
	return []semtypes.EntityDomainDelegation{
		{Producer: Producer, Domain: "content"},
		{Producer: Producer, Domain: "sensor"},
		{Producer: Producer, Domain: "maintenance"},
		{Producer: Producer, Domain: "observation"},
	}
}
