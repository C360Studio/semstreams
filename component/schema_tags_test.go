package component

import (
	"bytes"
	"encoding/json"
	stderrors "errors"
	"reflect"
	"testing"

	"github.com/c360studio/semstreams/pkg/errs"
)

func TestParseSchemaTag(t *testing.T) {
	tests := []struct {
		name    string
		tag     string
		want    SchemaDirectives
		wantErr bool
	}{
		{
			name: "simple string field",
			tag:  "type:string,description:Component name,category:basic",
			want: SchemaDirectives{
				Type:        "string",
				Description: "Component name",
				Category:    "basic",
			},
			wantErr: false,
		},
		{
			name: "int field with constraints",
			tag:  "type:int,description:Listen port,min:1,max:65535,default:8080",
			want: SchemaDirectives{
				Type:        "int",
				Description: "Listen port",
				Default:     "8080",
				Min:         intPtr(1),
				Max:         intPtr(65535),
			},
			wantErr: false,
		},
		{
			name: "enum field",
			tag:  "type:enum,description:Log level,enum:debug|info|warn|error,default:info",
			want: SchemaDirectives{
				Type:        "enum",
				Description: "Log level",
				Default:     "info",
				Enum:        []string{"debug", "info", "warn", "error"},
			},
			wantErr: false,
		},
		{
			name: "array field with default",
			tag:  "type:array,description:Enabled rules,default:battery_monitor",
			want: SchemaDirectives{
				Type:        "array",
				Description: "Enabled rules",
				Default:     "battery_monitor",
			},
			wantErr: false,
		},
		{
			name: "bool field",
			tag:  "type:bool,description:Enable feature,default:true",
			want: SchemaDirectives{
				Type:        "bool",
				Description: "Enable feature",
				Default:     "true",
			},
			wantErr: false,
		},
		{
			name: "readonly field",
			tag:  "readonly,type:string,description:Port identifier",
			want: SchemaDirectives{
				Type:        "string",
				Description: "Port identifier",
				ReadOnly:    true,
			},
			wantErr: false,
		},
		{
			name: "editable field",
			tag:  "editable,type:string,description:NATS subject pattern",
			want: SchemaDirectives{
				Type:        "string",
				Description: "NATS subject pattern",
				Editable:    true,
			},
			wantErr: false,
		},
		{
			name: "hidden field",
			tag:  "hidden,type:bool,description:Internal flag",
			want: SchemaDirectives{
				Type:        "bool",
				Description: "Internal flag",
				Hidden:      true,
			},
			wantErr: false,
		},
		{
			name: "required field",
			tag:  "required,type:string,description:API key",
			want: SchemaDirectives{
				Type:        "string",
				Description: "API key",
				Required:    true,
			},
			wantErr: false,
		},
		{
			name: "float field",
			tag:  "type:float,description:Timeout,min:0,max:30,default:5.5",
			want: SchemaDirectives{
				Type:        "float",
				Description: "Timeout",
				Default:     "5.5",
				Min:         intPtr(0),
				Max:         intPtr(30),
			},
			wantErr: false,
		},
		{
			name: "object field",
			tag:  "type:object,description:Cache configuration,category:advanced",
			want: SchemaDirectives{
				Type:        "object",
				Description: "Cache configuration",
				Category:    "advanced",
			},
			wantErr: false,
		},
		{
			name: "ports field",
			tag:  "type:ports,description:Port configuration,category:basic",
			want: SchemaDirectives{
				Type:        "ports",
				Description: "Port configuration",
				Category:    "basic",
			},
			wantErr: false,
		},
		{
			name: "enum with spaces",
			tag:  "type:enum,description:Level,enum: debug | info | warn ",
			want: SchemaDirectives{
				Type:        "enum",
				Description: "Level",
				Enum:        []string{"debug", "info", "warn"},
			},
			wantErr: false,
		},
		{
			name: "multiple boolean flags",
			tag:  "required,readonly,type:string,description:Fixed value",
			want: SchemaDirectives{
				Type:        "string",
				Description: "Fixed value",
				Required:    true,
				ReadOnly:    true,
			},
			wantErr: false,
		},
		{
			name: "future extensions",
			tag:  "type:string,description:Email,help:https://example.com,placeholder:Enter email,pattern:^[^@]+@,format:email",
			want: SchemaDirectives{
				Type:        "string",
				Description: "Email",
				Help:        "https://example.com",
				Placeholder: "Enter email",
				Pattern:     "^[^@]+@",
				Format:      "email",
			},
			wantErr: false,
		},
		// Error cases
		{
			name:    "empty tag",
			tag:     "",
			wantErr: true,
		},
		{
			name:    "missing type",
			tag:     "description:Some field",
			wantErr: true,
		},
		{
			name:    "invalid type",
			tag:     "type:invalid,description:Field",
			wantErr: true,
		},
		{
			name:    "invalid category",
			tag:     "type:string,description:Field,category:invalid",
			wantErr: true,
		},
		{
			name:    "invalid min",
			tag:     "type:int,description:Port,min:abc",
			wantErr: true,
		},
		{
			name:    "invalid max",
			tag:     "type:int,description:Port,max:xyz",
			wantErr: true,
		},
		{
			name:    "unknown boolean flag",
			tag:     "type:string,description:Field,unknownflag",
			wantErr: true,
		},
		{
			name:    "unknown directive",
			tag:     "type:string,description:Field,unknown:value",
			wantErr: true,
		},
		{
			name:    "malformed directive",
			tag:     "type:string,description:Field,invalid",
			wantErr: true,
		},
		{
			name:    "empty value",
			tag:     "type:,description:Field",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseSchemaTag(tt.tag)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseSchemaTag() expected error, got nil")
				}
				// Verify error is properly wrapped as ClassifiedError with Invalid class
				var classifiedErr *errs.ClassifiedError
				if !stderrors.As(err, &classifiedErr) {
					t.Errorf("ParseSchemaTag() error should be ClassifiedError, got %T", err)
				} else if classifiedErr.Class != errs.ErrorInvalid {
					t.Errorf("ParseSchemaTag() error class = %v, want ErrorInvalid", classifiedErr.Class)
				}
				return
			}
			if err != nil {
				t.Errorf("ParseSchemaTag() unexpected error = %v", err)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseSchemaTag() got = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestConvertDefault(t *testing.T) {
	tests := []struct {
		name      string
		value     any
		fieldType string
		want      any
	}{
		{
			name:      "string value",
			value:     "hello",
			fieldType: "string",
			want:      "hello",
		},
		{
			name:      "int value",
			value:     "8080",
			fieldType: "int",
			want:      8080,
		},
		{
			name:      "bool true",
			value:     "true",
			fieldType: "bool",
			want:      true,
		},
		{
			name:      "bool false",
			value:     "false",
			fieldType: "bool",
			want:      false,
		},
		{
			name:      "float value",
			value:     "3.14",
			fieldType: "float",
			want:      3.14,
		},
		{
			name:      "enum value",
			value:     "info",
			fieldType: "enum",
			want:      "info",
		},
		{
			name:      "array value",
			value:     "battery_monitor",
			fieldType: "array",
			want:      []string{"battery_monitor"},
		},
		{
			name:      "empty array",
			value:     "",
			fieldType: "array",
			want:      []string{},
		},
		{
			name:      "object returns nil",
			value:     "{}",
			fieldType: "object",
			want:      nil,
		},
		{
			name:      "ports returns nil",
			value:     "{}",
			fieldType: "ports",
			want:      nil,
		},
		{
			name:      "nil value",
			value:     nil,
			fieldType: "string",
			want:      nil,
		},
		{
			name:      "invalid int",
			value:     "abc",
			fieldType: "int",
			want:      nil,
		},
		{
			name:      "invalid bool",
			value:     "maybe",
			fieldType: "bool",
			want:      nil,
		},
		{
			name:      "invalid float",
			value:     "not-a-number",
			fieldType: "float",
			want:      nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := convertDefault(tt.value, tt.fieldType)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("convertDefault() = %v (%T), want %v (%T)", got, got, tt.want, tt.want)
			}
		})
	}
}

// testConfigType returns the test config struct type for schema generation tests
func testConfigType() reflect.Type {
	type TestConfig struct {
		// Basic fields
		Name    string `json:"name"    schema:"type:string,description:Component name,category:basic"`
		Port    int    `json:"port"    schema:"type:int,description:Listen port,min:1,max:65535,default:8080,category:basic"`
		Enabled bool   `json:"enabled" schema:"type:bool,description:Enable feature,default:true"`

		// Advanced fields
		Timeout  string `json:"timeout"   schema:"type:string,description:Timeout duration,default:30s,category:advanced"`
		LogLevel string `json:"log_level" schema:"type:enum,description:Log level,enum:debug|info|warn|error,default:info,category:advanced"`

		// Required field
		APIKey string `json:"api_key" schema:"required,type:string,description:API key for authentication"`

		// Array field
		Rules []string `json:"rules" schema:"type:array,description:Enabled rules,default:rule1"`

		// Object field
		Cache struct{} `json:"cache" schema:"type:object,description:Cache configuration"`

		// Ports field
		Ports *PortConfig `json:"ports" schema:"type:ports,description:Port configuration,category:basic"`

		// No schema tag - should be skipped
		Internal string `json:"internal"`

		// No json tag - should be skipped
		Unexported string `schema:"type:string,description:Not exported"`

		// json:"-" - should be skipped
		Ignored string `json:"-" schema:"type:string,description:Ignored field"`
	}
	return reflect.TypeOf(TestConfig{})
}

// verifyFieldPresence checks that expected fields exist in schema
func verifyFieldPresence(t *testing.T, schema ConfigSchema, expectedFields []string) {
	for _, fieldName := range expectedFields {
		if _, exists := schema.Properties[fieldName]; !exists {
			t.Errorf("Expected field %s not found in schema", fieldName)
		}
	}
}

// verifyFieldAbsence checks that skipped fields don't exist in schema
func verifyFieldAbsence(t *testing.T, schema ConfigSchema, skippedFields []string) {
	for _, fieldName := range skippedFields {
		if _, exists := schema.Properties[fieldName]; exists {
			t.Errorf("Field %s should have been skipped", fieldName)
		}
	}
}

// verifyStringField verifies a string field's properties
func verifyStringField(t *testing.T, schema ConfigSchema, fieldName, expectedDesc, expectedCategory string) {
	if prop, exists := schema.Properties[fieldName]; exists {
		if prop.Type != "string" {
			t.Errorf("%s.Type = %s, want string", fieldName, prop.Type)
		}
		if prop.Description != expectedDesc {
			t.Errorf("%s.Description = %s, want '%s'", fieldName, prop.Description, expectedDesc)
		}
		if prop.Category != expectedCategory {
			t.Errorf("%s.Category = %s, want %s", fieldName, prop.Category, expectedCategory)
		}
	}
}

// verifyIntFieldWithConstraints verifies an int field with min/max constraints
func verifyIntFieldWithConstraints(t *testing.T, schema ConfigSchema, fieldName string, expectedDefault, expectedMin, expectedMax int) {
	if prop, exists := schema.Properties[fieldName]; exists {
		if prop.Type != "int" {
			t.Errorf("%s.Type = %s, want int", fieldName, prop.Type)
		}
		if prop.Default != expectedDefault {
			t.Errorf("%s.Default = %v, want %d", fieldName, prop.Default, expectedDefault)
		}
		if prop.Minimum == nil || *prop.Minimum != expectedMin {
			t.Errorf("%s.Minimum = %v, want %d", fieldName, prop.Minimum, expectedMin)
		}
		if prop.Maximum == nil || *prop.Maximum != expectedMax {
			t.Errorf("%s.Maximum = %v, want %d", fieldName, prop.Maximum, expectedMax)
		}
	}
}

// verifyBoolField verifies a bool field's properties
func verifyBoolField(t *testing.T, schema ConfigSchema, fieldName string, expectedDefault bool) {
	if prop, exists := schema.Properties[fieldName]; exists {
		if prop.Type != "bool" {
			t.Errorf("%s.Type = %s, want bool", fieldName, prop.Type)
		}
		if prop.Default != expectedDefault {
			t.Errorf("%s.Default = %v, want %v", fieldName, prop.Default, expectedDefault)
		}
	}
}

// verifyEnumField verifies an enum field's properties
func verifyEnumField(t *testing.T, schema ConfigSchema, fieldName string, expectedEnum []string, expectedDefault string) {
	if prop, exists := schema.Properties[fieldName]; exists {
		if prop.Type != "enum" {
			t.Errorf("%s.Type = %s, want enum", fieldName, prop.Type)
		}
		if !reflect.DeepEqual(prop.Enum, expectedEnum) {
			t.Errorf("%s.Enum = %v, want %v", fieldName, prop.Enum, expectedEnum)
		}
		if prop.Default != expectedDefault {
			t.Errorf("%s.Default = %v, want %s", fieldName, prop.Default, expectedDefault)
		}
	}
}

// verifyArrayField verifies an array field's properties
func verifyArrayField(t *testing.T, schema ConfigSchema, fieldName string, expectedDefault []string) {
	if prop, exists := schema.Properties[fieldName]; exists {
		if prop.Type != "array" {
			t.Errorf("%s.Type = %s, want array", fieldName, prop.Type)
		}
		if !reflect.DeepEqual(prop.Default, expectedDefault) {
			t.Errorf("%s.Default = %v, want %v", fieldName, prop.Default, expectedDefault)
		}
	}
}

// verifyPortsField verifies a ports field's properties
func verifyPortsField(t *testing.T, schema ConfigSchema, fieldName string) {
	if prop, exists := schema.Properties[fieldName]; exists {
		if prop.Type != "ports" {
			t.Errorf("%s.Type = %s, want ports", fieldName, prop.Type)
		}
		if prop.PortFields == nil {
			t.Errorf("%s.PortFields should not be nil", fieldName)
		}
		if len(prop.PortFields) == 0 {
			t.Errorf("%s.PortFields should not be empty", fieldName)
		}
	}
}

func TestGenerateConfigSchema(t *testing.T) {
	schema := GenerateConfigSchema(testConfigType())

	// Verify field presence/absence
	expectedFields := []string{"name", "port", "enabled", "timeout", "log_level", "api_key", "rules", "cache", "ports"}
	verifyFieldPresence(t, schema, expectedFields)

	skippedFields := []string{"internal", "Unexported", "Ignored"}
	verifyFieldAbsence(t, schema, skippedFields)

	// Verify individual fields
	verifyStringField(t, schema, "name", "Component name", "basic")
	verifyIntFieldWithConstraints(t, schema, "port", 8080, 1, 65535)
	verifyBoolField(t, schema, "enabled", true)
	verifyEnumField(t, schema, "log_level", []string{"debug", "info", "warn", "error"}, "info")
	verifyArrayField(t, schema, "rules", []string{"rule1"})
	verifyPortsField(t, schema, "ports")

	// Verify required field
	if !contains(schema.Required, "api_key") {
		t.Errorf("Expected api_key in Required list")
	}
}

func TestGenerateConfigSchema_WithPointer(t *testing.T) {
	type TestConfig struct {
		Name string `json:"name" schema:"type:string,description:Name"`
	}

	// Test with pointer type
	schema := GenerateConfigSchema(reflect.TypeOf(&TestConfig{}))

	if _, exists := schema.Properties["name"]; !exists {
		t.Errorf("Expected field name not found when using pointer type")
	}
}

func TestGenerateConfigSchema_NonStruct(t *testing.T) {
	// Test with non-struct type
	schema := GenerateConfigSchema(reflect.TypeOf("string"))

	if len(schema.Properties) != 0 {
		t.Errorf("Expected empty schema for non-struct type, got %d properties", len(schema.Properties))
	}
}

func TestGeneratePortFieldSchema(t *testing.T) {
	fields := GeneratePortFieldSchema()
	configField, ok := fields["config"]
	if !ok || configField.Type != "object" || !configField.Editable {
		t.Fatalf("config field = %#v", configField)
	}
	if len(fields) != 4 {
		t.Fatalf("fields = %#v, want only canonical common-envelope fields", fields)
	}
	if len(configField.Variants) != len(canonicalPortKinds) {
		t.Fatalf("config variants = %d, want %d canonical kinds", len(configField.Variants), len(canonicalPortKinds))
	}
	nats := configField.Variants[string(PortKindNATS)]
	if !sameStrings(nats.Required, []string{"kind", "subject"}) ||
		!sameDirections(nats.Directions, []Direction{DirectionInput, DirectionOutput}) {
		t.Fatalf("nats schema = %#v", nats)
	}
	if kind := nats.Properties["kind"]; !sameStrings(kind.Enum, []string{string(PortKindNATS)}) {
		t.Fatalf("nats kind schema = %#v", kind)
	}
	jetstream := configField.Variants[string(PortKindJetStream)]
	if !sameStrings(jetstream.Required, []string{"kind", "subjects"}) {
		t.Fatalf("jetstream common requirements = %#v", jetstream.Required)
	}
	if got := jetstream.RequiredByDirection[DirectionInput]; !sameStrings(got, []string{"stream_name"}) {
		t.Fatalf("jetstream input requirements = %#v", jetstream.RequiredByDirection)
	}
	if got := jetstream.RequiredByDirection[DirectionOutput]; len(got) != 0 {
		t.Fatalf("jetstream output requirements = %#v", jetstream.RequiredByDirection)
	}
	if len(jetstream.AnyRequired) != 0 {
		t.Fatalf("jetstream retains alternative requirements = %#v", jetstream.AnyRequired)
	}
	maxAckPending := jetstream.Properties["max_ack_pending"]
	if maxAckPending.Minimum == nil || *maxAckPending.Minimum != -1 ||
		!maxAckPending.ZeroIsOmitted() || !sameDirections(maxAckPending.Directions, []Direction{DirectionInput}) {
		t.Fatalf("max_ack_pending constraint = %#v", maxAckPending)
	}
	discoveryJSON, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(discoveryJSON, []byte("zeroIsOmitted")) || bytes.Contains(discoveryJSON, []byte("zero_is_omitted")) {
		t.Fatalf("private zero-omission marker leaked into discovery JSON: %s", discoveryJSON)
	}
	if got := configField.Variants[string(PortKindTimer)].Directions; !sameDirections(got, []Direction{DirectionInput}) {
		t.Fatalf("timer directions = %v", got)
	}
	if got := configField.Variants[string(PortKindKVWrite)].Directions; !sameDirections(got, []Direction{DirectionOutput}) {
		t.Fatalf("kv-write directions = %v", got)
	}
	for kind, variant := range configField.Variants {
		if variant.AdditionalProperties == nil || *variant.AdditionalProperties {
			t.Errorf("variant %q does not close unknown fields", kind)
		}
		for _, required := range variant.Required {
			if _, ok := variant.Properties[required]; !ok {
				t.Errorf("variant %q requires missing property %q", kind, required)
			}
		}
		for direction, requiredFields := range variant.RequiredByDirection {
			if direction != DirectionInput && direction != DirectionOutput {
				t.Errorf("variant %q has unknown required direction %q", kind, direction)
			}
			for _, required := range requiredFields {
				if _, ok := variant.Properties[required]; !ok {
					t.Errorf("variant %q requires missing %s property %q", kind, direction, required)
				}
			}
		}
	}
}

func sameStrings(got, want []string) bool { return reflect.DeepEqual(got, want) }

func sameDirections(got, want []Direction) bool { return reflect.DeepEqual(got, want) }

func sameStringGroups(got, want [][]string) bool { return reflect.DeepEqual(got, want) }

func TestGeneratePortFieldSchema_WithTags(t *testing.T) {
	// Test struct with schema tags
	type TestPort struct {
		Name     string `json:"name"     schema:"readonly,type:string,description:Port name"`
		Subject  string `json:"subject"  schema:"editable,type:string,description:NATS subject"`
		Internal string `json:"internal" schema:"type:string,description:Internal field"`
	}

	portType := reflect.TypeOf(TestPort{})
	fields := make(map[string]PortFieldInfo)

	for i := 0; i < portType.NumField(); i++ {
		field := portType.Field(i)
		jsonTag := field.Tag.Get("json")
		if jsonTag == "" || jsonTag == "-" {
			continue
		}

		fieldName := jsonTag
		schemaTag := field.Tag.Get("schema")
		if schemaTag == "" {
			fields[fieldName] = PortFieldInfo{
				Type:     "string",
				Editable: false,
			}
			continue
		}

		directives, err := ParseSchemaTag(schemaTag)
		if err != nil {
			continue
		}

		fields[fieldName] = PortFieldInfo{
			Type:     directives.Type,
			Editable: directives.Editable,
		}
	}

	// Verify read-only field
	if info, exists := fields["name"]; exists {
		if info.Editable {
			t.Errorf("name.Editable = true, want false")
		}
		if info.Type != "string" {
			t.Errorf("name.Type = %s, want string", info.Type)
		}
	} else {
		t.Errorf("Expected field name not found")
	}

	// Verify editable field
	if info, exists := fields["subject"]; exists {
		if !info.Editable {
			t.Errorf("subject.Editable = false, want true")
		}
		if info.Type != "string" {
			t.Errorf("subject.Type = %s, want string", info.Type)
		}
	} else {
		t.Errorf("Expected field subject not found")
	}

	// Verify field without readonly/editable flag
	if info, exists := fields["internal"]; exists {
		if info.Editable {
			t.Errorf("internal.Editable = true, want false (default)")
		}
	} else {
		t.Errorf("Expected field internal not found")
	}
}

// Helper function for contains
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// Benchmark tests
func BenchmarkParseSchemaTag(b *testing.B) {
	tag := "type:string,description:Component name,category:basic,default:test"
	for i := 0; i < b.N; i++ {
		_, _ = ParseSchemaTag(tag)
	}
}

func BenchmarkGenerateConfigSchema(b *testing.B) {
	type BenchConfig struct {
		Name     string `json:"name"      schema:"type:string,description:Name,category:basic"`
		Port     int    `json:"port"      schema:"type:int,description:Port,min:1,max:65535,default:8080"`
		Enabled  bool   `json:"enabled"   schema:"type:bool,description:Enable,default:true"`
		LogLevel string `json:"log_level" schema:"type:enum,description:Log level,enum:debug|info|warn|error"`
	}

	configType := reflect.TypeOf(BenchConfig{})
	for i := 0; i < b.N; i++ {
		_ = GenerateConfigSchema(configType)
	}
}

func BenchmarkGeneratePortFieldSchema(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = GeneratePortFieldSchema()
	}
}
