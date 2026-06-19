package graph

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCursorRoundTrip verifies that EncodeCursor/DecodeCursor are inverses
// for a representative set of entity ID values.
func TestCursorRoundTrip(t *testing.T) {
	cases := []string{
		"acme.ops.logistics.warehouse.robot.robot-001",
		"c360.platform.robotics.mav1.drone.001",
		"z",
		"a.b.c.d.e.f",
		// Characters that are special in standard Base64 but safe in URL-safe:
		"org.sys-hyphen.dom_underscore.sub.type.inst",
	}
	for _, key := range cases {
		t.Run(key, func(t *testing.T) {
			encoded := EncodeCursor(key)
			decoded, err := DecodeCursor(encoded)
			require.NoError(t, err)
			assert.Equal(t, key, decoded, "round-trip must be identity")
		})
	}
}

// TestDecodeCursorEmpty verifies the first-page sentinel: empty cursor decodes
// to empty string without error.
func TestDecodeCursorEmpty(t *testing.T) {
	key, err := DecodeCursor("")
	require.NoError(t, err)
	assert.Equal(t, "", key)
}

// TestDecodeCursorInvalid verifies that a malformed cursor returns an error
// rather than silently producing garbage.
func TestDecodeCursorInvalid(t *testing.T) {
	_, err := DecodeCursor("!!!not-base64!!!")
	require.Error(t, err, "malformed cursor must return error")
}

// TestPrefixQueryResponseWireSuperset verifies that the old wire shape
// {"entities":[]} decodes cleanly into PrefixQueryResponse with an empty
// Entities slice and no NextCursor — old consumers are unaffected.
func TestPrefixQueryResponseWireSuperset(t *testing.T) {
	oldWire := []byte(`{"entities":[]}`)
	var resp PrefixQueryResponse
	require.NoError(t, json.Unmarshal(oldWire, &resp))
	assert.Empty(t, resp.Entities)
	assert.Empty(t, resp.NextCursor, "old wire shape must leave NextCursor empty")
}

// TestPrefixQueryResponseOmitsNextCursorWhenEmpty verifies that marshalling a
// PrefixQueryResponse with an empty NextCursor does NOT include the field —
// preserving wire-compatibility with consumers that expect {"entities":[…]}.
func TestPrefixQueryResponseOmitsNextCursorWhenEmpty(t *testing.T) {
	resp := PrefixQueryResponse{
		Entities: []EntityState{},
	}
	b, err := json.Marshal(resp)
	require.NoError(t, err)

	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(b, &raw))
	_, hasNextCursor := raw["next_cursor"]
	assert.False(t, hasNextCursor, "next_cursor must be absent when empty (omitempty)")
}

// TestPrefixQueryRequestOldShapeUnmarshal verifies that the old wire shape
// {"prefix":"foo","limit":10} unmarshals cleanly into PrefixQueryRequest.
func TestPrefixQueryRequestOldShapeUnmarshal(t *testing.T) {
	oldWire := []byte(`{"prefix":"foo","limit":10}`)
	var req PrefixQueryRequest
	require.NoError(t, json.Unmarshal(oldWire, &req))
	assert.Equal(t, "foo", req.Prefix)
	assert.Equal(t, 10, req.Limit)
	assert.Empty(t, req.Cursor, "old wire shape must leave Cursor empty")
}

// TestPrefixQueryRequestMarshalOmitsCursorWhenEmpty verifies that a
// PrefixQueryRequest with no Cursor omits the "cursor" key from JSON
// output — keeping the wire shape identical to old consumers' expectations.
func TestPrefixQueryRequestMarshalOmitsCursorWhenEmpty(t *testing.T) {
	req := PrefixQueryRequest{Prefix: "acme", Limit: 5}
	b, err := json.Marshal(req)
	require.NoError(t, err)

	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(b, &raw))
	_, hasCursor := raw["cursor"]
	assert.False(t, hasCursor, "cursor must be absent when empty (omitempty)")
}

// TestPrefixQueryResponseWithNextCursor verifies the happy-path pagination shape:
// a populated NextCursor survives a JSON round-trip.
func TestPrefixQueryResponseWithNextCursor(t *testing.T) {
	entityID := "acme.ops.logistics.warehouse.robot.robot-001"
	cursor := EncodeCursor(entityID)
	resp := PrefixQueryResponse{
		Entities:   []EntityState{{ID: entityID}},
		NextCursor: cursor,
	}
	b, err := json.Marshal(resp)
	require.NoError(t, err)

	var decoded PrefixQueryResponse
	require.NoError(t, json.Unmarshal(b, &decoded))
	assert.Equal(t, cursor, decoded.NextCursor)
	require.Len(t, decoded.Entities, 1)
	assert.Equal(t, entityID, decoded.Entities[0].ID)
}

// TestPrefixQueryLimitConstants verifies the published constant values so
// downstream consumers that hard-code comparisons against them detect
// accidental changes.
func TestPrefixQueryLimitConstants(t *testing.T) {
	assert.Equal(t, 1000, DefaultPrefixQueryLimit)
	assert.Equal(t, DefaultPrefixQueryLimit, MaxPrefixQueryLimit,
		"MaxPrefixQueryLimit must equal DefaultPrefixQueryLimit until byte-budget refinement")
}
