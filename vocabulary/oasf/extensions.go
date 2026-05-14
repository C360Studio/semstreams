package oasf

import (
	"hash/fnv"
	"strings"
)

// ExtensionBase is the floor of the ID range SemStreams uses for skills
// outside the published OASF taxonomy. The upstream taxonomy uses
// 1-to-5-digit IDs (categories 1-15, subcategories in the hundreds,
// specific skills in the ten-thousands); reserving IDs at and above
// 9,000,000 leaves room for future taxonomy growth without collision.
//
// See [ADR-042] for the rationale on emitting unmapped capabilities as
// extensions rather than coercing them into published classes.
//
// [ADR-042]: ../../docs/adr/042-oasf-taxonomy-adoption.md
const ExtensionBase uint32 = 9_000_000

// extensionPrefix is the hierarchical-name prefix used for extension
// skills so consumers parsing Skill.name can distinguish AGNTCY-canonical
// skills from SemStreams extensions on the wire.
//
//	canonical: "natural_language_processing/natural_language_understanding/contextual_comprehension"
//	extension: "semstreams/ops/anomaly_review"
const extensionPrefix = "semstreams"

// ExtensionID returns a stable extension class ID for the given internal
// capability expression. The mapping is deterministic — the same input
// always returns the same ID across processes and restarts — and falls
// at or above [ExtensionBase].
//
// Use this from the oasf-generator when an internal expression does not
// match any published OASF class via [ID]. The corresponding
// hierarchical name is [ExtensionName] applied to the same input.
//
// IDs are derived from a 32-bit FNV-1a hash of the expression masked to
// the ~16M slot above ExtensionBase. Collisions are theoretically
// possible (~16M slots) but unconcerning at MVP scale; if collisions
// ever matter, the resolution path is a curated mapping table, not
// shifting the algorithm.
func ExtensionID(expression string) uint32 {
	expr := strings.ToLower(strings.TrimSpace(expression))
	if expr == "" {
		return 0
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(expr))
	// Mask to a 24-bit slot above ExtensionBase, giving ~16M extension
	// slots starting at 9,000,000.
	return ExtensionBase + (h.Sum32() & 0x00FFFFFF)
}

// ExtensionName returns the hierarchical name for an extension skill
// keyed by the given internal capability expression. The result is
// prefixed with "semstreams/" so wire consumers can distinguish
// extension skills from canonical OASF taxonomy entries by name alone.
//
// Empty or whitespace-only expressions return the bare prefix, which
// callers should treat as "unidentified extension".
func ExtensionName(expression string) string {
	expr := strings.ToLower(strings.TrimSpace(expression))
	if expr == "" {
		return extensionPrefix
	}
	// Convert kebab-case to snake_case so the hierarchy reads
	// consistently with OASF's path-segment convention.
	expr = strings.ReplaceAll(expr, "-", "_")
	return extensionPrefix + "/" + expr
}

// IsExtension reports whether the given class ID falls in the SemStreams
// extension range. Use this in oasf-generator and downstream consumers
// to gate behaviour that depends on AGNTCY-canonical vs extension class
// membership (e.g., richer JSON-LD export for canonical classes).
func IsExtension(id uint32) bool {
	return id >= ExtensionBase
}
