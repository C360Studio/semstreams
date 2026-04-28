package operatingmodel

import (
	"errors"
	"fmt"

	"github.com/c360studio/semstreams/payloadregistry"
)

// RegisterPayloads registers the operating-model payload types with the
// supplied registry. Called from payloadbuiltins.Register at process
// bootstrap.
//
// Builders are intentionally omitted: the registry's JSON fallback
// (Factory + json.Unmarshal) handles payload construction without
// requiring duplicate field-mapping code.
func RegisterPayloads(reg *payloadregistry.Registry) error {
	registrations := []*payloadregistry.Registration{
		{
			Domain:      Domain,
			Category:    CategoryLayerApproved,
			Version:     SchemaVersion,
			Description: "Approved operating-model layer checkpoint emitted by the /onboard interview.",
			Factory:     func() any { return &LayerApproved{} },
		},
		{
			Domain:      Domain,
			Category:    CategoryProfileContext,
			Version:     SchemaVersion,
			Description: "Assembled operating-model profile context for loop system-prompt injection.",
			Factory:     func() any { return &ProfileContext{} },
		},
	}

	var errs []error
	for _, r := range registrations {
		if err := reg.Register(r); err != nil {
			errs = append(errs, fmt.Errorf("register %s: %w", r.MessageType(), err))
		}
	}
	return errors.Join(errs...)
}
