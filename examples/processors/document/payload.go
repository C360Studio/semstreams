// Package document provides a generic document processor demonstrating the Graphable
// implementation pattern for text-rich content like documents, maintenance records,
// and observations.
//
// This package serves as a reference implementation showing how to:
//   - Create domain-specific payloads that implement the Graphable interface
//   - Generate federated 6-part entity IDs with organizational context
//   - Produce semantic triples using registered vocabulary predicates
//   - Transform incoming JSON into meaningful graph structures
//   - Support multiple document types with a single processor
//
// The document processor handles:
//   - General documents (manuals, reports, guides)
//   - Maintenance records (work orders, repairs)
//   - Observations (safety reports, inspections)
//   - Sensor documents (rich-text sensor descriptions)
//
// Payload types are split into separate files:
//   - payload_document.go: Document struct
//   - payload_maintenance.go: Maintenance struct
//   - payload_observation.go: Observation struct
//   - payload_sensor.go: SensorDocument struct
package document

import (
	"fmt"

	"github.com/c360studio/semstreams/payloadregistry"
)

// Triple source and confidence constants
const (
	tripleSourceName  = "document_processor"
	defaultConfidence = 1.0
)

// payloadRegistrationErrors collects errors from init() for deferred handling.
// This avoids panics during init() which are difficult to handle.
var payloadRegistrationErrors []error

// init registers all document payload types with the global PayloadRegistry.
// Errors are collected in payloadRegistrationErrors instead of panicking.
// Call CheckPayloadRegistration() to verify registration succeeded.
func init() {
	registerPayloads()
}

// Builder functions for document payload types

func buildDocument(fields map[string]any) (any, error) {
	msg := &Document{}

	if v, ok := fields["id"].(string); ok {
		msg.ID = v
	}
	if v, ok := fields["title"].(string); ok {
		msg.Title = v
	}
	if v, ok := fields["description"].(string); ok {
		msg.Description = v
	}
	if v, ok := fields["body"].(string); ok {
		msg.Body = v
	}
	if v, ok := fields["summary"].(string); ok {
		msg.Summary = v
	}
	if v, ok := fields["category"].(string); ok {
		msg.Category = v
	}
	if v, ok := fields["created_at"].(string); ok {
		msg.CreatedAt = v
	}
	if v, ok := fields["updated_at"].(string); ok {
		msg.UpdatedAt = v
	}
	if v, ok := fields["org_id"].(string); ok {
		msg.OrgID = v
	}
	if v, ok := fields["platform"].(string); ok {
		msg.Platform = v
	}

	// Handle tags slice
	if v, ok := fields["tags"].([]any); ok {
		msg.Tags = make([]string, len(v))
		for i, item := range v {
			if str, ok := item.(string); ok {
				msg.Tags[i] = str
			}
		}
	}

	if err := msg.Validate(); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	return msg, nil
}

func buildMaintenance(fields map[string]any) (any, error) {
	msg := &Maintenance{}

	if v, ok := fields["id"].(string); ok {
		msg.ID = v
	}
	if v, ok := fields["title"].(string); ok {
		msg.Title = v
	}
	if v, ok := fields["description"].(string); ok {
		msg.Description = v
	}
	if v, ok := fields["body"].(string); ok {
		msg.Body = v
	}
	if v, ok := fields["technician"].(string); ok {
		msg.Technician = v
	}
	if v, ok := fields["status"].(string); ok {
		msg.Status = v
	}
	if v, ok := fields["completion_date"].(string); ok {
		msg.CompletionDate = v
	}
	if v, ok := fields["category"].(string); ok {
		msg.Category = v
	}
	if v, ok := fields["org_id"].(string); ok {
		msg.OrgID = v
	}
	if v, ok := fields["platform"].(string); ok {
		msg.Platform = v
	}
	if v, ok := fields["tags"].([]any); ok {
		msg.Tags = make([]string, 0, len(v))
		for _, tag := range v {
			if s, ok := tag.(string); ok {
				msg.Tags = append(msg.Tags, s)
			}
		}
	}

	if err := msg.Validate(); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}
	return msg, nil
}

func buildObservation(fields map[string]any) (any, error) {
	msg := &Observation{}

	if v, ok := fields["id"].(string); ok {
		msg.ID = v
	}
	if v, ok := fields["title"].(string); ok {
		msg.Title = v
	}
	if v, ok := fields["description"].(string); ok {
		msg.Description = v
	}
	if v, ok := fields["body"].(string); ok {
		msg.Body = v
	}
	if v, ok := fields["observer"].(string); ok {
		msg.Observer = v
	}
	if v, ok := fields["severity"].(string); ok {
		msg.Severity = v
	}
	if v, ok := fields["observed_at"].(string); ok {
		msg.ObservedAt = v
	}
	if v, ok := fields["category"].(string); ok {
		msg.Category = v
	}
	if v, ok := fields["org_id"].(string); ok {
		msg.OrgID = v
	}
	if v, ok := fields["platform"].(string); ok {
		msg.Platform = v
	}
	if v, ok := fields["tags"].([]any); ok {
		msg.Tags = make([]string, 0, len(v))
		for _, tag := range v {
			if s, ok := tag.(string); ok {
				msg.Tags = append(msg.Tags, s)
			}
		}
	}

	if err := msg.Validate(); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}
	return msg, nil
}

func buildSensorDocument(fields map[string]any) (any, error) {
	msg := &SensorDocument{}

	if v, ok := fields["id"].(string); ok {
		msg.ID = v
	}
	if v, ok := fields["title"].(string); ok {
		msg.Title = v
	}
	if v, ok := fields["description"].(string); ok {
		msg.Description = v
	}
	if v, ok := fields["body"].(string); ok {
		msg.Body = v
	}
	if v, ok := fields["location"].(string); ok {
		msg.Location = v
	}
	if v, ok := fields["reading"].(float64); ok {
		msg.Reading = v
	}
	if v, ok := fields["unit"].(string); ok {
		msg.Unit = v
	}
	if v, ok := fields["category"].(string); ok {
		msg.Category = v
	}
	if v, ok := fields["org_id"].(string); ok {
		msg.OrgID = v
	}
	if v, ok := fields["platform"].(string); ok {
		msg.Platform = v
	}
	if v, ok := fields["tags"].([]any); ok {
		msg.Tags = make([]string, 0, len(v))
		for _, tag := range v {
			if s, ok := tag.(string); ok {
				msg.Tags = append(msg.Tags, s)
			}
		}
	}

	if err := msg.Validate(); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}
	return msg, nil
}

// registerPayloads registers all payload types, collecting errors instead of panicking.
func registerPayloads() {
	// Register Document payload
	if err := payloadregistry.Register(&payloadregistry.Registration{
		Domain:      "content",
		Category:    "document",
		Version:     "v1",
		Description: "Generic document payload with Graphable implementation",
		Factory: func() any {
			return &Document{}
		},
		Builder: buildDocument,
		Example: map[string]any{
			"ID":          "doc-001",
			"Title":       "Safety Manual",
			"Description": "Comprehensive safety guidelines",
			"Category":    "safety",
		},
	}); err != nil {
		payloadRegistrationErrors = append(payloadRegistrationErrors,
			fmt.Errorf("registering Document payload: %w", err))
	}

	// Register Maintenance payload
	if err := payloadregistry.Register(&payloadregistry.Registration{
		Domain:      "content",
		Category:    "maintenance",
		Version:     "v1",
		Description: "Maintenance record payload with Graphable implementation",
		Factory: func() any {
			return &Maintenance{}
		},
		Builder: buildMaintenance,
		Example: map[string]any{
			"ID":         "maint-001",
			"Title":      "Pump Repair",
			"Technician": "John Smith",
			"Status":     "completed",
		},
	}); err != nil {
		payloadRegistrationErrors = append(payloadRegistrationErrors,
			fmt.Errorf("registering Maintenance payload: %w", err))
	}

	// Register Observation payload
	if err := payloadregistry.Register(&payloadregistry.Registration{
		Domain:      "content",
		Category:    "observation",
		Version:     "v1",
		Description: "Observation record payload with Graphable implementation",
		Factory: func() any {
			return &Observation{}
		},
		Builder: buildObservation,
		Example: map[string]any{
			"ID":       "obs-001",
			"Title":    "Safety Hazard Report",
			"Observer": "Jane Doe",
			"Severity": "high",
		},
	}); err != nil {
		payloadRegistrationErrors = append(payloadRegistrationErrors,
			fmt.Errorf("registering Observation payload: %w", err))
	}

	// Register SensorDocument payload
	if err := payloadregistry.Register(&payloadregistry.Registration{
		Domain:      "content",
		Category:    "sensor_doc",
		Version:     "v1",
		Description: "Sensor documentation payload with Graphable implementation",
		Factory: func() any {
			return &SensorDocument{}
		},
		Builder: buildSensorDocument,
		Example: map[string]any{
			"ID":       "sensor-doc-001",
			"Title":    "Temperature Sensor T-42",
			"Location": "Warehouse B",
			"Unit":     "celsius",
		},
	}); err != nil {
		payloadRegistrationErrors = append(payloadRegistrationErrors,
			fmt.Errorf("registering SensorDocument payload: %w", err))
	}
}

// CheckPayloadRegistration returns any errors that occurred during payload registration.
// Call this during component initialization to verify all payloads registered correctly.
func CheckPayloadRegistration() error {
	if len(payloadRegistrationErrors) == 0 {
		return nil
	}
	return fmt.Errorf("payload registration errors: %v", payloadRegistrationErrors)
}
