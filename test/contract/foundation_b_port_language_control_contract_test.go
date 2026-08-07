package contract

import (
	"path/filepath"
	"testing"

	"github.com/c360studio/semstreams/internal/portgrammarcontrol"
)

func TestFoundationBPortLanguagePopulationIsFrozen(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := portgrammarcontrol.LoadPlan(root)
	if err != nil {
		t.Fatalf("load Foundation B control artifacts: %v", err)
	}
	live, err := portgrammarcontrol.Census(root)
	if err != nil {
		t.Fatalf("census Foundation B migration population: %v", err)
	}
	if err := plan.ValidateAgainst(live); err != nil {
		t.Fatalf("Foundation B migration population drift: %v", err)
	}
	if len(plan.ConfigItems()) != 522 || plan.MechanicalCount() != 448 || len(plan.Dispositions) != 74 {
		t.Fatalf("configuration population changed: rows=%d mechanical=%d dispositions=%d",
			len(plan.ConfigItems()), plan.MechanicalCount(), len(plan.Dispositions))
	}
	if len(plan.GoItems()) != 124 || plan.GoFileCount() != 34 || plan.GoSourceCount() != 41 {
		t.Fatalf("Go population changed: literals=%d files=%d sources=%d",
			len(plan.GoItems()), plan.GoFileCount(), plan.GoSourceCount())
	}
}
