package contract

import (
	"fmt"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/componentregistry"
	optionalotel "github.com/c360studio/semstreams/frameworkadapters/otel"
	"github.com/c360studio/semstreams/frameworkcapabilities/graphresearch"
)

// registerPublishedComposition mirrors task schema:generate. The published
// catalog intentionally includes core, graph research, and the explicitly
// selected optional OTEL adapter.
func registerPublishedComposition(registry *component.Registry) error {
	if err := componentregistry.Register(registry); err != nil {
		return fmt.Errorf("register core: %w", err)
	}
	if err := graphresearch.RegisterComponents(registry); err != nil {
		return fmt.Errorf("register graph research: %w", err)
	}
	if err := optionalotel.Register(registry); err != nil {
		return fmt.Errorf("register OTEL: %w", err)
	}
	return nil
}
