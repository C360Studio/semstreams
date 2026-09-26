// Package main provides the E2E test application for SemStreams.
//
// It boots the same framework composition as cmd/semstreams (internal/boot),
// with the options internal/e2eboot enables from SEMSTREAMS_E2E_* environment
// variables — the bundled examples, the mission workflow, the lifecycle seed,
// the lesson-curation responder and the three E2E hooks. With none set, its
// boot options are the production options.
package main

import (
	"context"
	"fmt"
	_ "net/http/pprof" // Register pprof handlers on DefaultServeMux (served by service.MaybeStartPProf)
	"os"
	"runtime"

	compositioncli "github.com/c360studio/semstreams/composition/cli"
	"github.com/c360studio/semstreams/internal/boot"
	"github.com/c360studio/semstreams/internal/e2eboot"
	"github.com/c360studio/semstreams/vocabulary/builtins"
)

var (
	// Version is the semantic version of the E2E test application.
	Version = "0.1.0-e2e"
	// GitCommit is the source revision, replaced with release metadata via -ldflags.
	GitCommit = "unknown"
	// BuildTime is the build timestamp, set during compilation.
	BuildTime = "dev"
)

func main() {
	defer func() {
		if r := recover(); r != nil {
			buf := make([]byte, 4096)
			n := runtime.Stack(buf, false)
			_, _ = fmt.Fprintf(os.Stderr, "PANIC: %v\nStack trace:\n%s\n", r, string(buf[:n]))
			os.Exit(2)
		}
	}()

	build := boot.BuildInfo{Version: Version, GitCommit: GitCommit, BuildTime: BuildTime}
	opts := e2eboot.FromEnv(boot.ParseFlags(os.Args[1:], build), build, os.LookupEnv)

	// Composition verbs serve the catalog this binary can compose — including
	// the components its enabled options register — and exit.
	if code, handled := dispatchCompositionVerb(os.Args[1:], opts); handled {
		os.Exit(code)
	}

	// The process composition boundary: the one root context.
	if err := boot.Run(context.Background(), opts); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// dispatchCompositionVerb serves a composition verb against the full catalog
// this binary can compose and reports whether the arguments named one.
func dispatchCompositionVerb(args []string, opts boot.Options) (int, bool) {
	if len(args) == 0 || !compositioncli.IsVerb(args[0]) {
		return 0, false
	}
	builtins.Register()
	registry, err := boot.RegistryFor(opts, nil, true)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1, true
	}
	return compositioncli.Main(args, registry, os.Stdout, os.Stderr), true
}
