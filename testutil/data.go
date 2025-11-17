package testutil

// core test data with ZERO semantic concepts.
// NO EntityID, NO MAVLink, NO robotics payloads, NO SOSA/SSN.
// Just vanilla JSON, CSV, and generic data structures.

// TestMessages contains generic JSON messages for testing (core data only).
var TestMessages = []string{
	`{"id": 1, "value": "foo", "timestamp": 1234567890, "count": 42}`,
	`{"id": 2, "value": "bar", "timestamp": 1234567891, "count": 43}`,
	`{"id": 3, "value": "baz", "timestamp": 1234567892, "count": 44}`,
	`{"id": 4, "value": "qux", "timestamp": 1234567893, "count": 45}`,
	`{"id": 5, "value": "quux", "timestamp": 1234567894, "count": 46}`,
}

// TestJSONObjects contains generic JSON objects for testing.
var TestJSONObjects = []map[string]any{
	{
		"id":        1,
		"name":      "Alice",
		"age":       30,
		"city":      "NYC",
		"timestamp": 1234567890,
	},
	{
		"id":        2,
		"name":      "Bob",
		"age":       25,
		"city":      "LA",
		"timestamp": 1234567891,
	},
	{
		"id":        3,
		"name":      "Charlie",
		"age":       35,
		"city":      "Chicago",
		"timestamp": 1234567892,
	},
}

// TestCSV is a generic CSV dataset for testing CSV parsing.
const TestCSV = `name,age,city,timestamp
Alice,30,NYC,1234567890
Bob,25,LA,1234567891
Charlie,35,Chicago,1234567892
Diana,28,Boston,1234567893
Eve,32,Seattle,1234567894`

// TestCSVWithHeaders is CSV data with headers for testing.
const TestCSVWithHeaders = `id,value,count,enabled
1,foo,42,true
2,bar,43,false
3,baz,44,true
4,qux,45,false
5,quux,46,true`

// TestPlainText contains plain text data for testing.
var TestPlainText = []string{
	"This is a test message",
	"Another test message",
	"Yet another test message",
	"One more test message",
	"Final test message",
}

// TestBinaryData contains test binary data patterns.
var TestBinaryData = [][]byte{
	{0x01, 0x02, 0x03, 0x04, 0x05},
	{0x0A, 0x0B, 0x0C, 0x0D, 0x0E},
	{0xFF, 0xFE, 0xFD, 0xFC, 0xFB},
}

// TestMetrics contains generic metric data for testing.
var TestMetrics = []map[string]any{
	{
		"metric":    "cpu_usage",
		"value":     45.2,
		"unit":      "percent",
		"timestamp": 1234567890,
	},
	{
		"metric":    "memory_usage",
		"value":     2048,
		"unit":      "MB",
		"timestamp": 1234567891,
	},
	{
		"metric":    "disk_usage",
		"value":     75.5,
		"unit":      "percent",
		"timestamp": 1234567892,
	},
}

// TestHTTPRequests contains generic HTTP request data for testing.
var TestHTTPRequests = []map[string]any{
	{
		"method":    "GET",
		"path":      "/api/v1/users",
		"status":    200,
		"timestamp": 1234567890,
	},
	{
		"method":    "POST",
		"path":      "/api/v1/users",
		"status":    201,
		"timestamp": 1234567891,
	},
	{
		"method":    "PUT",
		"path":      "/api/v1/users/123",
		"status":    200,
		"timestamp": 1234567892,
	},
	{
		"method":    "DELETE",
		"path":      "/api/v1/users/123",
		"status":    204,
		"timestamp": 1234567893,
	},
}

// TestErrors contains generic error messages for testing error handling.
var TestErrors = []string{
	"connection timeout",
	"invalid input format",
	"resource not found",
	"permission denied",
	"internal server error",
}

// TestTimestamps contains various timestamp formats for testing.
var TestTimestamps = []any{
	1234567890,              // Unix timestamp (int)
	"2024-01-15T10:30:00Z",  // RFC3339
	"2024-01-15 10:30:00",   // Common format
	"1234567890000",         // Unix milliseconds (string)
	float64(1234567890.123), // Unix with fractional seconds
}

// TestUDPPackets contains generic UDP packet data for testing.
var TestUDPPackets = [][]byte{
	[]byte("UDP packet 1: Hello World"),
	[]byte("UDP packet 2: Test Data"),
	[]byte("UDP packet 3: Sample Message"),
	[]byte("UDP packet 4: More Test Data"),
	[]byte("UDP packet 5: Final Packet"),
}

// TestWebSocketMessages contains generic WebSocket message data.
var TestWebSocketMessages = []string{
	`{"type":"ping","timestamp":1234567890}`,
	`{"type":"data","value":"test"}`,
	`{"type":"status","code":200}`,
	`{"type":"error","message":"test error"}`,
	`{"type":"close","reason":"done"}`,
}

// TestBufferData contains data of various sizes for buffer testing.
var TestBufferData = map[string][]byte{
	"small":  []byte("small data"),
	"medium": []byte(string(make([]byte, 1024))),  // 1KB
	"large":  []byte(string(make([]byte, 10240))), // 10KB
}

// GenericMessage is a core generic message structure (no domain concepts).
type GenericMessage struct {
	ID        int            `json:"id"`
	Type      string         `json:"type"`
	Value     string         `json:"value"`
	Timestamp int64          `json:"timestamp"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// NewGenericMessage creates a new generic test message.
func NewGenericMessage(id int, msgType, value string, timestamp int64) *GenericMessage {
	return &GenericMessage{
		ID:        id,
		Type:      msgType,
		Value:     value,
		Timestamp: timestamp,
		Metadata:  make(map[string]any),
	}
}

// GenericEvent is a core generic event structure for testing.
type GenericEvent struct {
	EventID   string         `json:"event_id"`
	EventType string         `json:"event_type"`
	Data      map[string]any `json:"data"`
	Timestamp int64          `json:"timestamp"`
}

// NewGenericEvent creates a new generic test event.
func NewGenericEvent(id, eventType string, timestamp int64) *GenericEvent {
	return &GenericEvent{
		EventID:   id,
		EventType: eventType,
		Data:      make(map[string]any),
		Timestamp: timestamp,
	}
}
