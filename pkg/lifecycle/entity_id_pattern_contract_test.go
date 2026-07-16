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
			w.ReferencePredicates = []ReferenceSpec{{
				Predicate: "mission.annotation.note", TargetPattern: "a.b.c.d.e._bad",
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
