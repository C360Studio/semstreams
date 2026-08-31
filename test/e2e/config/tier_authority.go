package config

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Tier variant names as the scenarios and the compose profiles spell them.
const (
	VariantStructural  = "structural"
	VariantStatistical = "statistical"
	VariantSemantic    = "semantic"
)

// tierAuthorityStem maps a tier variant to the authority its shipped
// configuration DECLARES — the `platform.org` / `platform.id` of the config its
// compose profile passes to the binary (ADR-102 d2).
//
// It is the stem, not the authority. Since ADR-104 the framework mints an
// entropy suffix onto `platform.id` on a deployment's first boot, so the pair a
// tier actually mints under is `<org>.<id>-<6 hex>` and is knowable only by
// READING it from the running stack. Use EffectiveTierAuthority for anything
// that composes an entity ID the graph must accept; this table exists to say
// which configuration a tier boots, and TestTierAuthorityMatchesShippedConfigs
// re-derives it from docker/compose/tiered.yml so it cannot drift from that.
var tierAuthorityStem = map[string]string{
	VariantStructural:  "c360.semstreams-e2e-structural",
	VariantStatistical: "c360.semstreams-statistical",
	VariantSemantic:    "c360.semstreams-kitchen-sink-ml",
}

// CoreAuthorityStem is the authority configs/protocol-flow.json DECLARES — the
// config docker/compose/e2e.yml boots. It sits beside the tier table rather
// than in it because core is a different compose document with no profiles, but
// it obeys the same rule: one home, re-derived by
// TestCoreAuthorityMatchesShippedConfig so it cannot drift from the config the
// stack actually starts, and a STEM rather than the effective pair.
const CoreAuthorityStem = "c360.streamkit-pure"

// TierAuthorityStem returns the authority prefix (org.platform) a tier variant's
// configuration declares, before the ADR-104 suffix is minted onto it. An
// unknown variant panics rather than returning a plausible default: an entity ID
// built under the wrong authority produces a "not found" three stages later,
// which is the failure this whole indirection exists to stop being silent.
func TierAuthorityStem(variant string) string {
	authority, ok := tierAuthorityStem[variant]
	if !ok {
		panic(fmt.Sprintf("e2e config: no deployment authority registered for tier variant %q", variant))
	}
	return authority
}

// TierStemEntityID composes a full entity ID from a tier's DECLARED authority
// and the last four canonical positions. It is for assertions about ID SHAPE
// only. An entity the running stack minted carries the effective pair, so an ID
// built here will not be found in the graph — compose those from
// EffectiveTierAuthority.
func TierStemEntityID(variant, suffix string) string {
	return TierAuthorityStem(variant) + "." + suffix
}

const (
	// PlatformIdentityBucket is the shared configuration bucket every sem* app
	// on one NATS server uses.
	PlatformIdentityBucket = "semstreams_config"
	// PlatformIdentityKey holds the deployment's durable platform identity.
	// Reading it is the ADR-104 cross-repo contract: an adopter observes the
	// pair a deployment mints under instead of predicting it from a config file.
	// Exported so the scenarios that assert on the record name it from here
	// rather than keeping a second copy of a cross-repo contract's address.
	PlatformIdentityKey = "platform_identity"
)

// AuthorityReader reads one KV value from the running stack.
// *client.NATSValidationClient satisfies it; the interface keeps this package
// free of a dependency on the e2e client.
type AuthorityReader interface {
	GetKV(ctx context.Context, bucket, key string) ([]byte, error)
}

// platformIdentityRecord is the record's shape, normative in the
// component-runtime-config capability spec.
type platformIdentityRecord struct {
	Org  string `json:"org"`
	Stem string `json:"stem"`
	ID   string `json:"id"`
}

// EffectiveAuthority reads the authority the running deployment ACTUALLY mints
// under and cross-checks it against the configuration a scenario believes it
// booted.
//
// Predicting the pair from a config file has been wrong since ADR-104 — the
// framework mints an entropy suffix onto `platform.id` at first boot — and it
// was always the kind of wrong that surfaces stages later as a missing entity
// or a refused write. Observing it cannot be wrong about a value it never
// guessed; the declaredStem cross-check turns "this stack booted a different
// configuration than I think" into its own named failure here.
func EffectiveAuthority(ctx context.Context, reader AuthorityReader, declaredStem string) (string, error) {
	if reader == nil {
		return "", fmt.Errorf("e2e config: reading the deployment authority needs a NATS reader; nothing may predict it from a configuration file (ADR-104)")
	}
	org, stem, ok := strings.Cut(declaredStem, ".")
	if !ok || org == "" || stem == "" {
		return "", fmt.Errorf("e2e config: declared authority %q is not org.platform", declaredStem)
	}

	raw, err := reader.GetKV(ctx, PlatformIdentityBucket, PlatformIdentityKey)
	if err != nil {
		return "", fmt.Errorf(
			"e2e config: read %s/%s, where the deployment records the authority it mints under (ADR-104): %w",
			PlatformIdentityBucket, PlatformIdentityKey, err,
		)
	}
	var record platformIdentityRecord
	if err := json.Unmarshal(raw, &record); err != nil {
		return "", fmt.Errorf("e2e config: parse %s/%s: %w", PlatformIdentityBucket, PlatformIdentityKey, err)
	}
	if record.Org == "" || record.ID == "" {
		return "", fmt.Errorf(
			"e2e config: %s/%s is incomplete (org=%q stem=%q id=%q)",
			PlatformIdentityBucket, PlatformIdentityKey, record.Org, record.Stem, record.ID,
		)
	}
	if record.Org != org || record.Stem != stem {
		return "", fmt.Errorf(
			"e2e config: the running deployment records org=%q stem=%q, but this scenario booted for %q — the stack under test is not the configuration this scenario names",
			record.Org, record.Stem, declaredStem,
		)
	}
	return record.Org + "." + record.ID, nil
}

// EffectiveTierAuthority is EffectiveAuthority for a tier variant.
func EffectiveTierAuthority(ctx context.Context, reader AuthorityReader, variant string) (string, error) {
	return EffectiveAuthority(ctx, reader, TierAuthorityStem(variant))
}
