package component

import "fmt"

// StoreReadPort declares streaming read access to a content storage bucket.
// Components use this to read large content (documents, images, video) from
// storage backends (NATS ObjectStore, filesystem, etc.) without coupling to
// a specific backend implementation.
type StoreReadPort struct {
	Bucket    string             `json:"bucket"`              // Storage bucket name (e.g., "MESSAGES")
	Interface *InterfaceContract `json:"interface,omitempty"` // Optional content type contract
}

// ResourceID returns unique identifier for store read ports
func (s StoreReadPort) ResourceID() string {
	return fmt.Sprintf("store-read:%s", s.Bucket)
}

// IsExclusive returns false as multiple readers are allowed
func (s StoreReadPort) IsExclusive() bool {
	return false
}

// Kind returns the canonical port kind.
func (s StoreReadPort) Kind() PortKind {
	return PortKindStoreRead
}
