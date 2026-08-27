package document

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadregistry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDocument_GraphableRoundtrip verifies that Document can be type-asserted
// to graph.Graphable after JSON round-trip through BaseMessage
func TestDocument_GraphableRoundtrip(t *testing.T) {
	// Create a document with context fields set
	doc := &Document{
		ID:       "doc-001",
		Title:    "Test Document",
		Category: "test",
		OrgID:    "acme",
		Platform: "logistics",
	}

	// Verify document implements Graphable directly
	var g graph.Graphable = doc
	assert.Equal(t, "acme.logistics.document.content.test.doc-001", g.EntityID())

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
		// OrgID and Platform are now preserved through JSON serialization
		entityID := graphable.EntityID()
		t.Logf("EntityID after round-trip: %s", entityID)
		// The entity ID should now be fully qualified with org and platform
		assert.Equal(t, "acme.logistics.document.content.test.doc-001", entityID)
	}

	// Also test via 'any' - this is what ProcessMessage does
	var anyPayload any = payload
	graphableFromAny, ok := anyPayload.(graph.Graphable)
	assert.True(t, ok, "payload via any should implement graph.Graphable, got type %T", anyPayload)
	if ok {
		t.Logf("EntityID via any: %s", graphableFromAny.EntityID())
	}
}

// TestDocument_OrgPlatformPreservation tests that OrgID and Platform
// ARE preserved through JSON serialization for proper federated entity IDs
func TestDocument_OrgPlatformPreservation(t *testing.T) {
	doc := &Document{
		ID:       "doc-001",
		Title:    "Test",
		OrgID:    "acme",
		Platform: "logistics",
	}

	// Direct check
	assert.Equal(t, "acme.logistics.document.content.general.doc-001", doc.EntityID())

	// After JSON round-trip
	data, err := json.Marshal(doc)
	require.NoError(t, err)
	t.Logf("Serialized doc: %s", string(data))

	var restored Document
	err = json.Unmarshal(data, &restored)
	require.NoError(t, err)

	// OrgID and Platform should be preserved after round-trip
	assert.Equal(t, "acme", restored.OrgID, "OrgID should be preserved in JSON")
	assert.Equal(t, "logistics", restored.Platform, "Platform should be preserved in JSON")

	// EntityID should be fully qualified
	assert.Equal(t, "acme.logistics.document.content.general.doc-001", restored.EntityID())
}
