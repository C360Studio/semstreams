// Package graph defines the types and primitives for the SemStreams knowledge
// graph engine.
package graph

import "encoding/base64"

// DefaultPrefixQueryLimit is the default number of entities returned per page
// when the caller does not specify a limit. Matches the server-side default
// that existed before typed pagination was introduced.
const DefaultPrefixQueryLimit = 1000

// MaxPrefixQueryLimit is the maximum number of entities returned per page.
// The NATS max_payload ceiling (~1 MB) is the binding constraint: a typical
// EntityState with a handful of triples serialises to a few KB; at 1000
// entities that is already several MB. We therefore cap count at
// DefaultPrefixQueryLimit and rely on the byte-budget guard
// (maxPrefixResponseBytes in graph-ingest) as the hard ceiling. Callers
// requesting more than this value are silently clamped.
const MaxPrefixQueryLimit = DefaultPrefixQueryLimit

// PrefixQueryRequest is the typed request envelope for graph.query.prefix.
//
// Cursor contract (OPAQUE):
//   - First page: leave Cursor empty.
//   - Subsequent pages: pass back the NextCursor value VERBATIM from the
//     previous PrefixQueryResponse. Consumers MUST NOT parse, construct, or
//     modify cursor values — the encoding may change between server versions.
//   - Empty NextCursor in the response means the result set is exhausted.
//
// Limit semantics:
//   - <=0 → server applies DefaultPrefixQueryLimit.
//   - Values above MaxPrefixQueryLimit are clamped to MaxPrefixQueryLimit.
//   - Even within the limit, the server may return fewer entities when the
//     accumulated response size would approach the NATS max_payload ceiling.
//     In that case NextCursor is set so the caller can fetch the remainder.
//
// V1 pagination caveat (keyset semantics):
//   - The server sorts matching keys lexicographically and uses the last
//     returned key as the page boundary. Entities whose IDs sort before the
//     cursor position and that are inserted between page fetches will not
//     appear on later pages — standard keyset behaviour.
//
// Backend note (informational, subject to change):
//   - NATS KV has no ranged scan; the server fetches ALL keys matching the
//     prefix, sorts them, then slices the requested page. Large namespaces
//     therefore incur a full key scan on every request.
type PrefixQueryRequest struct {
	// Prefix is a dot-delimited entity-ID prefix (e.g. "acme.ops.robotics").
	// Empty string matches all entities. Do NOT include a trailing dot —
	// the server appends it automatically when filtering.
	Prefix string `json:"prefix"`

	// Limit is the maximum number of entities to return on this page.
	// <=0 means "use server default". Clamped to MaxPrefixQueryLimit.
	Limit int `json:"limit,omitempty"`

	// Cursor is the opaque page-continuation token from a previous response.
	// Leave empty to request the first page.
	Cursor string `json:"cursor,omitempty"`
}

// PrefixQueryResponse is the typed response envelope for graph.query.prefix.
//
// Entities contains the full EntityState values for this page, triples
// included. SemOps depends on full values being present so that downstream
// consumers do not need additional N+1 fetches.
//
// NextCursor is the opaque token to pass as PrefixQueryRequest.Cursor on the
// next call. An empty NextCursor means the result set is exhausted. The field
// is omitted from the JSON output when empty, preserving wire-compatibility
// with old consumers that expect {"entities":[…]}.
type PrefixQueryResponse struct {
	// Entities is the full EntityState slice for this page.
	Entities []EntityState `json:"entities"`

	// NextCursor is the opaque continuation token; empty = exhausted.
	// Old consumers that do not know about pagination safely ignore this field.
	NextCursor string `json:"next_cursor,omitempty"`
}

// EncodeCursor encodes a raw key as an opaque, URL-safe cursor token.
func EncodeCursor(key string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(key))
}

// DecodeCursor decodes a cursor token back to its raw key.
// Returns empty string and no error for the empty-cursor (first-page) case.
func DecodeCursor(cursor string) (string, error) {
	if cursor == "" {
		return "", nil
	}
	b, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
