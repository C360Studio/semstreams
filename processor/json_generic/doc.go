// Package jsongeneric provides a processor for wrapping plain JSON into
// GenericJSON (core .json.v1) format for integration with StreamKit pipelines.
//
// # Overview
//
// The JSON generic processor acts as an ingestion adapter, converting external
// JSON data into the StreamKit GenericJSON message format. It subscribes to NATS
// subjects carrying plain JSON, wraps the data in GenericJSONPayload structure,
// and publishes to output subjects for downstream processing.
//
// # Purpose
//
// Use the JSON generic processor when:
//
//   - Ingesting data from external systems that emit plain JSON
//   - Converting raw JSON to GenericJSON for use with json_filter or json_map
//   - Normalizing heterogeneous JSON sources into a standard format
//   - Adding GenericJSON interface compatibility to legacy data sources
//
// # Quick Start
//
// Wrap plain JSON sensor data into GenericJSON format:
//
//	config := jsongeneric.JSONGenericConfig{
//	    Ports: &component.PortConfig{
//	        Inputs: []component.PortDefinition{
//	            {Name: "input", Type: "nats", Subject: "external.sensors.>", Required: true},
//	        },
//	        Outputs: []component.PortDefinition{
//	            {Name: "output", Type: "nats", Subject: "internal.sensors",
//	             Interface: "core .json.v1", Required: true},
//	        },
//	    },
//	}
//
//	rawConfig, _ := json.Marshal(config)
//	processor, err := jsongeneric.NewJSONGenericProcessor(rawConfig, deps)
//
// # Message Transformation
//
// **Input (Plain JSON):**
//
//	{
//	  "sensor_id": "temp-001",
//	  "value": 23.5,
//	  "unit": "celsius",
//	  "timestamp": 1234567890
//	}
//
// **Output (GenericJSON):**
//
//	{
//	  "data": {
//	    "sensor_id": "temp-001",
//	    "value": 23.5,
//	    "unit": "celsius",
//	    "timestamp": 1234567890
//	  }
//	}
//
// The processor wraps the original JSON object in a "data" field, conforming to
// the GenericJSONPayload structure required by the core .json.v1 interface.
//
// # Message Flow
//
//	External System → Plain JSON → NATS(raw.>) → JSONGenericProcessor →
//	                  GenericJSON → NATS(generic.messages) → Downstream Processors
//
// # GenericJSON Interface
//
// Output messages conform to core .json.v1:
//
//	type GenericJSONPayload struct {
//	    Data map[string]any `json:"data"`
//	}
//
// This enables integration with other StreamKit processors:
//
//   - json_filter: Filter wrapped messages by field values
//   - json_map: Transform wrapped message fields
//   - Custom processors: Process standardized GenericJSON format
//
// # Configuration Schema
//
//	{
//	  "ports": {
//	    "inputs": [
//	      {
//	        "name": "nats_input",
//	        "type": "nats",
//	        "subject": "raw.>",
//	        "required": true,
//	        "description": "NATS subjects with plain JSON data"
//	      }
//	    ],
//	    "outputs": [
//	      {
//	        "name": "nats_output",
//	        "type": "nats",
//	        "subject": "generic.messages",
//	        "interface": "core .json.v1",
//	        "required": true,
//	        "description": "NATS subject for GenericJSON wrapped messages"
//	      }
//	    ]
//	  }
//	}
//
// # Error Handling
//
// The processor uses streamkit/errors for consistent error classification:
//
//   - Invalid config: errors.WrapInvalid (bad configuration)
//   - NATS errors: errors.WrapTransient (network issues, retryable)
//   - Unmarshal errors: errors.WrapInvalid (malformed JSON input)
//
// **Invalid JSON Handling:**
//
// Messages that fail JSON parsing are logged at Debug level and dropped:
//
//	// Invalid JSON input
//	{this is not valid json}
//
//	// Logged: "Failed to parse message as JSON"
//	// Action: Message dropped, error counter incremented
//	// Impact: No output published, processing continues
//
// This prevents downstream processors from receiving malformed data while
// maintaining system resilience.
//
// # Complete Integration Example
//
// **Scenario:** External weather API publishes plain JSON to NATS, StreamKit
// pipeline filters for high temperatures.
//
//	// External API publishes plain JSON
//	weatherAPI → NATS("weather.raw")
//
//	// JSONGenericProcessor wraps to GenericJSON
//	raw.weather → JSONGenericProcessor → generic.weather (core .json.v1)
//
//	// JSONFilterProcessor filters high temperatures
//	generic.weather → JSONFilterProcessor → weather.high_temp
//
//	// Configuration: JSONGenericProcessor
//	{
//	  "ports": {
//	    "inputs": [{"subject": "weather.raw"}],
//	    "outputs": [{"subject": "generic.weather", "interface": "core .json.v1"}]
//	  }
//	}
//
//	// Configuration: JSONFilterProcessor
//	{
//	  "ports": {
//	    "inputs": [{"subject": "generic.weather", "interface": "core .json.v1"}],
//	    "outputs": [{"subject": "weather.high_temp", "interface": "core .json.v1"}]
//	  },
//	  "rules": [{"field": "temperature", "operator": "gt", "value": 30}]
//	}
//
// # Performance Considerations
//
//   - Wrapping: O(1) - Single map allocation per message
//   - Validation: O(n) - Validates payload structure (minimal overhead)
//   - Marshaling: O(n) - JSON serialization of wrapped payload
//
// Typical throughput: 15,000+ messages/second per processor instance.
//
// # Observability
//
// The processor implements component.Discoverable for monitoring:
//
//	meta := processor.Meta()
//	// Name: json-generic-processor
//	// Type: processor
//	// Description: Wraps plain JSON into GenericJSON (core .json.v1) format
//
//	dataFlow := processor.DataFlow()
//	// MessagesProcessed: Total messages received (valid + invalid JSON)
//	// MessagesWrapped: Successfully wrapped messages
//	// ErrorsTotal: JSON parse errors + NATS publish errors
//
// Metrics help identify:
//
//   - Input data quality (ErrorsTotal / MessagesProcessed)
//   - Processing rate (MessagesWrapped / time)
//   - System health (NATS publish errors)
//
// # Use Cases
//
// **External API Integration:**
//
//	// Ingest third-party JSON APIs
//	external.api.weather → JSONGenericProcessor → internal.weather (GenericJSON)
//
// **Legacy System Migration:**
//
//	// Wrap legacy JSON formats
//	legacy.system.data → JSONGenericProcessor → modern.pipeline (GenericJSON)
//
// **Data Normalization:**
//
//	// Standardize multiple JSON sources
//	source.a.data → JSONGenericProcessor ┐
//	source.b.data → JSONGenericProcessor ├→ unified.data (GenericJSON)
//	source.c.data → JSONGenericProcessor ┘
//
// **Pipeline Entry Point:**
//
//	// Convert raw JSON to pipeline-compatible format
//	raw.input → JSONGenericProcessor → validated.input → FilterProcessor → MapProcessor
//
// # Comparison with Other Processors
//
// **JSONGenericProcessor vs JSONFilterProcessor:**
//
//   - JSONGenericProcessor: Wraps plain JSON → GenericJSON (no filtering)
//   - JSONFilterProcessor: Filters GenericJSON → GenericJSON (no wrapping)
//
// **JSONGenericProcessor vs JSONMapProcessor:**
//
//   - JSONGenericProcessor: Wraps plain JSON → GenericJSON (no transformation)
//   - JSONMapProcessor: Transforms GenericJSON → GenericJSON (no wrapping)
//
// **When to use JSONGenericProcessor:**
//
// Use when input is plain JSON that needs GenericJSON wrapping.
// Do not use when input is already GenericJSON format.
//
// # Limitations
//
// Current version limitations:
//
//   - No schema validation of input JSON
//   - No custom wrapping structure (always uses "data" field)
//   - No metadata injection (timestamps, source tags, etc.)
//   - Invalid JSON messages are dropped (no DLQ/retry)
//
// These may be addressed in future versions based on user requirements.
//
// # Testing
//
// The package includes test coverage:
//
//   - Unit tests: Creation, configuration, port handling, metadata
//   - Integration tests: TBD (end-to-end NATS message flows)
//
// Run tests:
//
//	go test ./processor/json_generic -v              # Unit tests
//	INTEGRATION_TESTS=1 go test ./processor/json_generic -v  # Integration tests (when available)
//
// # Design Decisions
//
// **Why separate JSONGenericProcessor from parsers:**
//
//   - Parser package is stateless utility functions
//   - JSONGenericProcessor is stateful component with lifecycle management
//   - Separation enables parser reuse in other contexts
//
// **Why drop invalid JSON instead of error:**
//
//   - Resilience: One bad message shouldn't stop processing
//   - Observability: Errors are counted and logged
//   - Simplicity: No complex error recovery needed
//
// **Why no schema validation:**
//
//   - Performance: Validation adds overhead
//   - Flexibility: Accepts any valid JSON structure
//   - Downstream: Schema validation can be added in pipeline
//
// For questions or contributions, see the StreamKit repository.
package jsongeneric
