package operatingmodel

import (
	"github.com/c360studio/semstreams/component"
)

// init registers the operating-model payload types with the semstreams global
// component.PayloadRegistry so BaseMessage.UnmarshalJSON can recreate typed
// payloads from JSON across the message bus.
//
// Builders are intentionally omitted: the registry's JSON fallback
// (Factory + json.Unmarshal) handles payload construction without requiring
// duplicate field-mapping code.
func init() {
	registerOrPanic(&component.PayloadRegistration{
		Domain:      Domain,
		Category:    CategoryLayerApproved,
		Version:     SchemaVersion,
		Description: "Approved operating-model layer checkpoint emitted by the /onboard interview.",
		Factory:     func() any { return &LayerApproved{} },
	})

	registerOrPanic(&component.PayloadRegistration{
		Domain:      Domain,
		Category:    CategoryProfileContext,
		Version:     SchemaVersion,
		Description: "Assembled operating-model profile context for loop system-prompt injection.",
		Factory:     func() any { return &ProfileContext{} },
	})
}

// registerOrPanic wraps component.RegisterPayload and panics on failure.
// Registration errors at init() time are programming bugs — the process must
// not start with a half-registered payload surface.
func registerOrPanic(registration *component.PayloadRegistration) {
	if err := component.RegisterPayload(registration); err != nil {
		panic("operating-model: failed to register " + registration.MessageType() + ": " + err.Error())
	}
}
