package e2eboot

import (
	"github.com/c360studio/semstreams/cmd/e2e-semstreams/mission"
	"github.com/c360studio/semstreams/internal/boot"
)

// enableMission registers the lifecycle tier's mission-command component, its
// payload, and the mission Participant workflow.
func enableMission(opts *boot.Options, _ string) {
	opts.Components = append(opts.Components, mission.Register)
	opts.Payloads = append(opts.Payloads, mission.RegisterPayloads)
	opts.Workflows = append(opts.Workflows, mission.WorkflowDeclaration())
}
