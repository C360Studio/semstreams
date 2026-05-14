package oasf

import "strings"

// categoryIDByName is the reverse index of categoryNames, built once at
// init for O(1) lookup. Kept private because the forward direction
// (categoryNames) remains the source of truth — this is a derived view.
var categoryIDByName = func() map[string]uint32 {
	m := make(map[string]uint32, len(categoryNames))
	for id, name := range categoryNames {
		m[name] = id
	}
	return m
}()

// Name returns the OASF hierarchical name (path-segment form) for the
// given canonical class ID, or the empty string if the ID is not part of
// SemStreams' MVP coverage of the OASF taxonomy.
//
// Extension IDs return the empty string here — callers that emit
// extension skills resolve names via [ExtensionName] keyed by the
// originating capability expression, not by ID.
func Name(id uint32) string {
	if name, ok := categoryNames[id]; ok {
		return name
	}
	return ""
}

// Caption returns the OASF human-readable display caption for the given
// canonical class ID. Returns the empty string for unmapped IDs.
func Caption(id uint32) string {
	if caption, ok := categoryCaptions[id]; ok {
		return caption
	}
	return ""
}

// LookupID returns the canonical OASF class ID for the given hierarchical
// name and a boolean indicating whether the lookup succeeded. The name
// may be a bare top-level segment ("natural_language_processing") or a
// full hierarchical path ("natural_language_processing/.../foo"); in the
// hierarchical case only the top-level segment is matched against MVP
// coverage. Subcategory and skill lookups land in a later PR alongside
// subcategories.go / skills.go.
//
// Prefer this over [ID] in mapper code where "you handed me garbage" and
// "the name was empty" need to be distinguished — [ID] collapses both to
// the zero sentinel.
func LookupID(name string) (uint32, bool) {
	if i := strings.IndexByte(name, '/'); i >= 0 {
		name = name[:i]
	}
	id, ok := categoryIDByName[name]
	return id, ok
}

// ID is a convenience wrapper around [LookupID] that returns 0 on miss.
// Zero is intentionally not a valid OASF class ID (the taxonomy starts
// at 1), so callers that don't need to distinguish miss-from-empty can
// rely on `id == 0` as the "not found" sentinel.
//
// For unmapped expressions, callers should fall back to [ExtensionID].
func ID(name string) uint32 {
	id, _ := LookupID(name)
	return id
}

// IsCanonical reports whether the given class ID is part of SemStreams'
// MVP coverage of the published OASF taxonomy (returns true for any
// covered category, subcategory, or skill; false for zero, for extension
// IDs, and for canonical OASF IDs we have not yet added constants for).
//
// Pair with [IsExtension] to distinguish the three states a class ID
// can be in: canonical-and-covered, extension, or unrecognised.
func IsCanonical(id uint32) bool {
	_, ok := categoryNames[id]
	return ok
}
