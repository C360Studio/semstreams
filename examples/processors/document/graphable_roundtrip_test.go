package document

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadregistry"
	"github.com/c360studio/semstreams/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testAuthority is a deployment authority in the shape a composition root
// supplies it: platform.org / platform.id. "dep1" is a deployment name, not a
// product name — position 2 never carries a product (ADR-102 d2, d3).
var testAuthority = types.PlatformMeta{Org: "acme", Platform: "dep1"}

// TestDocument_GraphableRoundtrip verifies that Document can be type-asserted
// to graph.Graphable after JSON round-trip through BaseMessage
func TestDocument_GraphableRoundtrip(t *testing.T) {
	// Create a document whose identity was minted under the deployment
	// authority, the way the processor mints it.
	doc := &Document{
		ID:            "doc-001",
		Title:         "Test Document",
		Category:      "test",
		EntityIDValue: MintDocumentEntityID(testAuthority, "test", "doc-001"),
	}

	// Verify document implements Graphable directly
	var g graph.Graphable = doc
	assert.Equal(t, "acme.dep1.document.content.test.doc-001", g.EntityID())

	// Create BaseMessage
	msgType := message.Type{
		Domain:   "content",
		Category: "document",
		Version:  "v1",
	}
	baseMsg := message.NewBaseMessage(msgType, doc, "test-source")

	// Marshal to JSON
	data, err := json.Marshal(baseMsg)
	require.NoError(t, err)
	t.Logf("Serialized message: %s", string(data))

	// Unmarshal back
	reg := payloadregistry.NewWithSubset(t, RegisterPayloads)
	restored, err := message.NewDecoder(reg).Decode(data)
	require.NoError(t, err)

	// Extract payload
	payload := restored.Payload()
	require.NotNil(t, payload)
	t.Logf("Payload type: %T", payload)

	// Critical test: Can we assert to Graphable?
	graphable, ok := payload.(graph.Graphable)
	assert.True(t, ok, "payload should implement graph.Graphable, got type %T", payload)

	if ok {
		// The minted identity is preserved through JSON serialization.
		entityID := graphable.EntityID()
		t.Logf("EntityID after round-trip: %s", entityID)
		assert.Equal(t, "acme.dep1.document.content.test.doc-001", entityID)
	}

	// Also test via 'any' - this is what ProcessMessage does
	var anyPayload any = payload
	graphableFromAny, ok := anyPayload.(graph.Graphable)
	assert.True(t, ok, "payload via any should implement graph.Graphable, got type %T", anyPayload)
	if ok {
		t.Logf("EntityID via any: %s", graphableFromAny.EntityID())
	}
}

// TestDocument_MintedIdentityPreservation tests that the identity minted
// under the deployment authority survives JSON serialization, and that the
// retired authority fields are gone from the wire shape (ADR-102 d2).
func TestDocument_MintedIdentityPreservation(t *testing.T) {
	doc := &Document{
		ID:            "doc-001",
		Title:         "Test",
		EntityIDValue: MintDocumentEntityID(testAuthority, "", "doc-001"),
	}

	// Direct check — the empty category resolves to the documented default.
	assert.Equal(t, "acme.dep1.document.content.general.doc-001", doc.EntityID())

	// After JSON round-trip
	data, err := json.Marshal(doc)
	require.NoError(t, err)
	t.Logf("Serialized doc: %s", string(data))

	var wire map[string]any
	require.NoError(t, json.Unmarshal(data, &wire))
	for _, retired := range []string{"org_id", "platform"} {
		_, present := wire[retired]
		assert.Falsef(t, present, "retired authority field %q is still on the wire", retired)
	}

	var restored Document
	err = json.Unmarshal(data, &restored)
	require.NoError(t, err)

	// The minted identity is preserved after round-trip.
	assert.Equal(t, "acme.dep1.document.content.general.doc-001", restored.EntityID())
}
