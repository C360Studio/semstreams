package iotsensor

import semtypes "github.com/c360studio/semstreams/pkg/types"

// Producer is the trusted producer name this example registers its entity
// domains under. A product's producer identity is supplied by its composition
// root, never inferred from a payload.
const Producer = "iot-sensor-example"

// Entity-ID positions this example owns. Under the canonical order
// org.platform.system.domain.type.instance (ADR-102) position 3 is the SOURCE
// and position 4 the delegated taxonomy: a reading is
// `org.platform.sensor.environmental.<type>.<device>` and a zone is
// `org.platform.zone.facility.<zoneType>.<zoneID>`.
const (
	sensorSystem      = "sensor"
	environmentDomain = "environmental"
	zoneSystem        = "zone"
	facilityDomain    = "facility"
)

// EntityDomainDelegations declares the entity domains this example mints under.
// A composition root passes them to semtypes.NewEntityDomainAuthority so a
// collision with another product's domain is a boot-time rejection, and the
// entity-ID corpus audit reads them as the registered set for its
// domain_unregistered rule.
func EntityDomainDelegations() []semtypes.EntityDomainDelegation {
	return []semtypes.EntityDomainDelegation{
		{Producer: Producer, Domain: environmentDomain},
		{Producer: Producer, Domain: facilityDomain},
	}
}
