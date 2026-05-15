package swe

import (
	"fmt"

	"github.com/c360studio/semstreams/vocabulary/export"
)

// Register binds the swe: prefix into the export prefix table.
// Importing the package triggers this from init(); downstream
// callers normally do not need to invoke it directly, but it is
// exported so consumers building a custom export pipeline can
// wire the prefix deterministically.
//
// Idempotent: calling Register a second time with the same
// namespace is a no-op.
func Register() error {
	if err := export.Register(Prefix, Namespace); err != nil {
		return fmt.Errorf("vocabulary/swe: register swe prefix: %w", err)
	}
	return nil
}

func init() {
	if err := Register(); err != nil {
		panic(err)
	}
}
