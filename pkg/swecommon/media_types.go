package swecommon

// CS API media type identifiers for SWE Common encodings. These
// match the strings the OGC Connected Systems API uses in Accept /
// Content-Type negotiation. Importing consumers (e.g. CS API
// gateways) should reference these constants rather than redeclare
// the strings, so a future OGC errata that changes a suffix lands
// in exactly one place.
const (
	// MediaSWEJSON — schema-bound JSON encoding.
	MediaSWEJSON = "application/swe+json"

	// MediaSWECSV — schema-bound text encoding using CSV-shaped
	// separators (comma token, newline block).
	MediaSWECSV = "application/swe+csv"

	// MediaSWEBinary — schema-bound packed primitives binary
	// encoding.
	MediaSWEBinary = "application/swe+binary"
)

// SubsetObservationValues is the legacy semconnect-side header
// value used to flag the value-only projection subset (no schema
// binding). Consumers that have migrated to this package's
// schema-bound encoders should drop the header entirely; the
// constant is provided so the migration code path can grep for one
// canonical token while it removes the workaround.
const SubsetObservationValues = "observation-values"
