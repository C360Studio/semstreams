package rule

import "github.com/c360studio/semstreams/vocabulary"

// These are declaration fixtures used by tests that exercise rule behavior
// beyond the authoring gate. Tests for undeclared/malformed predicates use
// distinct names and remain fail-closed.
func init() {
	for _, predicate := range []string{
		"robotics.battery.level",
		"robotics.battery.voltage",
		"some.test.predicate",
		"test.entity.field",
		"workflow.state.next-phase",
	} {
		vocabulary.Register(predicate)
	}
}
