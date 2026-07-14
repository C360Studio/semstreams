package lifecycle

import "github.com/c360studio/semstreams/vocabulary"

func init() {
	for _, predicate := range []string{
		"fixture.lifecycle.phase",
		"fixture.value.x",
		"mission.annotation.note",
		"mission.assignment.drone",
		"mission.child.subtask",
		"mission.control.command",
		"mission.identity.owner-org-id",
		"mission.lifecycle.phase",
		"mission.transition.at",
		"mission.transition.from",
		"mission.transition.note",
		"mission.transition.source",
		"phaseonly.lifecycle.phase",
		"sensor.lifecycle.phase",
		"some.other.predicate",
		"workflow.lifecycle.phase",
		"x.y.z",
	} {
		vocabulary.Register(predicate)
	}
}
