package component

import "fmt"

// HTTPClientPort describes an outbound HTTP-client / polling input dependency:
// a relationship where the component initiates connections to an external HTTP
// resource (REST API, alert feed, snapshot endpoint) rather than binding a
// listener (NetworkPort) or consuming a NATS subject (NATSPort).
//
// It is a DESCRIPTOR, not a runtime — like TimerPort it declares the dependency
// shape so lifecycle/config/health/flowgraph/ownership/telemetry tooling can SEE
// it; the polling/HTTP execution stays in the component. Cadence is NOT owned
// here: when polling is timer-driven the component also declares a TimerPort and
// this port references it by name via TriggerPort (single source of truth for
// the interval).
//
// Secrets never appear in this descriptor. AuthRef carries a credential
// REFERENCE (a key name resolved at runtime from the component's secret source),
// never a value. No field holds a secret.
type HTTPClientPort struct {
	Method        string             `json:"method,omitempty"`         // Required HTTP method
	URLPattern    string             `json:"url_pattern"`              // target endpoint/resource pattern; NO inline creds, NO query-string secrets
	TriggerPort   string             `json:"trigger_port,omitempty"`   // NAME of a sibling TimerPort driving cadence (empty = event-driven/internal)
	AuthRef       string             `json:"auth_ref,omitempty"`       // credential key name resolved at runtime; NEVER a secret value; empty = unauthenticated
	ContactPolicy string             `json:"contact_policy,omitempty"` // public User-Agent/contact identity (feeds often require it); not a secret
	Interface     *InterfaceContract `json:"interface,omitempty"`      // message-interface contract this port produces
}

// ResourceID returns a unique identifier for the HTTP client dependency.
// Two ports with the same method and URL pattern identify the same external resource.
func (h HTTPClientPort) ResourceID() string {
	return fmt.Sprintf("http-client:%s:%s", h.Method, h.URLPattern)
}

// IsExclusive returns false: an outbound client relationship is shareable
// (multiple components may poll the same endpoint), unlike a NetworkPort listener.
func (h HTTPClientPort) IsExclusive() bool { return false }

// Kind returns the canonical port kind.
func (h HTTPClientPort) Kind() PortKind { return PortKindHTTPClient }
