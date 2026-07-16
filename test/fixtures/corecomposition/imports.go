// Package corecomposition is a build-graph fixture for the framework-core
// composition roots. It intentionally imports registration packages only.
package corecomposition

import (
	// Blank imports are intentional: this fixture measures dependency closure
	// without executing package registration.
	_ "github.com/c360studio/semstreams/componentregistry"
	_ "github.com/c360studio/semstreams/payloadbuiltins"
	_ "github.com/c360studio/semstreams/processor/agentic-tools/executors"
)
