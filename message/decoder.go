package message

import (
	"encoding/json"

	"github.com/c360studio/semstreams/payloadregistry"
	"github.com/c360studio/semstreams/pkg/errs"
)

// Decoder unmarshal JSON wire-format messages into a *BaseMessage,
// resolving payload type discriminators against the supplied registry.
// It is the only public path to a registry-aware BaseMessage. Mirrors
// stdlib json.Decoder / gob.Decoder.
//
//	dec := message.NewDecoder(deps.PayloadRegistry)
//	msg, err := dec.Decode(data)
//
// In hot paths (per-message handlers), construct the Decoder once at
// component init/start time and store it on the component to avoid
// per-message allocation:
//
//	c.decoder = message.NewDecoder(c.deps.PayloadRegistry)
//	// ...later, per message:
//	msg, err := c.decoder.Decode(data)
type Decoder struct {
	registry *payloadregistry.Registry
}

// NewDecoder constructs a Decoder bound to the supplied registry.
// reg must be non-nil; a Decoder built with nil fails fast on every
// Decode call so misconfiguration surfaces at the first message rather
// than silently using a stale registry.
func NewDecoder(reg *payloadregistry.Registry) *Decoder {
	return &Decoder{registry: reg}
}

// Decode unmarshal data into a fresh *BaseMessage bound to this
// Decoder's registry.
func (d *Decoder) Decode(data []byte) (*BaseMessage, error) {
	if d == nil || d.registry == nil {
		return nil, errs.WrapInvalid(
			errs.ErrInvalidConfig,
			"Decoder", "Decode", "decoder constructed with nil registry")
	}
	msg := &BaseMessage{registry: d.registry}
	if err := json.Unmarshal(data, msg); err != nil {
		return nil, err
	}
	return msg, nil
}
