package types

const (
	// ErrorCodeEntityIDAuthorityInvalid classifies an authority rejection: the
	// candidate is structurally canonical but its positions 1-2 do not match
	// the lane it arrived on. Distinct from ErrorCodeEntityIDInvalid so a
	// caller can tell "malformed" from "not yours".
	ErrorCodeEntityIDAuthorityInvalid = "entity_id_authority_invalid"

	// EntityIDReasonForeignAuthority identifies a candidate whose org.platform
	// differs from the deployment's own on a lane that is not an import lane.
	EntityIDReasonForeignAuthority = "foreign_authority"
	// EntityIDReasonLocalAuthorityClaimed identifies a candidate claiming the
	// deployment's own org.platform on a declared import lane.
	EntityIDReasonLocalAuthorityClaimed = "local_authority_claimed"

	// EntityIDDetailLane reports which lane the candidate arrived on.
	EntityIDDetailLane = "lane"
	// EntityIDLaneLocal is the lane value for every non-import lane.
	EntityIDLaneLocal = "local"
	// EntityIDLaneImport is the lane value for a declared import lane.
	EntityIDLaneImport = "import"
)

// ValidateEntityIDAuthority checks positions 1-2 of candidate against the
// deployment's own org and platform for the lane the candidate arrived on.
// Structural validation runs first; an authority reason never masks a
// structural one. On a non-import lane a foreign pair is rejected with
// foreign_authority; on an import lane the deployment's own pair is rejected
// with local_authority_claimed and any foreign pair is accepted unchanged.
//
// It takes strings rather than types.PlatformMeta because that type lives in
// the root types package, which pkg/types must not import. Details carry only
// reason, segment_index, and lane — never identity bytes.
func ValidateEntityIDAuthority(candidate, org, platform string, importLane bool) error {
	parsed, err := ParseEntityID(candidate)
	if err != nil {
		return err
	}
	lane := EntityIDLaneLocal
	if importLane {
		lane = EntityIDLaneImport
	}
	foreignIndex := -1
	switch {
	case parsed.Org != org:
		foreignIndex = 0
	case parsed.Platform != platform:
		foreignIndex = 1
	}
	if importLane {
		if foreignIndex == -1 {
			return newEntityIDAuthorityError(EntityIDReasonLocalAuthorityClaimed, 1, lane)
		}
		return nil
	}
	if foreignIndex != -1 {
		return newEntityIDAuthorityError(EntityIDReasonForeignAuthority, foreignIndex, lane)
	}
	return nil
}

func newEntityIDAuthorityError(reason string, segmentIndex int, lane string) error {
	detail := map[string]any{
		EntityIDDetailSegmentIndex: segmentIndex,
	}
	if lane != "" {
		detail[EntityIDDetailLane] = lane
	}
	return newEntityIDContractError(ErrorCodeEntityIDAuthorityInvalid, reason, detail)
}
