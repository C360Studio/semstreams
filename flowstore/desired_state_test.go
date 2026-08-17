package flowstore

import "testing"

func TestDesiredStateValues(t *testing.T) {
	states := []DesiredState{DesiredAbsent, DesiredDisabled, DesiredEnabled}
	want := []string{"absent", "disabled", "enabled"}
	for index, state := range states {
		if string(state) != want[index] {
			t.Fatalf("state[%d] = %q, want %q", index, state, want[index])
		}
	}
}
