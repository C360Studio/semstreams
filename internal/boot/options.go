// Package boot is the one framework composition function both framework
// binaries boot through (#1301). cmd/semstreams calls Run with Production();
// cmd/e2e-semstreams calls it with the options internal/e2eboot enables from
// SEMSTREAMS_E2E_* environment variables. The two binaries differ only by the
// value of Options — never by a second copy of the boot sequence, and never by
// a build tag.
//
// The package is internal by ruling (#1301 scope bound: "unexported, so no
// Tier 1 surface"); sister applications do not compose through it.
package boot

import (
	"context"
	"io"
	"log/slog"

	"github.com/c360studio/semstreams/agentic/agentrun"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadregistry"
	"github.com/c360studio/semstreams/pkg/lifecycle"
	"github.com/c360studio/semstreams/pkg/projection"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
	"github.com/c360studio/semstreams/processor/agentic-tools/executors"
	"github.com/c360studio/semstreams/types"
)

// BuildInfo is the release metadata each main injects with
// -ldflags "-X main.Version=… -X main.GitCommit=… -X main.BuildTime=…". The
// variables stay in package main because ldflags target it; each main copies
// its own into Options.Build.
type BuildInfo struct {
	Version   string
	GitCommit string
	BuildTime string
}

// Options is the one seam through which cmd/semstreams and cmd/e2e-semstreams
// diverge. Production() fills the scalars and leaves every extension empty;
// e2eboot.FromEnv appends to the extensions it is told to. Each extension is
// applied at exactly one boot phase, named in Run.
type Options struct {
	// CLI carries the scalars ParseFlags produced — one parser, production's,
	// for both mains.
	CLI

	// NATSURLs overrides the NATS server URLs; empty falls through to
	// SEMSTREAMS_NATS_URLS, then the config's nats.urls, then the default.
	NATSURLs string
	// Build is the release metadata the main was linked with.
	Build BuildInfo

	// Extensions, in the order Run applies them.

	// ConfigPatches run on the loaded file configuration before it is
	// validated, so a patched configuration is validated like any other.
	ConfigPatches []func(*config.Config) error
	// AfterConnect runs once the NATS client is connected and before the
	// config manager arbitrates the effective configuration.
	AfterConnect []func(context.Context, *natsclient.Client) error
	// Components register component factories beside componentregistry.Register.
	Components []func(*component.Registry) error
	// Payloads register payload types beside payloadbuiltins.Register.
	Payloads []func(*payloadregistry.Registry) error
	// Tools register tool executors after executors.RegisterBuiltins.
	Tools []func(context.Context, *agentictools.ExecutorRegistry, executors.ToolDependencies) error
	// Workflows register after lifecycle.NewManager, before agentrun.Register.
	Workflows []lifecycle.Workflow
	// Responders subscribe after the graph runtime is wired; each returned
	// closer is closed with the root resources at shutdown.
	Responders []func(context.Context, *natsclient.Client, *projection.MutationClient, *slog.Logger) (io.Closer, error)
	// MilestoneHooks run inside the milestone-service registration, before the
	// subscriber starts.
	MilestoneHooks []func(*agentrun.MilestoneSubscriber, *natsclient.Client, *slog.Logger) error
	// PostStart runs after every service has started, with the lifecycle
	// manager and the deployment's platform identity.
	PostStart []func(context.Context, *lifecycle.Manager, types.PlatformMeta) error
}

// Production returns the options of the shipped binary: the parsed command
// line and the build metadata, with no extension.
func Production(cli CLI, build BuildInfo) Options {
	return Options{CLI: cli, Build: build}
}
