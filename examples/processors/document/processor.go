package document

import (
	"fmt"

	"github.com/c360studio/semstreams/types"
)

// Payload represents a document that implements both Graphable and Payload interfaces.
// Used as a common return type from the processor.
type Payload interface {
	EntityID() string
	Validate() error
}

// Processor transforms incoming JSON document data into Graphable payloads.
// It mints every entity ID under the deployment authority and produces
// document instances with proper federated entity IDs and semantic triples.
type Processor struct {
	// authority is the composition root's platform.org / platform.id,
	// received through component.Dependencies.Platform. It is the ONLY
	// source of positions 1-2 of every entity this processor mints: not an
	// operator config key, not a constant, not a product name, and not a
	// field on an incoming payload (ADR-102 d2).
	authority types.PlatformMeta
}

// NewProcessor creates a new document processor minting under the given
// deployment authority. Callers pass component.Dependencies.Platform verbatim.
func NewProcessor(authority types.PlatformMeta) *Processor {
	return &Processor{
		authority: authority,
	}
}

// Process transforms incoming JSON data into a Graphable document payload.
// It determines the document type from the "type" field and creates
// the appropriate payload.
//
// Expected JSON format (common fields):
//
//	{
//	  "id": "doc-001",
//	  "type": "document|maintenance|observation|sensor_doc",
//	  "title": "Document Title",
//	  "description": "Document description for semantic search",
//	  ...type-specific fields...
//	}
func (p *Processor) Process(input map[string]any) (Payload, error) {
	// Get document type to determine which payload to create
	docType, err := getString(input, "type")
	if err != nil {
		// Default to generic document
		docType = "document"
	}

	switch docType {
	case "document":
		return p.processDocument(input)
	case "maintenance":
		return p.processMaintenance(input)
	case "observation":
		return p.processObservation(input)
	case "sensor_doc":
		return p.processSensorDocument(input)
	default:
		// Treat unknown types as generic documents
		return p.processDocument(input)
	}
}

// processDocument creates a Document payload from raw JSON.
func (p *Processor) processDocument(input map[string]any) (*Document, error) {
	id, err := getString(input, "id")
	if err != nil {
		return nil, fmt.Errorf("missing id: %w", err)
	}

	title, err := getString(input, "title")
	if err != nil {
		return nil, fmt.Errorf("missing title: %w", err)
	}

	doc := &Document{
		ID:          id,
		Title:       title,
		Description: getStringOpt(input, "description"),
		Body:        getStringOpt(input, "body"),
		Summary:     getStringOpt(input, "summary"),
		Category:    getStringOpt(input, "category"),
		Tags:        getStringSlice(input, "tags"),
		CreatedAt:   getStringOpt(input, "created_at"),
		UpdatedAt:   getStringOpt(input, "updated_at"),
	}
	doc.EntityIDValue = MintDocumentEntityID(p.authority, doc.Category, doc.ID)

	return doc, nil
}

// processMaintenance creates a Maintenance payload from raw JSON.
func (p *Processor) processMaintenance(input map[string]any) (*Maintenance, error) {
	id, err := getString(input, "id")
	if err != nil {
		return nil, fmt.Errorf("missing id: %w", err)
	}

	title, err := getString(input, "title")
	if err != nil {
		return nil, fmt.Errorf("missing title: %w", err)
	}

	maint := &Maintenance{
		ID:             id,
		Title:          title,
		Description:    getStringOpt(input, "description"),
		Body:           getStringOpt(input, "body"),
		Technician:     getStringOpt(input, "technician"),
		Status:         getStringOpt(input, "status"),
		CompletionDate: getStringOpt(input, "completion_date"),
		Category:       getStringOpt(input, "category"),
		Tags:           getStringSlice(input, "tags"),
	}

	// Default status if not provided
	if maint.Status == "" {
		maint.Status = defaultMaintenanceStatus
	}
	maint.EntityIDValue = MintMaintenanceEntityID(p.authority, maint.Status, maint.ID)

	return maint, nil
}

// processObservation creates an Observation payload from raw JSON.
func (p *Processor) processObservation(input map[string]any) (*Observation, error) {
	id, err := getString(input, "id")
	if err != nil {
		return nil, fmt.Errorf("missing id: %w", err)
	}

	title, err := getString(input, "title")
	if err != nil {
		return nil, fmt.Errorf("missing title: %w", err)
	}

	obs := &Observation{
		ID:          id,
		Title:       title,
		Description: getStringOpt(input, "description"),
		Body:        getStringOpt(input, "body"),
		Observer:    getStringOpt(input, "observer"),
		Severity:    getStringOpt(input, "severity"),
		ObservedAt:  getStringOpt(input, "observed_at"),
		Category:    getStringOpt(input, "category"),
		Tags:        getStringSlice(input, "tags"),
	}

	// Default severity if not provided
	if obs.Severity == "" {
		obs.Severity = defaultObservationSeverity
	}
	obs.EntityIDValue = MintObservationEntityID(p.authority, obs.Severity, obs.ID)

	return obs, nil
}

// processSensorDocument creates a SensorDocument payload from raw JSON.
func (p *Processor) processSensorDocument(input map[string]any) (*SensorDocument, error) {
	id, err := getString(input, "id")
	if err != nil {
		return nil, fmt.Errorf("missing id: %w", err)
	}

	title, err := getString(input, "title")
	if err != nil {
		return nil, fmt.Errorf("missing title: %w", err)
	}

	sensorDoc := &SensorDocument{
		ID:          id,
		Title:       title,
		Description: getStringOpt(input, "description"),
		Body:        getStringOpt(input, "body"),
		Location:    getStringOpt(input, "location"),
		Reading:     getFloat64Opt(input, "reading"),
		Unit:        getStringOpt(input, "unit"),
		Category:    getStringOpt(input, "category"),
		Tags:        getStringSlice(input, "tags"),
	}
	sensorDoc.EntityIDValue = SensorMintDocumentEntityID(p.authority, sensorDoc.Category, sensorDoc.ID)

	return sensorDoc, nil
}

// Helper functions for type-safe field extraction

func getString(m map[string]any, key string) (string, error) {
	v, ok := m[key]
	if !ok {
		return "", fmt.Errorf("field %q not found", key)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("field %q is not a string: %T", key, v)
	}
	return s, nil
}

func getStringOpt(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

func getStringSlice(m map[string]any, key string) []string {
	v, ok := m[key]
	if !ok {
		return nil
	}
	slice, ok := v.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(slice))
	for _, item := range slice {
		if s, ok := item.(string); ok {
			result = append(result, s)
		}
	}
	return result
}

func getFloat64Opt(m map[string]any, key string) float64 {
	v, ok := m[key]
	if !ok {
		return 0
	}
	switch val := v.(type) {
	case float64:
		return val
	case float32:
		return float64(val)
	case int:
		return float64(val)
	case int64:
		return float64(val)
	default:
		return 0
	}
}
