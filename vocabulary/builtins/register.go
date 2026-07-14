// Package builtins registers SemStreams-owned vocabularies at an application
// composition root before configuration authoring validation runs.
package builtins

import (
	"github.com/c360studio/semstreams/vocabulary/agentic"
	"github.com/c360studio/semstreams/vocabulary/rulepacks"
)

// Register declares every first-party vocabulary whose package requires an
// explicit registration call. It is safe to call more than once.
func Register() {
	agentic.Register()
	rulepacks.Register()
}
