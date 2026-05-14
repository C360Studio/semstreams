package oasf

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

// ID returns the canonical OASF class ID for the given hierarchical
// name (or its top-level prefix), or 0 if the name does not match a
// covered class. Zero is intentionally not a valid OASF class ID (the
// taxonomy starts at 1), so callers can rely on `id == 0` as a "not
// found" sentinel.
//
// For unmapped expressions, callers should fall back to [ExtensionID].
func ID(name string) uint32 {
	for id, n := range categoryNames {
		if n == name {
			return id
		}
	}
	return 0
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
