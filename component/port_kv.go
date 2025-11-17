package component

import "fmt"

// KVWatchPort - NATS KV Watch for state observation
type KVWatchPort struct {
	Bucket    string             `json:"bucket"`            // e.g., "ENTITY_STATES"
	Keys      []string           `json:"keys,omitempty"`    // Keys to watch, empty = all
	History   bool               `json:"history,omitempty"` // Include historical values
	Interface *InterfaceContract `json:"interface,omitempty"`
}

// ResourceID returns unique identifier for KV watch ports
func (k KVWatchPort) ResourceID() string {
	return fmt.Sprintf("kvwatch:%s", k.Bucket)
}

// IsExclusive returns false as multiple watchers are allowed
func (k KVWatchPort) IsExclusive() bool {
	return false
}

// Type returns the port type identifier
func (k KVWatchPort) Type() string {
	return "kvwatch"
}

// KVWritePort - NATS KV Write for state persistence
type KVWritePort struct {
	Bucket    string             `json:"bucket"`              // e.g., "ENTITY_STATES"
	Interface *InterfaceContract `json:"interface,omitempty"` // Data type contract
}

// ResourceID returns unique identifier for KV write ports
func (k KVWritePort) ResourceID() string {
	return fmt.Sprintf("kvwrite:%s", k.Bucket)
}

// IsExclusive returns false as multiple writers are allowed (with CAS handling)
func (k KVWritePort) IsExclusive() bool {
	return false
}

// Type returns the port type identifier
func (k KVWritePort) Type() string {
	return "kvwrite"
}
