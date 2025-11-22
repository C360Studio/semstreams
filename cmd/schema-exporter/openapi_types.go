package main

// OpenAPIDocument represents the complete OpenAPI 3.0 specification
type OpenAPIDocument struct {
	OpenAPI    string              `yaml:"openapi"`
	Info       InfoObject          `yaml:"info"`
	Servers    []ServerObject      `yaml:"servers"`
	Paths      map[string]PathItem `yaml:"paths"`
	Components ComponentsObject    `yaml:"components"`
	Tags       []TagObject         `yaml:"tags"`
}

// InfoObject contains API metadata
type InfoObject struct {
	Title       string `yaml:"title"`
	Description string `yaml:"description"`
	Version     string `yaml:"version"`
}

// ServerObject defines an API server
type ServerObject struct {
	URL         string `yaml:"url"`
	Description string `yaml:"description"`
}

// ComponentsObject holds reusable objects
type ComponentsObject struct {
	Schemas map[string]interface{} `yaml:"schemas"`
}

// TagObject defines an API tag
type TagObject struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// PathItem describes operations available on a path
type PathItem struct {
	Get    *Operation `yaml:"get,omitempty"`
	Post   *Operation `yaml:"post,omitempty"`
	Put    *Operation `yaml:"put,omitempty"`
	Delete *Operation `yaml:"delete,omitempty"`
}
