package e2eboot

import (
	"github.com/c360studio/semstreams/cmd/e2e-semstreams/fixtures"
	"github.com/c360studio/semstreams/examples/processors/document"
	iotsensor "github.com/c360studio/semstreams/examples/processors/iot_sensor"
	"github.com/c360studio/semstreams/internal/boot"
)

// enableExamples registers the bundled example processors (iot_sensor,
// document) and their payloads, plus the fixture payloads — the keys the E2E
// scenarios stamp on entity.create (ADR-103), without which every scenario
// birth through the real wire is refused. The examples are kept out of
// componentregistry.Register so downstream consumers do not inherit them.
func enableExamples(opts *boot.Options, _ string) {
	opts.Components = append(opts.Components, iotsensor.Register, document.Register)
	opts.Payloads = append(opts.Payloads,
		iotsensor.RegisterPayloads, document.RegisterPayloads, fixtures.RegisterPayloads)
}
