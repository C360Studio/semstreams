package e2eboot

import (
	"github.com/c360studio/semstreams/internal/boot"
	"github.com/c360studio/semstreams/test/e2e/harness/milestoneprobe"
)

// milestoneProbeVariable is the probe's own name for its arming variable, so
// the harness and the boot spell it once.
const milestoneProbeVariable = milestoneprobe.EnvVar

// enableMilestoneProbe installs the agentic tier's milestone settlement probe
// (#1155 stage D) on the milestone subscriber before it starts. The handler it
// registers deliberately crashes the process, panics, and stays transient.
func enableMilestoneProbe(opts *boot.Options, _ string) {
	opts.MilestoneHooks = append(opts.MilestoneHooks, milestoneprobe.Register)
}
