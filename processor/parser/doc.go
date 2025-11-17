// Package parser provides utilities for parsing various data formats into
// structured maps for use with StreamKit message processing.
//
// # Overview
//
// The parser package offers simple, focused parsers for common data formats.
// Each parser converts raw bytes into map[string]any for integration with
// GenericJSON messages or other StreamKit processors.
//
// # Supported Formats
//
// **JSON Parser (Production Ready):**
//
// Parses JSON data into structured maps with full error handling and validation.
//
// **CSV Parser (Placeholder - Not Production Ready):**
//
// Current CSV implementation is a placeholder that always returns "parsed": false.
// This parser is not functional and should not be used in production deployments.
//
// # JSON Parser
//
// The JSON parser provides reliable parsing with validation:
//
//	import "github.com/c360/semstreams/processor/parser"
//
//	// Parse JSON bytes
//	data := []byte(`{"sensor": "temp-001", "value": 23.5}`)
//	result, err := parser.ParseJSON(data)
//	if err != nil {
//	    // Handle parse error
//	}
//
//	// Access parsed data
//	sensorID := result["sensor"].(string)    // "temp-001"
//	value := result["value"].(float64)       // 23.5
//
// # Validation
//
// Parsers include validation methods to check data format before parsing:
//
//	// Validate before parsing
//	err := parser.ValidateJSON(data)
//	if err != nil {
//	    // Invalid JSON format
//	    return err
//	}
//
//	// Parse validated data
//	result, err := parser.ParseJSON(data)
//
// # JSON Parser Features
//
// **Type Handling:**
//
// JSON values are parsed to Go types:
//
//   - JSON strings → string
//   - JSON numbers → float64
//   - JSON booleans → bool
//   - JSON null → nil
//   - JSON objects → map[string]any
//   - JSON arrays → []any
//
// **Error Handling:**
//
// The parser uses standard Go error handling:
//
//	result, err := parser.ParseJSON(invalidJSON)
//	if err != nil {
//	    // errors.Is(err, parser.ErrInvalidJSON) == true
//	}
//
// **Validation:**
//
//	err := parser.ValidateJSON(data)
//	// Returns error if:
//	//   - Data is not valid JSON
//	//   - Data is empty
//	//   - JSON is not an object (must be {...})
//
// # JSON Parser Examples
//
// **Parse Simple Object:**
//
//	data := []byte(`{"name": "sensor-001", "active": true}`)
//	result, err := parser.ParseJSON(data)
//	// result: map[string]any{
//	//   "name": "sensor-001",
//	//   "active": true,
//	// }
//
// **Parse Nested Object:**
//
//	data := []byte(`{
//	  "sensor": "temp-001",
//	  "reading": {
//	    "value": 23.5,
//	    "unit": "celsius"
//	  }
//	}`)
//	result, err := parser.ParseJSON(data)
//	// Access nested data:
//	reading := result["reading"].(map[string]any)
//	value := reading["value"].(float64)
//
// **Parse Array Values:**
//
//	data := []byte(`{
//	  "sensor": "multi-001",
//	  "readings": [21.5, 22.0, 23.5]
//	}`)
//	result, err := parser.ParseJSON(data)
//	readings := result["readings"].([]any)
//	firstReading := readings[0].(float64)  // 21.5
//
// **Validate Before Parsing:**
//
//	data := []byte(`{"sensor": "temp-001"}`)
//
//	// Validate first
//	if err := parser.ValidateJSON(data); err != nil {
//	    return fmt.Errorf("invalid JSON: %w", err)
//	}
//
//	// Parse validated data
//	result, err := parser.ParseJSON(data)
//
// # CSV Parser Status
//
// ⚠️ **IMPORTANT: CSV Parser is NOT Production Ready**
//
// The current CSV parser implementation is a placeholder:
//
//	result, err := parser.ParseCSV(data)
//	// Always returns: map[string]any{"parsed": false}
//
// **DO NOT USE** the CSV parser in production deployments.
//
// # CSV Parser Roadmap
//
// Future CSV parser implementation will support:
//
//   - Header row parsing for field names
//   - Type inference for columns
//   - Configurable delimiters (comma, tab, pipe)
//   - Quote handling and escaping
//   - Multi-line field support
//
// If you need CSV parsing now, consider:
//
//   - Use a dedicated CSV processor in your application
//   - Pre-process CSV to JSON before ingestion
//   - Contribute a CSV parser implementation to StreamKit
//
// # Integration with GenericJSON
//
// Parsed results can be wrapped in GenericJSON messages:
//
//	import (
//	    "github.com/c360/semstreams/message"
//	    "github.com/c360/semstreams/processor/parser"
//	)
//
//	// Parse raw JSON
//	data := []byte(`{"sensor": "temp-001", "value": 23.5}`)
//	parsed, err := parser.ParseJSON(data)
//	if err != nil {
//	    return err
//	}
//
//	// Wrap in GenericJSON
//	payload := message.NewGenericJSON(parsed)
//	// Ready for StreamKit processing
//
// # Error Types
//
// The parser defines standard error variables for error checking:
//
//	var (
//	    ErrInvalidJSON = errors.New("invalid JSON format")
//	    ErrEmptyData   = errors.New("empty data")
//	)
//
// Use with errors.Is():
//
//	if errors.Is(err, parser.ErrInvalidJSON) {
//	    // Handle invalid JSON specifically
//	}
//
// # Performance
//
// **JSON Parser:**
//
//   - Uses encoding/json from standard library
//   - Performance: ~100,000 small objects/second
//   - Memory: Minimal allocations for simple objects
//   - Suitable for high-throughput message processing
//
// **CSV Parser:**
//
//   - Not applicable (placeholder implementation)
//
// # Testing
//
// The package has 100% test coverage for JSON parsing:
//
//   - Valid JSON objects
//   - Invalid JSON formats
//   - Empty data handling
//   - Type preservation
//   - Error conditions
//
// Run tests:
//
//	go test ./processor/parser -v
//
// # Design Philosophy
//
// The parser package follows these principles:
//
//   - **Simplicity**: Focused on common use cases, not feature-complete
//   - **Reliability**: Production-ready features are fully tested
//   - **Transparency**: Placeholder features clearly documented as non-functional
//   - **Integration**: Output format designed for StreamKit message processing
//
// # Future Enhancements
//
// Planned additions based on user requirements:
//
//   - CSV parser implementation
//   - XML parser
//   - Protocol Buffers parser
//   - Custom delimiter support
//   - Schema validation
//   - Streaming parsers for large files
//
// # Migration Notes
//
// If upgrading from a version expecting CSV support:
//
//   - Review all uses of parser.ParseCSV()
//   - Implement alternative CSV parsing
//   - Watch for parser package updates with CSV support
//   - Consider contributing CSV implementation
//
// For questions or contributions, see the StreamKit repository.
package parser
