// Package payloadbuiltins aggregates first-party payload registrations
// into a single bootstrap call. Mirrors how componentregistry aggregates
// first-party component factories.
//
// Why this lives outside payloadregistry: payloadregistry must remain a
// leaf package with no upward deps (the cycle that beta.16 broke). This
// aggregator imports every package that owns first-party payloads, so
// it has to live above them in the import graph.
//
// Examples are NOT included — they're domain-specific and registered
// separately by binaries that load them (typically cmd/e2e-semstreams).
// Federation payloads are domain-parameterized and called explicitly by
// products that use them (semsource, semquery, etc.).
package payloadbuiltins

import (
	"errors"

	"github.com/c360studio/semstreams/agentic"
	agenticoperatingmodel "github.com/c360studio/semstreams/agentic/operating-model"
	githubwebhook "github.com/c360studio/semstreams/input/github-webhook"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/message/oms"
	"github.com/c360studio/semstreams/payloadregistry"
	agenticdispatch "github.com/c360studio/semstreams/processor/agentic-dispatch"
	"github.com/c360studio/semstreams/processor/rule"
	"github.com/c360studio/semstreams/storage/objectstore"
)

// Register registers all first-party payload types with the supplied
// registry. Aggregates errors via errors.Join so a misconfigured
// deployment sees every collision on a single boot.
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
	track(oms.RegisterPayloads(reg))
	track(agentic.RegisterPayloads(reg))
	track(agenticoperatingmodel.RegisterPayloads(reg))
	track(agenticdispatch.RegisterPayloads(reg))
	track(rule.RegisterPayloads(reg))
	track(githubwebhook.RegisterPayloads(reg))
	track(objectstore.RegisterPayloads(reg))

	return errors.Join(errs...)
}
