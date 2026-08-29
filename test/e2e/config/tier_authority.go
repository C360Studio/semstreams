package config

import "fmt"

// Tier variant names as the scenarios and the compose profiles spell them.
const (
	VariantStructural  = "structural"
	VariantStatistical = "statistical"
	VariantSemantic    = "semantic"
)

// tierAuthority maps a tier variant to positions 1-2 of every entity the
// deployment that variant boots mints: the platform.org / platform.id of the
// shipped config its compose profile passes to the binary (ADR-102 d2 — the
// composition root's own identity field is the authority, and nothing else).
//
// The three tiers deploy three DIFFERENT authorities, so a fixture cannot
// hardcode one value and be right in more than one tier. This table is the one
// place the values live; TestTierAuthorityMatchesShippedConfigs re-derives it
// from docker/compose/tiered.yml and the config each profile names, so it
// cannot drift from what the tier actually boots without a unit test going red
// — a second of feedback rather than ninety.
var tierAuthority = map[string]string{
	VariantStructural:  "c360.semstreams-e2e-structural",
	VariantStatistical: "c360.semstreams-statistical",
	VariantSemantic:    "c360.semstreams-kitchen-sink-ml",
}

// CoreAuthority is positions 1-2 of every entity the CORE stack mints — the
// platform.org / platform.id of configs/protocol-flow.json, which
// docker/compose/e2e.yml boots. It sits beside the tier table rather than in it
// because core is a different compose document with no profiles, but it obeys
// the same rule: one home, re-derived by TestCoreAuthorityMatchesShippedConfig
// so it cannot drift from the config the stack actually starts.
//
// The graph round-trip canary is minted under it (#1095 slice B); since
// ADR-102 d5 the boundary refuses every other pair.
const CoreAuthority = "c360.streamkit-pure"

// TierAuthority returns the deployment authority prefix (org.platform) for a
// tier variant. An unknown variant panics rather than returning a plausible
// default: an entity ID built under the wrong authority produces a
// "not found" three stages later, which is the failure this whole change
// exists to stop being silent.
func TierAuthority(variant string) string {
	authority, ok := tierAuthority[variant]
	if !ok {
		panic(fmt.Sprintf("e2e config: no deployment authority registered for tier variant %q", variant))
	}
	return authority
}

// TierEntityID composes a full entity ID for a tier variant from the last four
// canonical positions — system.domain.type.instance — under that tier's own
// deployment authority.
//
//	TierEntityID("structural", "sensor.environmental.temperature.temp-sensor-001")
//	  -> "c360.semstreams-e2e-structural.sensor.environmental.temperature.temp-sensor-001"
func TierEntityID(variant, suffix string) string {
	return TierAuthority(variant) + "." + suffix
}
