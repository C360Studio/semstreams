package main

// Operation describes a single API operation
type Operation struct {
	Summary     string              `yaml:"summary"`
	Description string              `yaml:"description,omitempty"`
	Tags        []string            `yaml:"tags,omitempty"`
	Parameters  []Parameter         `yaml:"parameters,omitempty"`
	Responses   map[string]Response `yaml:"responses"`
}

// Parameter describes an operation parameter
type Parameter struct {
	Name        string    `yaml:"name"`
	In          string    `yaml:"in"` // "query", "path", "header"
	Required    bool      `yaml:"required,omitempty"`
	Description string    `yaml:"description,omitempty"`
	Schema      SchemaRef `yaml:"schema"`
}

// Response describes an operation response
type Response struct {
	Description string               `yaml:"description"`
	Content     map[string]MediaType `yaml:"content,omitempty"`
}

// MediaType describes a media type and schema
type MediaType struct {
	Schema SchemaRef `yaml:"schema"`
}

// SchemaRef references a schema
type SchemaRef struct {
	Ref   string      `yaml:"$ref,omitempty"`
	Type  string      `yaml:"type,omitempty"`
	Items *SchemaRef  `yaml:"items,omitempty"`
	OneOf []SchemaRef `yaml:"oneOf,omitempty"`
}
