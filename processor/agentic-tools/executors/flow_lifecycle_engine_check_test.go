package executors

// Compile-time assertion that *flowengine.Engine satisfies
// FlowEngineManager. Lives in a _test.go file so the production
// flow_lifecycle.go can stay free of the heavy flowengine import — the
// production executor only needs the interface, and product shells
// (cmd/semteams, cmd/semstreams) wire the concrete engine at boot.
//
// If a future engine refactor changes Deploy/Start/Stop/Undeploy
// signatures, this assertion fails at `go test` time with a clear
// "does not implement FlowEngineManager" message — louder than a
// silent caller-side type error at boot.

import (
	"github.com/c360studio/semstreams/engine"
)

var _ FlowEngineManager = (*flowengine.Engine)(nil)
