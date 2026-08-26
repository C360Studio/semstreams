// Package payloadbuiltins registers the framework-core payload set.
//
// Why this lives outside payloadregistry: payloadregistry must remain a
// leaf package with no upward deps (the cycle that beta.16 broke). This
// aggregator imports every package that owns first-party payloads, so
// it has to live above them in the import graph.
//
// Examples are NOT included — they're domain-specific and registered
// separately by binaries that load them (typically cmd/e2e-semstreams).
package payloadbuiltins

import (
	"errors"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/governance"
	"github.com/c360studio/semstreams/graph/inference"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadregistry"
	"github.com/c360studio/semstreams/pkg/lifecycle"
	agenticdispatch "github.com/c360studio/semstreams/processor/agentic-dispatch"
	gateddagexec "github.com/c360studio/semstreams/processor/gated-dag"
	"github.com/c360studio/semstreams/storage/objectstore"
)

// Register registers all first-party payload types with the supplied
// registry. Aggregates errors via errors.Join so a misconfigured
// deployment sees every collision on a single boot — the registry's
// duplicate-key rejection is the collision detector for every framework
// type (ADR-103).
//
// Called from cmd/semstreams/main.go and cmd/e2e-semstreams/main.go
// after the registry is constructed but before component lifecycle
// begins. Downstream binaries (semspec, semdragon) may call this and
// then layer their own custom payload registrations on top via
// reg.Register(...).
func Register(reg *payloadregistry.Registry) error {
	var errs []error
	track := func(err error) {
		if err != nil {
			errs = append(errs, err)
		}
	}

	track(message.RegisterPayloads(reg))
	track(agentic.RegisterPayloads(reg))
	track(agenticdispatch.RegisterPayloads(reg))
	track(gateddagexec.RegisterPayloads(reg))
	track(objectstore.RegisterPayloads(reg))
	track(governance.RegisterPayloads(reg))
	// Framework entity types born on the mutation lane (ADR-103): the
	// lifecycle harness carrier and graph-ingest's hierarchy container.
	track(lifecycle.RegisterPayloads(reg))
	track(inference.RegisterPayloads(reg))

	return errors.Join(errs...)
}
