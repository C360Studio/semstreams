package boot

import (
	"fmt"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/componentregistry"
	"github.com/c360studio/semstreams/config"
	optionalotel "github.com/c360studio/semstreams/frameworkadapters/otel"
	"github.com/c360studio/semstreams/frameworkcapabilities/graphresearch"
)

// RegistryFor builds the component registry for opts: the core factories, the
// component extensions, and the optional capabilities. With full set it
// registers every capability this binary can compose — the catalog the
// composition verbs and --validate judge against, so a configuration that
// selects graph research or OTEL validates the same way offline; cfg is not
// read and may be nil. Without full, boot registers only the capabilities cfg
// selects. The full-versus-selected policy itself is #1107's question.
func RegistryFor(opts Options, cfg *config.Config, full bool) (*component.Registry, error) {
	registry := component.NewRegistry()
	if err := componentregistry.Register(registry); err != nil {
		return nil, fmt.Errorf("register components: %w", err)
	}
	if full || graphresearch.Selected(cfg) {
		if err := graphresearch.RegisterComponents(registry); err != nil {
			return nil, fmt.Errorf("register graph research components: %w", err)
		}
	}
	if full || optionalotel.Selected(cfg) {
		if err := optionalotel.Register(registry); err != nil {
			return nil, fmt.Errorf("register optional OTEL adapter: %w", err)
		}
	}
	for _, register := range opts.Components {
		if err := register(registry); err != nil {
			return nil, fmt.Errorf("register component extension: %w", err)
		}
	}
	return registry, nil
}
