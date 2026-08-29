//go:build !e2e_process_barrier

package main

import (
	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/natsclient"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

// prepareE2EProcessBarrierConfig leaves ordinary runtime configuration intact.
func prepareE2EProcessBarrierConfig(*config.Config) error { return nil }

// registerE2EProcessBarrier is a compile-time no-op in every ordinary build.
// The default cmd/semstreams dependency graph does not import the E2E harness.
func registerE2EProcessBarrier(*agentictools.ExecutorRegistry, *natsclient.Client) error {
	return nil
}
