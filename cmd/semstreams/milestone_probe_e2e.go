//go:build e2e_process_barrier

package main

import (
	"log/slog"

	"github.com/c360studio/semstreams/agentic/agentrun"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/test/e2e/harness/milestoneprobe"
)

// registerE2EMilestoneProbe is a compile-time E2E overlay on the SAME tag the
// agentic tier's image already builds (docker/Dockerfile target
// e2e-process-barrier). It reaches the probe only when
// milestoneprobe.EnvVar — SEMSTREAMS_E2E_MILESTONE_PROBE — is set, which only
// docker/compose/agentic.yml does.
func registerE2EMilestoneProbe(
	subscriber *agentrun.MilestoneSubscriber, client *natsclient.Client, logger *slog.Logger,
) error {
	return milestoneprobe.Register(subscriber, client, logger)
}
