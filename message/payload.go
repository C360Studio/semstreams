package message

import "encoding/json"

// Payload represents the data carried by a message.
// All message payloads must implement this interface to provide
// schema information, validation, and serialization capabilities.
//
// Payloads may also implement behavioral interfaces (Graphable,
// Locatable, Observable, etc.) to expose additional capabilities
// that can be discovered and utilized at runtime.
//
// Example implementation:
//
//	type GPSPayload struct {
//	    DeviceID  string    `json:"device_id"`
//	    Latitude  float64   `json:"latitude"`
//	    Longitude float64   `json:"longitude"`
//	    Timestamp time.Time `json:"timestamp"`
//	}
//
//	func (p *GPSPayload) Schema() Type {
//	    return Type{Domain: "sensors", Category: "gps", Version: "v1"}
//	}
//
//	func (p *GPSPayload) Validate() error {
//	    if p.DeviceID == "" {
//	        return errors.New("device ID is required")
//	    }
//	    if p.Latitude < -90 || p.Latitude > 90 {
//	        return errors.New("invalid latitude")
//	    }
//	    if p.Longitude < -180 || p.Longitude > 180 {
//	        return errors.New("invalid longitude")
//	    }
//	    return nil
//	}
//
//	func (p *GPSPayload) MarshalJSON() ([]byte, error) {
//	    // Use alias to avoid infinite recursion
//	    type Alias GPSPayload
//	    return json.Marshal((*Alias)(p))
//	}
//
//	func (p *GPSPayload) UnmarshalJSON(data []byte) error {
//	    // Use alias to avoid infinite recursion
//	    type Alias GPSPayload
//	    return json.Unmarshal(data, (*Alias)(p))
//	}
type Payload interface {
	// Schema returns the Type that defines this payload's structure.
	// This enables type-safe routing and processing throughout the system.
	Schema() Type

	// Validate checks the payload data for correctness.
	// Returns nil if valid, or an error describing the validation failure.
	// Should validate:
	//   - Required fields are present
	//   - Values are within acceptable ranges
	//   - Business rules are satisfied
	Validate() error

	// JSON serialization using standard Go interfaces.
	// Payloads must implement json.Marshaler and json.Unmarshaler
	// for deterministic serialization. The same payload must always
	// produce the same JSON output.
	json.Marshaler
	json.Unmarshaler
}
