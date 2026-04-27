package operatingmodel

import "github.com/c360studio/semstreams/payloadregistry"

// init registers the operating-model payload types with the semstreams global
// payloadregistry.Registry so BaseMessage.UnmarshalJSON can recreate typed
// payloads from JSON across the message bus.
//
// Builders are intentionally omitted: the registry's JSON fallback
// (Factory + json.Unmarshal) handles payload construction without requiring
// duplicate field-mapping code.
func init() {
	registerOrPanic(&payloadregistry.Registration{
		Domain:      Domain,
		Category:    CategoryLayerApproved,
		Version:     SchemaVersion,
		Description: "Approved operating-model layer checkpoint emitted by the /onboard interview.",
		Factory:     func() any { return &LayerApproved{} },
	})

	registerOrPanic(&payloadregistry.Registration{
		Domain:      Domain,
		Category:    CategoryProfileContext,
		Version:     SchemaVersion,
		Description: "Assembled operating-model profile context for loop system-prompt injection.",
		Factory:     func() any { return &ProfileContext{} },
	})
}

// registerOrPanic wraps payloadregistry.Register and panics on failure.
// Registration errors at init() time are programming bugs — the process must
// not start with a half-registered payload surface.
func registerOrPanic(registration *payloadregistry.Registration) {
	if err := payloadregistry.Register(registration); err != nil {
		panic("operating-model: failed to register " + registration.MessageType() + ": " + err.Error())
	}
}
