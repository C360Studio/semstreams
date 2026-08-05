package builtinprojection

import (
	"testing"

	"github.com/c360studio/semstreams/pkg/projection"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
	"github.com/c360studio/semstreams/vocabulary/builtins"
)

func TestContractsDeclareBuiltinBirthAndMutableLanes(t *testing.T) {
	builtins.Register()

	contracts := Contracts()
	if len(contracts) != 2 {
		t.Fatalf("Contracts() returned %d contracts, want 2", len(contracts))
	}

	loop := contracts[0]
	if loop.Name != LoopExecutionContractName {
		t.Fatalf("loop contract name = %q", loop.Name)
	}
	assertExactSet(t, "loop birth predicates", loop.BirthPredicates, []string{
		agvocab.LoopRole,
		agvocab.LoopTask,
		agvocab.LoopParent,
		agvocab.LoopRun,
		agvocab.LoopRunEntityID,
		agvocab.LoopReplyTo,
		agvocab.LoopWorkflow,
		agvocab.LoopWorkflowStep,
		agvocab.LoopUser,
		agvocab.LoopDescription,
	})
	if len(loop.Groups) != 1 || loop.Groups[0].Name != TodoGroupName ||
		loop.Groups[0].Mode != projection.ModeReconcile {
		t.Fatalf("loop mutable group = %#v", loop.Groups)
	}
	assertExactSet(t, "todo group predicates", loop.Groups[0].Predicates, []string{
		agvocab.TodoRecord,
	})
	assertNoOverlap(t, loop.BirthPredicates, loop.Groups[0].Predicates)

	lesson := contracts[1]
	if lesson.Name != LessonRecordContractName {
		t.Fatalf("lesson contract name = %q", lesson.Name)
	}
	assertExactSet(t, "lesson birth predicates", lesson.BirthPredicates, []string{
		agvocab.LessonCategory,
		agvocab.LessonPolarity,
		agvocab.LessonSeverity,
		agvocab.LessonCreatedAt,
		agvocab.LessonSummary,
		agvocab.LessonDetail,
		agvocab.LessonInjectionForm,
		agvocab.LessonEvidence,
		agvocab.LessonAppliesTo,
		agvocab.LessonObservedRole,
		agvocab.ActionExecutedBy,
	})
	if len(lesson.Groups) != 1 || lesson.Groups[0].Name != LessonLifecycleGroupName ||
		lesson.Groups[0].Mode != projection.ModeReconcile {
		t.Fatalf("lesson mutable group = %#v", lesson.Groups)
	}
	assertExactSet(t, "lesson lifecycle predicates", lesson.Groups[0].Predicates, []string{
		agvocab.LessonStatus,
		agvocab.LessonSupersededBy,
		agvocab.LessonRetiredAt,
	})
	assertNoOverlap(t, lesson.BirthPredicates, lesson.Groups[0].Predicates)

	names := make(map[string]struct{}, len(contracts))
	for _, contract := range contracts {
		if _, duplicate := names[contract.Name]; duplicate {
			t.Fatalf("duplicate contract name %q", contract.Name)
		}
		names[contract.Name] = struct{}{}
	}
	assertExactSet(t, "contract names", mapKeys(names), []string{
		LoopExecutionContractName,
		LessonRecordContractName,
	})

	for _, contract := range contracts {
		if err := contract.Validate(); err != nil {
			t.Errorf("contract %q invalid: %v", contract.Name, err)
		}
	}
}

func assertExactSet(t *testing.T, name string, got, want []string) {
	t.Helper()
	gotSet := make(map[string]struct{}, len(got))
	for _, value := range got {
		if _, duplicate := gotSet[value]; duplicate {
			t.Fatalf("%s contains duplicate %q", name, value)
		}
		gotSet[value] = struct{}{}
	}
	wantSet := make(map[string]struct{}, len(want))
	for _, value := range want {
		wantSet[value] = struct{}{}
	}
	if len(gotSet) != len(wantSet) {
		t.Fatalf("%s = %v, want exact set %v", name, got, want)
	}
	for value := range wantSet {
		if _, found := gotSet[value]; !found {
			t.Fatalf("%s missing %q: got %v", name, value, got)
		}
	}
}

func assertNoOverlap(t *testing.T, left, right []string) {
	t.Helper()
	leftSet := make(map[string]struct{}, len(left))
	for _, value := range left {
		leftSet[value] = struct{}{}
	}
	for _, value := range right {
		if _, overlap := leftSet[value]; overlap {
			t.Fatalf("birth and mutable predicates overlap at %q", value)
		}
	}
}

func mapKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for value := range values {
		keys = append(keys, value)
	}
	return keys
}

func TestContractsReturnsIndependentCopies(t *testing.T) {
	t.Parallel()

	first := Contracts()
	first[0].BirthPredicates[0] = "mutated"
	first[0].Groups[0].Predicates[0] = "mutated"

	second := Contracts()
	if second[0].BirthPredicates[0] == "mutated" ||
		second[0].Groups[0].Predicates[0] == "mutated" {
		t.Fatal("Contracts() leaked mutable slices")
	}
}
