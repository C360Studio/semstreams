// Package identity provides local DID-based cryptographic identity primitives.
package identity

import (
	"fmt"
	"strings"
)

// DID represents a Decentralized Identifier as specified by W3C DID Core.
// See: https://www.w3.org/TR/did-core/
type DID struct {
	// Method identifies the DID method (for example, "web" or "key").
	Method string `json:"method"`

	// ID is the method-specific identifier.
	ID string `json:"id"`

	// Fragment is an optional fragment reference (e.g., key reference).
	Fragment string `json:"fragment,omitempty"`
}

// Common DID methods
const (
	// MethodKey is the did:key method using public key encoding.
	MethodKey = "key"

	// MethodWeb is the did:web method using DNS domain names.
	MethodWeb = "web"
)

// ParseDID parses a DID string into a DID struct.
// Format: did:method:method-specific-id[#fragment]
func ParseDID(s string) (*DID, error) {
	if s == "" {
		return nil, fmt.Errorf("empty DID string")
	}

	// Check for did: prefix
	if !strings.HasPrefix(s, "did:") {
		return nil, fmt.Errorf("DID must start with 'did:': %q", s)
	}

	// Remove did: prefix
	remainder := s[4:]

	// Extract fragment if present
	var fragment string
	if idx := strings.Index(remainder, "#"); idx != -1 {
		fragment = remainder[idx+1:]
		remainder = remainder[:idx]
	}

	// Split method and ID
	parts := strings.SplitN(remainder, ":", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid DID format, expected did:method:id: %q", s)
	}

	method := parts[0]
	id := parts[1]

	if method == "" {
		return nil, fmt.Errorf("DID method cannot be empty: %q", s)
	}
	if id == "" {
		return nil, fmt.Errorf("DID ID cannot be empty: %q", s)
	}

	return &DID{
		Method:   method,
		ID:       id,
		Fragment: fragment,
	}, nil
}

// String returns the canonical string representation of the DID.
func (d *DID) String() string {
	s := fmt.Sprintf("did:%s:%s", d.Method, d.ID)
	if d.Fragment != "" {
		s += "#" + d.Fragment
	}
	return s
}

// Validate checks if the DID is valid.
func (d *DID) Validate() error {
	if d.Method == "" {
		return fmt.Errorf("method is required")
	}
	if d.ID == "" {
		return fmt.Errorf("id is required")
	}
	return nil
}

// WithFragment returns a copy of the DID with the specified fragment.
func (d *DID) WithFragment(fragment string) DID {
	return DID{
		Method:   d.Method,
		ID:       d.ID,
		Fragment: fragment,
	}
}

// WithoutFragment returns a copy of the DID without any fragment.
func (d *DID) WithoutFragment() DID {
	return DID{
		Method: d.Method,
		ID:     d.ID,
	}
}

// IsMethod checks if the DID uses the specified method.
func (d *DID) IsMethod(method string) bool {
	return d.Method == method
}

// Equal checks if two DIDs are equal (including fragment).
func (d *DID) Equal(other *DID) bool {
	if other == nil {
		return false
	}
	return d.Method == other.Method && d.ID == other.ID && d.Fragment == other.Fragment
}

// EqualIgnoreFragment checks if two DIDs are equal ignoring the fragment.
func (d *DID) EqualIgnoreFragment(other *DID) bool {
	if other == nil {
		return false
	}
	return d.Method == other.Method && d.ID == other.ID
}

// MarshalText implements encoding.TextMarshaler.
func (d DID) MarshalText() ([]byte, error) {
	return []byte(d.String()), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
func (d *DID) UnmarshalText(text []byte) error {
	parsed, err := ParseDID(string(text))
	if err != nil {
		return err
	}
	*d = *parsed
	return nil
}

// NewKeyDID creates a new did:key DID from a public key multibase encoding.
func NewKeyDID(multibaseKey string) *DID {
	return &DID{
		Method: MethodKey,
		ID:     multibaseKey,
	}
}

// NewWebDID creates a new did:web DID from a domain name.
// The domain should be URL-encoded for special characters.
func NewWebDID(domain string, paths ...string) *DID {
	id := domain
	for _, path := range paths {
		id += ":" + path
	}
	return &DID{
		Method: MethodWeb,
		ID:     id,
	}
}
