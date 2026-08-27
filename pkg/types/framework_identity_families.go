package types

// FrameworkIdentityFamily is one framework-minted entity family whose
// positions 3-6 have a fixed byte length by construction (a digest or a
// fixed-width hash in the instance position). The table is the single home for
// those families' prefixes — builders compose their identity from it so the
// authority-pair budget at configuration load is derived, never hand-copied
// (ADR-102 amends ADR-076 d2; ruled O-14).
type FrameworkIdentityFamily struct {
	// Name is the stable family name used in configuration-load errors.
	Name string
	// System is position 3: the minting framework component.
	System string
	// Domain is position 4: a framework-reserved domain.
	Domain string
	// Type is position 5.
	Type string
	// InstanceBytes is the fixed byte length of position 6.
	InstanceBytes int
}

// frameworkIdentityFamilies holds every fixed-suffix family. Unbounded-instance
// families (loop and chain executions, lessons, endpoints, diagnoses) are not
// fixed-suffix and are bounded only by the whole-ID validator.
var frameworkIdentityFamilies = [...]FrameworkIdentityFamily{
	{Name: "rule-alert", System: "rules", Domain: "graph", Type: "alert", InstanceBytes: 64},
	{Name: "rule-trigger", System: "rules", Domain: "graph", Type: "trigger", InstanceBytes: 64},
	{Name: "web-observation", System: "web", Domain: "agent", Type: "observation", InstanceBytes: 16},
}

// FrameworkIdentityFamilies returns a copy of the fixed-suffix family table.
func FrameworkIdentityFamilies() []FrameworkIdentityFamily {
	return append([]FrameworkIdentityFamily(nil), frameworkIdentityFamilies[:]...)
}

// RuleAlertIdentityFamily is the ADR-076 rule alert family under the
// deployment's own authority.
func RuleAlertIdentityFamily() FrameworkIdentityFamily { return frameworkIdentityFamilies[0] }

// RuleTriggerIdentityFamily is the ADR-076 rule trigger family under the
// deployment's own authority; the longest fixed suffix today.
func RuleTriggerIdentityFamily() FrameworkIdentityFamily { return frameworkIdentityFamilies[1] }

// WebObservationIdentityFamily is the agent web-observation family (sha256-16
// of the canonical URL in the instance position).
func WebObservationIdentityFamily() FrameworkIdentityFamily { return frameworkIdentityFamilies[2] }

// LongestFrameworkIdentityFamily returns the family with the most fixed bytes:
// the one that binds the authority-pair budget.
func LongestFrameworkIdentityFamily() FrameworkIdentityFamily {
	longest := frameworkIdentityFamilies[0]
	for _, family := range frameworkIdentityFamilies[1:] {
		if family.FixedBytes() > longest.FixedBytes() {
			longest = family
		}
	}
	return longest
}

// MaxAuthorityPairBytes is the configuration-load budget for
// len(platform.org)+len(platform.id): the canonical bound minus the longest
// fixed family suffix, so every framework identity fits under every admitted
// authority. 170 bytes while the rule trigger family binds.
func MaxAuthorityPairBytes() int {
	return MaxEntityIDBytes - LongestFrameworkIdentityFamily().FixedBytes()
}

// FixedBytes is every byte of a member identity that is not the authority
// pair: the five separators plus positions 3-6.
func (f FrameworkIdentityFamily) FixedBytes() int {
	return 5 + len(f.System) + len(f.Domain) + len(f.Type) + f.InstanceBytes
}

// EntityID composes and validates a member identity under the given
// authority. It fails closed on any non-canonical part, including an empty or
// dotted authority segment and an instance of the wrong length.
func (f FrameworkIdentityFamily) EntityID(org, platform, instance string) (string, error) {
	if len(instance) != f.InstanceBytes {
		return "", newEntityIDContractError(ErrorCodeEntityIDInvalid, EntityIDReasonBytes, map[string]any{
			EntityIDDetailMeasuredBytes: len(instance),
			EntityIDDetailAllowedBytes:  f.InstanceBytes,
		})
	}
	id := EntityID{Org: org, Platform: platform, System: f.System, Domain: f.Domain, Type: f.Type, Instance: instance}
	if err := validateEntityIDSegment(org); err != nil {
		return "", err
	}
	if err := validateEntityIDSegment(platform); err != nil {
		return "", err
	}
	if err := ValidateEntityID(id.Key()); err != nil {
		return "", err
	}
	return id.Key(), nil
}
