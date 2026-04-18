package agenticmemory

import (
	"sync/atomic"

	operatingmodel "github.com/c360studio/semstreams/agentic/operating-model"
	"github.com/c360studio/semstreams/component"
)

// outputSubject returns the resolved NATS subject for a named output port,
// replacing the trailing wildcard (*) with suffix.
func (c *Component) outputSubject(portName, suffix string) string {
	return component.ResolveSubject(c.outputPortDefs(), portName, suffix)
}

// SetProfileReader wires a ProfileReader for assembling operating-model
// profile context. Production deployments call this with a graph-backed
// reader during component initialization; tests may supply a stub.
func (c *Component) SetProfileReader(reader operatingmodel.ProfileReader) {
	if reader == nil {
		reader = operatingmodel.EmptyProfileReader{}
	}
	c.profileReader.Store(&reader)
}

// getProfileReader returns the currently-installed ProfileReader.
func (c *Component) getProfileReader() operatingmodel.ProfileReader {
	if r := c.profileReader.Load(); r != nil {
		return *r
	}
	return operatingmodel.EmptyProfileReader{}
}

// initProfileReader sets up the default empty reader in the constructor.
func initProfileReader(pr *atomic.Pointer[operatingmodel.ProfileReader]) {
	var initial operatingmodel.ProfileReader = operatingmodel.EmptyProfileReader{}
	pr.Store(&initial)
}
