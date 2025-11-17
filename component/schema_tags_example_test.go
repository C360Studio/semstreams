package component_test

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/c360/semstreams/component"
)

// ExampleGenerateConfigSchema demonstrates how to use schema tags to auto-generate
// configuration schemas from struct definitions
func ExampleGenerateConfigSchema() {
	// Define a configuration struct with schema tags
	type ComponentConfig struct {
		// Basic configuration
		Name    string `json:"name"    schema:"type:string,description:Component name,category:basic"`
		Port    int    `json:"port"    schema:"type:int,description:Listen port,min:1,max:65535,default:8080,category:basic"`
		Enabled bool   `json:"enabled" schema:"type:bool,description:Enable component,default:true,category:basic"`

		// Advanced configuration
		Timeout  string `json:"timeout"   schema:"type:string,description:Request timeout,default:30s,category:advanced"`
		LogLevel string `json:"log_level" schema:"type:enum,description:Logging level,enum:debug|info|warn|error,default:info,category:advanced"`

		// Required field
		APIKey string `json:"api_key" schema:"required,type:string,description:Authentication API key"`
	}

	// Generate the schema at init time (one-time reflection cost)
	schema := component.GenerateConfigSchema(reflect.TypeOf(ComponentConfig{}))

	// The generated schema can be used for validation, UI generation, etc.
	schemaJSON, _ := json.MarshalIndent(schema, "", "  ")
	fmt.Println(string(schemaJSON))

	// Output will show the generated schema with all properties
}

// ExampleParseSchemaTag demonstrates parsing individual schema tags
func ExampleParseSchemaTag() {
	// Parse a simple field tag
	tag := "type:int,description:Port number,min:1,max:65535,default:8080"
	directives, err := component.ParseSchemaTag(tag)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Type: %s\n", directives.Type)
	fmt.Printf("Description: %s\n", directives.Description)
	fmt.Printf("Min: %d\n", *directives.Min)
	fmt.Printf("Max: %d\n", *directives.Max)
	fmt.Printf("Default: %s\n", directives.Default)

	// Output:
	// Type: int
	// Description: Port number
	// Min: 1
	// Max: 65535
	// Default: 8080
}

// ExampleParseSchemaTag_enum demonstrates parsing enum tags
func ExampleParseSchemaTag_enum() {
	tag := "type:enum,description:Log level,enum:debug|info|warn|error,default:info"
	directives, _ := component.ParseSchemaTag(tag)

	fmt.Printf("Type: %s\n", directives.Type)
	fmt.Printf("Description: %s\n", directives.Description)
	fmt.Printf("Enum values: %v\n", directives.Enum)
	fmt.Printf("Default: %s\n", directives.Default)

	// Output:
	// Type: enum
	// Description: Log level
	// Enum values: [debug info warn error]
	// Default: info
}

// ExampleParseSchemaTag_flags demonstrates boolean flags
func ExampleParseSchemaTag_flags() {
	tag := "required,readonly,type:string,description:System identifier"
	directives, _ := component.ParseSchemaTag(tag)

	fmt.Printf("Type: %s\n", directives.Type)
	fmt.Printf("Required: %v\n", directives.Required)
	fmt.Printf("ReadOnly: %v\n", directives.ReadOnly)

	// Output:
	// Type: string
	// Required: true
	// ReadOnly: true
}
