package scenarios

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestGatewayShapePrefixProbeRequiresEntityPage(t *testing.T) {
	t.Parallel()

	var prefixProbe *shapeProbe
	for _, probe := range gatewayShapeProbes() {
		if probe.field == "entitiesByPrefix" {
			probe := probe
			prefixProbe = &probe
			break
		}
	}
	if prefixProbe == nil {
		t.Fatal("entitiesByPrefix shape probe is missing")
	}
	if !strings.Contains(prefixProbe.query, "entities { id }") ||
		!strings.Contains(prefixProbe.query, "next_cursor") {
		t.Fatalf("prefix probe does not select EntityPage fields: %s", prefixProbe.query)
	}

	page := json.RawMessage(`{"entities":[],"next_cursor":"after-page"}`)
	if err := assertProjectedShape(*prefixProbe, page); err != nil {
		t.Fatalf("valid EntityPage rejected: %v", err)
	}
	if err := assertProjectedShape(*prefixProbe, json.RawMessage(`[]`)); err == nil {
		t.Fatal("retired bare-array shape was accepted")
	}
}
