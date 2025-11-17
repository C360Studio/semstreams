package component

import "fmt"

// NATSPort - NATS pub/sub
type NATSPort struct {
	Subject   string             `json:"subject"`
	Queue     string             `json:"queue,omitempty"`
	Interface *InterfaceContract `json:"interface,omitempty"`
}

// ResourceID returns unique identifier for NATS ports
func (n NATSPort) ResourceID() string {
	return fmt.Sprintf("nats:%s", n.Subject)
}

// IsExclusive returns false as multiple components can subscribe
func (n NATSPort) IsExclusive() bool {
	return false
}

// Type returns the port type identifier
func (n NATSPort) Type() string {
	return "nats"
}

// NATSRequestPort - NATS Request/Response pattern for synchronous operations
type NATSRequestPort struct {
	Subject   string             `json:"subject"`
	Timeout   string             `json:"timeout,omitempty"` // Duration string e.g. "1s", "500ms"
	Retries   int                `json:"retries,omitempty"`
	Interface *InterfaceContract `json:"interface,omitempty"`
}

// ResourceID returns unique identifier for NATS request ports
func (n NATSRequestPort) ResourceID() string {
	return fmt.Sprintf("nats-request:%s", n.Subject)
}

// IsExclusive returns false as multiple components can handle requests
func (n NATSRequestPort) IsExclusive() bool {
	return false
}

// Type returns the port type identifier
func (n NATSRequestPort) Type() string {
	return "nats-request"
}
