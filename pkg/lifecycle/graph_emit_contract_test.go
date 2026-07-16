package lifecycle

import (
	"testing"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
)

func TestValidateMutationResponseEntityRejectsPoison(t *testing.T) {
	t.Parallel()

	validID := "acme.ops.test.system.widget.001"
	invalidEntityID := "bad"
	entity := &graph.EntityState{ID: validID, Triples: []message.Triple{{
		Subject: validID, Predicate: "test.state.target", Object: invalidEntityID, Datatype: message.EntityReferenceDatatype,
	}}}

	err := validateMutationResponseEntity(entity)
	if err == nil || !graph.IsStateContractError(err) {
		t.Fatalf("error = %T %v, want graph state reset contract", err, err)
	}
	if err := validateMutationResponseEntity(nil); err != nil {
		t.Fatalf("nil degraded response entity error = %v", err)
	}
}

// entity-id-audit:classify intentional-malformed "bad" line=14 column=21 surface=go-assignment:invalidEntityID lifecycle mutation reply reference poison fixture
