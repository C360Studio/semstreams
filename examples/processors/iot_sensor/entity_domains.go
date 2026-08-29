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
// The entity-ID corpus audit AST-scans these literals for the registered set
// its domain_unregistered rule consults. Nothing reads them at runtime and
// there is no constructor to call.
//
// What that rule does NOT cover today, measured rather than assumed: this
// example mints through semtypes.EntityID{...}.Key(), and
// internal/entityidaudit.entityIDConstructorValue only emits a candidate when
// ALL SIX fields resolve to string constants. `Org: authority.Org` and the
// caller-supplied `Type` never resolve, so no candidate is emitted and
// domain_unregistered has nothing to judge — replacing the Domain below with
// an undeclared token leaves `go run ./cmd/entity-id-audit .` GREEN. The
// fmt.Sprintf form this example used before ADR-102 d2 WAS judged, so the
// coverage was lost in that conversion, not merely absent. Restoring it means
// teaching the audit to emit a partial candidate when positions 3-6 resolve;
// that is filed as framework follow-up, not fixed here.
//
// What guards the domain today is behavioural: the package's own EntityID
// tests assert the minted prefix, so an undeclared or mistyped position-4
// token fails `go test ./examples/processors/...` even though the corpus
// audit passes. Declaring here is still required — the audit consults this
// set for every candidate it CAN extract, including any this example adds in
// a form it can read.
//
// Sharing a domain with another product is PERMITTED (owner ruling
// 2026-08-28): `system` at position 3 keeps the entity IDs distinct, and
// ADR-099 level 0 is source x taxonomy, so the communities stay distinct too.
// Picking a token another product already means something else by is a
// vocabulary question, not one the framework decides for you.
func EntityDomainDelegations() []semtypes.EntityDomainDelegation {
	return []semtypes.EntityDomainDelegation{
		{Producer: Producer, Domain: environmentDomain},
		{Producer: Producer, Domain: facilityDomain},
	}
}
