package lifecycle

import (
	"errors"
	"testing"

	"github.com/c360studio/semstreams/pkg/errs"
	semtypes "github.com/c360studio/semstreams/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWorkflowValidateUsesCanonicalEntityPatterns(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		configure func(*Workflow)
	}{
		{name: "workflow partial wildcard", configure: func(w *Workflow) {
			w.EntityIDPattern = "a.b.c.d.e.foo*"
		}},
		{name: "reference leading underscore", configure: func(w *Workflow) {
			w.ReferencePredicates = []ReferenceSpec{{ // predicate-audit:unrelated {"column":28,"surface":"go-assignment:ReferencePredicates","value":"","basis":"reviewed:predicate-container-values-audited"}
				Predicate: "mission.annotation.note", TargetPattern: "a.b.c.d.e._bad", // entity-id-audit:classify intentional-malformed "a.b.c.d.e._bad" line=25 column=58 surface=go-field:.TargetPattern entity_id_pattern_invalid:first_byte leading underscore rejection fixture
			}}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workflow := lifecycle{}.fixtureWorkflow()
			tt.configure(&workflow)
			err := workflow.validate()
			require.Error(t, err)
			assert.True(t, errors.Is(err, ErrInvalidWorkflow))
			assert.True(t, errs.IsInvalid(err))
			var classified *errs.ClassifiedError
			require.True(t, errors.As(err, &classified))
			assert.Equal(t, semtypes.ErrorCodeEntityIDPatternInvalid, classified.Code)
		})
	}
}
