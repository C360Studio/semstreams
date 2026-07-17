package rule

import "github.com/c360studio/semstreams/vocabulary"

// These are declaration fixtures used by tests that exercise rule behavior
// beyond the authoring gate. Tests for undeclared/malformed predicates use
// distinct names and remain fail-closed.
func init() {
	vocabulary.Register("robotics.battery.level")
	vocabulary.Register("robotics.battery.voltage")
	vocabulary.Register("some.test.predicate")
	vocabulary.Register("test.entity.field")
	vocabulary.Register("workflow.state.next-phase")
}
