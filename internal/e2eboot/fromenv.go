// Package e2eboot builds the E2E binary's boot options. It is the only package
// that imports the E2E harness (test/e2e/harness/*), the slow-consumer probe,
// the bundled examples, and the fixture and mission packages; only
// cmd/e2e-semstreams imports it, so the production binary links none of them.
//
// Each option is enabled by one SEMSTREAMS_E2E_* environment variable that its
// tier's compose service sets (the tier table in the payload-registry
// specification). With none set, FromEnv returns exactly the production
// options.
package e2eboot

import (
	"sort"

	"github.com/c360studio/semstreams/internal/boot"
)

// option is one E2E-only registration or hook: the variable that enables it
// and what it appends to the boot options. value is the variable's (nonempty)
// value, which only LIFECYCLE_SEED reads.
type option struct {
	name   string
	enable func(opts *boot.Options, value string)
}

// options is the one table of E2E boot options, in design § 2.2 order.
var options = []option{
	{name: "SEMSTREAMS_E2E_EXAMPLES", enable: enableExamples},
	{name: "SEMSTREAMS_E2E_MISSION", enable: enableMission},
	{name: "SEMSTREAMS_E2E_LIFECYCLE_SEED", enable: enableLifecycleSeed},
	{name: "SEMSTREAMS_E2E_LESSON_CURATION", enable: enableLessonCuration},
	{name: "SEMSTREAMS_E2E_PROCESS_BARRIER", enable: enableProcessBarrier},
	{name: milestoneProbeVariable, enable: enableMilestoneProbe},
	{name: "SEMSTREAMS_E2E_SLOW_CONSUMER", enable: enableSlowConsumer},
}

// FromEnv returns the production options for cli and build, extended by every
// option whose variable lookup reports with a nonempty value. `NAME=` is the
// same as unset. SEMSTREAMS_E2E_* names this table does not hold are ignored:
// compose files are checked against the tier table statically, and the E2E
// runner's own variables share the prefix (for example
// SEMSTREAMS_E2E_GLOBALSEARCH_TIMEOUT), so refusing them would break a
// hand-launched binary for nothing.
func FromEnv(cli boot.CLI, build boot.BuildInfo, lookup func(string) (string, bool)) boot.Options {
	opts := boot.Production(cli, build)
	for _, opt := range options {
		if value, _ := lookup(opt.name); value != "" {
			opt.enable(&opts, value)
		}
	}
	return opts
}

// Names returns the variables FromEnv reads, sorted.
func Names() []string {
	names := make([]string, 0, len(options))
	for _, opt := range options {
		names = append(names, opt.name)
	}
	sort.Strings(names)
	return names
}
