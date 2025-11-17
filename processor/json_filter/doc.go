// Package jsonfilter provides a processor for filtering GenericJSON messages
// based on field values and comparison rules.
//
// # Overview
//
// The JSON filter processor enables field-based filtering of GenericJSON payloads
// using flexible comparison operators. It subscribes to NATS subjects carrying
// GenericJSON messages (core .json.v1 interface), evaluates filter rules against
// the message data, and publishes matching messages to output subjects.
//
// # Supported Operators
//
// The processor supports six comparison operators:
//
//   - eq (equals): String or numeric equality
//   - ne (not equals): String or numeric inequality
//   - gt (greater than): Numeric comparison (field > value)
//   - gte (greater than or equal): Numeric comparison (field >= value)
//   - lt (less than): Numeric comparison (field < value)
//   - lte (less than or equal): Numeric comparison (field <= value)
//   - contains: Substring matching (case-sensitive)
//
// # Quick Start
//
// Filter messages where altitude exceeds 1000 meters:
//
//	config := jsonfilter.JSONFilterConfig{
//	    Ports: &component.PortConfig{
//	        Inputs: []component.PortDefinition{
//	            {Name: "input", Type: "nats", Subject: "sensor.data", Interface: "core .json.v1"},
//	        },
//	        Outputs: []component.PortDefinition{
//	            {Name: "output", Type: "nats", Subject: "sensor.high_altitude", Interface: "core .json.v1"},
//	        },
//	    },
//	    Rules: []jsonfilter.FilterRule{
//	        {Field: "altitude", Operator: "gt", Value: 1000},
//	    },
//	}
//
//	rawConfig, _ := json.Marshal(config)
//	processor, err := jsonfilter.NewJSONFilterProcessor(rawConfig, deps)
//
// # Filter Rules
//
// Rules are evaluated as logical AND - all rules must match for a message to pass.
//
// String equality example:
//
//	{Field: "status", Operator: "eq", Value: "active"}
//
// Numeric comparison example:
//
//	{Field: "temperature", Operator: "gte", Value: 20.5}
//
// Substring matching example:
//
//	{Field: "message", Operator: "contains", Value: "error"}
//
// # Multiple Rules
//
// Configure multiple rules for complex filtering:
//
//	Rules: []jsonfilter.FilterRule{
//	    {Field: "status", Operator: "eq", Value: "active"},
//	    {Field: "priority", Operator: "gte", Value: 5},
//	    {Field: "region", Operator: "contains", Value: "north"},
//	}
//
// All three rules must match for the message to be published.
//
// # Message Flow
//
//	Input Subject → GenericJSON → Filter Rules → Matching Messages → Output Subject
//	                  ↓
//	           Non-matching messages dropped (logged at Debug level)
//
// # GenericJSON Interface
//
// Input messages must conform to the core .json.v1 interface:
//
//	type GenericJSONPayload struct {
//	    Data map[string]any `json:"data"`
//	}
//
// Example input message:
//
//	{
//	  "data": {
//	    "sensor_id": "temp-001",
//	    "value": 23.5,
//	    "unit": "celsius",
//	    "location": "warehouse-a"
//	  }
//	}
//
// Filter rule: {Field: "value", Operator: "gt", Value: 20}
// Result: Message passes (23.5 > 20)
//
// # Type Handling
//
// Field values are converted to appropriate types for comparison:
//
//   - Numeric operators (gt, gte, lt, lte): Converts to float64
//   - String operators (eq, ne, contains): Converts to string
//
// If type conversion fails, the rule does not match (message is dropped).
//
// # Configuration Schema
//
// The processor uses component.PortConfig for flexible input/output configuration:
//
//	{
//	  "ports": {
//	    "inputs": [
//	      {"name": "input", "type": "nats", "subject": "raw.>", "interface": "core .json.v1"}
//	    ],
//	    "outputs": [
//	      {"name": "output", "type": "nats", "subject": "filtered.messages", "interface": "core .json.v1"}
//	    ]
//	  },
//	  "rules": [
//	    {"field": "value", "operator": "gt", "value": 100}
//	  ]
//	}
//
// # Error Handling
//
// The processor uses semstreams/errors for consistent error classification:
//
//   - Invalid config: errors.WrapInvalid (bad configuration, missing fields)
//   - NATS errors: errors.WrapTransient (network issues, retryable)
//   - Unmarshal errors: errors.WrapInvalid (malformed JSON payloads)
//
// Messages that fail parsing or filtering are logged at Debug level and dropped.
//
// # Performance Considerations
//
//   - Filter evaluation is O(n) where n is number of rules
//   - Field extraction is O(1) map lookup
//   - Type conversion overhead is minimal for primitive types
//   - Concurrent message processing via goroutines
//
// Typical throughput: 10,000+ messages/second per processor instance.
//
// # Observability
//
// The processor implements component.Discoverable for monitoring:
//
//	meta := processor.Meta()
//	// Name: json-filter-processor
//	// Type: processor
//	// Description: Filters GenericJSON messages (core .json.v1) based on field rules
//
//	dataFlow := processor.DataFlow()
//	// MessagesProcessed: Total messages received
//	// MessagesFiltered: Messages that passed all rules
//	// ErrorsTotal: Parse errors + filter evaluation errors
//
// # Integration Example
//
// Complete flow with sensor data filtering:
//
//	// Sensor publishes to "sensors.raw"
//	sensor → NATS("sensors.raw") → JSONFilterProcessor → NATS("sensors.high_temp")
//
//	// Configuration
//	config := jsonfilter.JSONFilterConfig{
//	    Ports: &component.PortConfig{
//	        Inputs: []component.PortDefinition{
//	            {Name: "input", Type: "nats", Subject: "sensors.raw", Interface: "core .json.v1"},
//	        },
//	        Outputs: []component.PortDefinition{
//	            {Name: "output", Type: "nats", Subject: "sensors.high_temp", Interface: "core .json.v1"},
//	        },
//	    },
//	    Rules: []jsonfilter.FilterRule{
//	        {Field: "temperature", Operator: "gte", Value: 30},
//	        {Field: "sensor_type", Operator: "eq", Value: "thermocouple"},
//	    },
//	}
//
// Only thermocouples reading >= 30°C will be forwarded to "sensors.high_temp".
//
// # Limitations
//
// Current version limitations:
//
//   - No support for nested field access (e.g., "position.lat")
//   - No logical OR between rules (all rules are AND)
//   - No regular expression matching (only exact/substring matching)
//   - No custom comparison functions
//
// These may be addressed in future versions based on user requirements.
//
// # Testing
//
// The package includes comprehensive test coverage:
//
//   - Unit tests: Rule matching logic, operator behavior
//   - Integration tests: End-to-end NATS message flows with testcontainers
//
// Run tests:
//
//	go test ./processor/json_filter -v              # Unit tests
//	INTEGRATION_TESTS=1 go test ./processor/json_filter -v  # Integration tests
package jsonfilter
