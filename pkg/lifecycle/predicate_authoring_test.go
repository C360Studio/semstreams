package lifecycle

import "github.com/c360studio/semstreams/vocabulary"

func init() {
	vocabulary.Register("fixture.lifecycle.phase")
	vocabulary.Register("fixture.value.x")
	vocabulary.Register("mission.annotation.note")
	vocabulary.Register("mission.assignment.drone")
	vocabulary.Register("mission.child.subtask")
	vocabulary.Register("mission.control.command")
	vocabulary.Register("mission.identity.owner-org-id")
	vocabulary.Register("mission.lifecycle.phase")
	vocabulary.Register("mission.transition.at")
	vocabulary.Register("mission.transition.from")
	vocabulary.Register("mission.transition.note")
	vocabulary.Register("mission.transition.source")
	vocabulary.Register("phaseonly.lifecycle.phase")
	vocabulary.Register("sensor.lifecycle.phase")
	vocabulary.Register("some.other.predicate")
	vocabulary.Register("workflow.lifecycle.phase")
	vocabulary.Register("x.y.z")
}
