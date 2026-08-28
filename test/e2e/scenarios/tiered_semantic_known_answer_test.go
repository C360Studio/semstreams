package scenarios

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/test/e2e/config"
)

func TestHasExactEntityDigestLabelRequiresLabelOnMeasuredFixture(t *testing.T) {
	t.Parallel()

	entityID := config.TierEntityID(config.VariantSemantic, "document.content.operations.doc-ops-001")
	const title = "Forklift Operation Manual"

	tests := []struct {
		name string
		body string
		want bool
	}{
		{
			name: "exact measured title",
			body: `{"entity_digests":[{"id":"` + entityID + `","label":"` + title + `"}]}`,
			want: true,
		},
		{
			name: "matching ID cannot replace label",
			body: `{"entity_digests":[{"id":"` + entityID + `","label":"doc-ops-001"}]}`,
		},
		{
			name: "title on a different row cannot replace ID join",
			body: `{"entity_digests":[{"id":"` +
				config.TierEntityID(config.VariantSemantic, "document.content.operations.other") +
				`","label":"` + title + `"}]}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var response globalSearchPayload
			if err := json.Unmarshal([]byte(test.body), &response); err != nil {
				t.Fatalf("unmarshal fixture: %v", err)
			}
			if got := hasExactEntityDigestLabel(response, entityID, title); got != test.want {
				t.Fatalf("hasExactEntityDigestLabel() = %v, want %v", got, test.want)
			}
		})
	}
}
