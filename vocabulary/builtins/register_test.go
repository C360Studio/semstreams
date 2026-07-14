package builtins

import (
	"testing"

	"github.com/c360studio/semstreams/vocabulary"
	"github.com/c360studio/semstreams/vocabulary/agentic"
	"github.com/c360studio/semstreams/vocabulary/rulepacks"
)

func TestRegisterProvidesBootVocabularyWithoutIncidentalInitialization(t *testing.T) {
	restore := vocabulary.SnapshotRegistry()
	defer restore()
	vocabulary.ClearRegistry()

	Register()
	for _, predicate := range []string{
		agentic.LoopRole,
		agentic.CoordinatorNextAction,
		agentic.OpsDiagnosisSeverity,
		rulepacks.WorkflowStatePhase,
	} {
		if err := vocabulary.RequireDeclaredPredicate(predicate); err != nil {
			t.Errorf("boot vocabulary missing %q: %v", predicate, err)
		}
	}
}
