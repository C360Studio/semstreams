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
	return fmt.Sprintf("kv:%s", k.Bucket)
}

// IsExclusive returns false as multiple watchers are allowed
func (k KVWatchPort) IsExclusive() bool {
	return false
}

// Kind returns the canonical port kind.
func (k KVWatchPort) Kind() PortKind {
	return PortKindKVWatch
}

// KVReadPort declares exact or list access to current values in one KV bucket.
// It is metadata only: acquisition and missing-value policy remain component-owned.
type KVReadPort struct {
	Bucket    string             `json:"bucket"`
	Interface *InterfaceContract `json:"interface,omitempty"`
}

// ResourceID returns the canonical KV read resource identity.
func (k KVReadPort) ResourceID() string {
	return fmt.Sprintf("kv:%s", k.Bucket)
}

// IsExclusive reports that concurrent readers may share a bucket.
func (k KVReadPort) IsExclusive() bool {
	return false
}

// Kind returns the canonical port kind.
func (k KVReadPort) Kind() PortKind {
	return PortKindKVRead
}

// KVWritePort - NATS KV Write for state persistence
type KVWritePort struct {
	Bucket    string             `json:"bucket"`              // e.g., "ENTITY_STATES"
	Interface *InterfaceContract `json:"interface,omitempty"` // Data type contract
}

// ResourceID returns unique identifier for KV write ports
func (k KVWritePort) ResourceID() string {
	return fmt.Sprintf("kv:%s", k.Bucket)
}

// IsExclusive returns false as multiple writers are allowed (with CAS handling)
func (k KVWritePort) IsExclusive() bool {
	return false
}

// Kind returns the canonical port kind.
func (k KVWritePort) Kind() PortKind {
	return PortKindKVWrite
}
