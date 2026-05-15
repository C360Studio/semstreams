package sosa

import (
	"fmt"

	"github.com/c360studio/semstreams/vocabulary/export"
)

// Register binds the sosa: and ssn: prefixes into the export
// prefix table. Importing the package triggers this from init();
// downstream callers normally do not need to invoke it directly,
// but it is exported so consumers that build a custom export
// pipeline (skipping the package's init) can still wire the
// prefixes deterministically.
//
// Registration is idempotent: calling Register a second time with
// the same namespaces is a no-op. The function returns an error
// only if vocabulary/export rejects the binding — which today only
// happens if a different namespace was already bound to the same
// prefix (collision).
func Register() error {
	if err := export.Register(Prefix, Namespace); err != nil {
		return fmt.Errorf("vocabulary/sosa: register sosa prefix: %w", err)
	}
	if err := export.Register(SSNPrefix, SSNNamespace); err != nil {
		return fmt.Errorf("vocabulary/sosa: register ssn prefix: %w", err)
	}
	return nil
}

func init() {
	if err := Register(); err != nil {
		// Hard panic: this can only happen if a downstream package
		// rebound sosa: or ssn: to a foreign namespace before our
		// init ran, which is a build-time integration bug, not a
		// runtime condition we can recover from.
		panic(err)
	}
}
