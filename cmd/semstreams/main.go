// Package main implements the entry point for the SemStreams application.
// SemStreams is a semantic stream processing framework that combines
// protocol-level data processing with semantic knowledge graph capabilities.
//
// The boot sequence lives in internal/boot, shared with cmd/e2e-semstreams;
// this binary boots it with the production options, which carry no extension.
package main

import (
	"context"
	"fmt"
	_ "net/http/pprof" // Register pprof handlers on DefaultServeMux (served by service.MaybeStartPProf)
	"os"
	"runtime"

	compositioncli "github.com/c360studio/semstreams/composition/cli"
	"github.com/c360studio/semstreams/internal/boot"
	"github.com/c360studio/semstreams/vocabulary/builtins"
)

var (
	// Version is the semantic version, replaced with release metadata via -ldflags.
	Version = "0.1.0"
	// GitCommit is the source revision, replaced with release metadata via -ldflags.
	GitCommit = "unknown"
	// BuildTime is the build timestamp, replaced with release metadata via -ldflags.
	BuildTime = "dev"
)

func main() {
	// Add panic recovery
	defer func() {
		if r := recover(); r != nil {
			buf := make([]byte, 4096)
			n := runtime.Stack(buf, false)
			_, _ = fmt.Fprintf(os.Stderr, "PANIC: %v\nStack trace:\n%s\n", r, string(buf[:n]))
			os.Exit(2)
		}
	}()

	opts := boot.Production(boot.ParseFlags(os.Args[1:]), boot.BuildInfo{
		Version: Version, GitCommit: GitCommit, BuildTime: BuildTime,
	})

	// Composition verbs (catalog, validate <config>, graph <config>) serve
	// the catalog this binary can compose and exit; no NATS, no banner.
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
