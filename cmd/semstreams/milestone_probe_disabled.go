//go:build !e2e_process_barrier

package main

import (
	"log/slog"

	"github.com/c360studio/semstreams/agentic/agentrun"
	"github.com/c360studio/semstreams/natsclient"
)

// registerE2EMilestoneProbe is a compile-time no-op in every ordinary build.
// The default cmd/semstreams dependency graph does not import the E2E harness,
// so no production binary contains the probe or the code that reads its
// environment variable.
func registerE2EMilestoneProbe(*agentrun.MilestoneSubscriber, *natsclient.Client, *slog.Logger) error {
	return nil
}
