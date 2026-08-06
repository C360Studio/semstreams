package projection

import (
	"errors"
	"testing"

	"github.com/c360studio/semstreams/vocabulary"
)

func TestContractUsesOnlyReconcileAndAppendIntent(t *testing.T) {
	t.Cleanup(vocabulary.SnapshotRegistry())
	vocabulary.Register("test.value.name")
	vocabulary.Register("test.event.seen")
	contract := Contract{
		Name: "test", EntityPattern: "*.*.test.system.widget.*",
		Groups: []PredicateGroup{
			{Name: "state", Mode: ModeReconcile, Predicates: []string{"test.value.name"}},
			{Name: "events", Mode: ModeAppend, Predicates: []string{"test.event.seen"}},
		},
	}
	if err := contract.Validate(); err != nil {
		t.Fatal(err)
	}
	contract.Groups[1].Mode = "owned"
	if err := contract.Validate(); !errors.Is(err, ErrInvalidContract) {
		t.Fatalf("Validate() = %v, want invalid contract", err)
	}
}

func TestValidateContractsRejectsDuplicateNames(t *testing.T) {
	t.Cleanup(vocabulary.SnapshotRegistry())
	vocabulary.Register("test.value.name")
	contract := Contract{
		Name: "test", EntityPattern: "*.*.test.system.widget.*",
		BirthPredicates: []string{"test.value.name"},
	}
	if err := ValidateContracts([]Contract{contract, contract}); !errors.Is(err, ErrInvalidContract) {
		t.Fatalf("ValidateContracts() = %v, want invalid contract", err)
	}
}
