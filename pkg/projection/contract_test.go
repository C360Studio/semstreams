package projection

import (
	"errors"
	"testing"
	"time"

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

// TestContractLiteralCompilesAgainstAliases documents that a contract literal
// written against pkg/projection's names validates exactly as before the leaf
// split: Contract, PredicateGroup, WriteMode, ModeReconcile, ModeAppend, and
// ErrInvalidContract are aliases of pkg/projection/contract.
func TestContractLiteralCompilesAgainstAliases(t *testing.T) {
	t.Cleanup(vocabulary.SnapshotRegistry())
	vocabulary.Register("test.value.name")
	contract := Contract{
		Name: "alias", EntityPattern: "*.*.test.system.widget.*",
		Groups: []PredicateGroup{{Name: "state", Mode: ModeReconcile, Predicates: []string{"test.value.name"}}},
	}
	if err := contract.Validate(); err != nil {
		t.Fatal(err)
	}
	var mode WriteMode = ModeAppend
	if mode != "append" {
		t.Fatalf("ModeAppend = %q", mode)
	}
	if err := ValidateContracts(nil); !errors.Is(err, ErrInvalidContract) {
		t.Fatalf("ValidateContracts(nil) = %v, want invalid contract", err)
	}
}

// TestOverlappingLocalContractsConstruct documents that contracts are local
// schemas: two clients whose contracts overlap on pattern and predicate both
// construct without any global registration.
func TestOverlappingLocalContractsConstruct(t *testing.T) {
	t.Cleanup(vocabulary.SnapshotRegistry())
	vocabulary.Register("test.value.name")
	requester := &projectionRequester{handle: func(string, []byte) ([]byte, error) { return nil, nil }}
	first := Contract{
		Name: "first", EntityPattern: "*.*.test.system.widget.*",
		BirthPredicates: []string{"test.value.name"},
	}
	second := Contract{
		Name: "second", EntityPattern: "*.*.test.system.widget.*",
		BirthPredicates: []string{"test.value.name"},
	}
	if _, err := newMutationClient(requester, []Contract{first}, time.Second); err != nil {
		t.Fatalf("first client: %v", err)
	}
	if _, err := newMutationClient(requester, []Contract{second}, time.Second); err != nil {
		t.Fatalf("second client: %v", err)
	}
}
