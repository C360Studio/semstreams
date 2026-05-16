package csapi

import (
	"fmt"

	"github.com/c360studio/semstreams/vocabulary/export"
)

// Register binds the csapi: prefix into the export prefix table.
// Importing the package triggers this from init(); idempotent.
func Register() error {
	if err := export.Register(Prefix, Namespace); err != nil {
		return fmt.Errorf("vocabulary/csapi: register csapi prefix: %w", err)
	}
	return nil
}

func init() {
	if err := Register(); err != nil {
		panic(err)
	}
}
