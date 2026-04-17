package operatingmodel

import (
	"fmt"
	"strings"

	"github.com/c360studio/semstreams/message"
)

// ProfileEntityID constructs the 6-part entity ID for a user's operating-model
// profile root.
// Format: {org}.{platform}.user.teams.profile.{userID}
//
// Panics if any input part is empty or contains a dot; callers are responsible
// for supplying well-formed identifiers.
func ProfileEntityID(org, platform, userID string) string {
	mustValidatePart("org", org)
	mustValidatePart("platform", platform)
	mustValidatePart("userID", userID)

	id := fmt.Sprintf("%s.%s.user.teams.profile.%s", org, platform, userID)
	mustValidateEntityID("ProfileEntityID", id)
	return id
}

// LayerEntityID constructs the 6-part entity ID for a per-user layer checkpoint.
// Format: {org}.{platform}.user.teams.om-layer.{userID}-{layer}
//
// The instance part ({userID}-{layer}) is treated as opaque — callers should
// not attempt to reverse-parse it. Both userIDs and layer names may contain
// hyphens and underscores, so the boundary is not unambiguously recoverable
// from the string alone. Use LayerEntityID whenever a layer identifier is
// needed, not manual string concatenation.
//
// Panics if any input part is empty, contains a dot, or if layer is not one of
// the canonical layer names.
func LayerEntityID(org, platform, userID, layer string) string {
	mustValidatePart("org", org)
	mustValidatePart("platform", platform)
	mustValidatePart("userID", userID)
	mustValidatePart("layer", layer)
	if !IsValidLayer(layer) {
		panic(fmt.Sprintf("LayerEntityID: %q is not a canonical layer name", layer))
	}

	instance := fmt.Sprintf("%s-%s", userID, layer)
	id := fmt.Sprintf("%s.%s.user.teams.om-layer.%s", org, platform, instance)
	mustValidateEntityID("LayerEntityID", id)
	return id
}

// EntryEntityID constructs the 6-part entity ID for an operating-model entry.
// Format: {org}.{platform}.user.teams.om-entry.{entryID}
//
// Panics if any input part is empty or contains a dot.
func EntryEntityID(org, platform, entryID string) string {
	mustValidatePart("org", org)
	mustValidatePart("platform", platform)
	mustValidatePart("entryID", entryID)

	id := fmt.Sprintf("%s.%s.user.teams.om-entry.%s", org, platform, entryID)
	mustValidateEntityID("EntryEntityID", id)
	return id
}

// mustValidatePart panics if value is empty or contains a dot. Dots are
// reserved as part separators in the 6-part entity ID format.
func mustValidatePart(name, value string) {
	if value == "" {
		panic(fmt.Sprintf("operating-model entity ID: %s must not be empty", name))
	}
	if strings.Contains(value, ".") {
		panic(fmt.Sprintf("operating-model entity ID: %s %q must not contain dots", name, value))
	}
}

// mustValidateEntityID panics if the constructed entity ID fails the
// semstreams 6-part validation. This is a programming-error guard.
func mustValidateEntityID(caller, id string) {
	if !message.IsValidEntityID(id) {
		panic(fmt.Sprintf("%s: constructed id %q failed IsValidEntityID", caller, id))
	}
}
