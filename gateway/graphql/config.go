package graphql

import (
	"fmt"
	"time"

	"github.com/c360/semstreams/errors"
)

// Config holds configuration for the GraphQL gateway component
type Config struct {
	// BindAddress is the HTTP bind address (default: ":8080")
	BindAddress string `json:"bind_address" schema:"type:string,description:HTTP bind address,default::8080,category:basic"`

	// Path is the GraphQL endpoint path (default: "/graphql")
	Path string `json:"path" schema:"type:string,description:GraphQL endpoint path,default:/graphql,category:basic"`

	// SchemaFile is the path to the GraphQL schema file (required for Phase 2)
	// For Phase 1, this can be empty as we're building generic infrastructure
	SchemaFile string `json:"schema_file,omitempty" schema:"type:string,description:GraphQL schema file path,category:basic"`

	// EnablePlayground enables GraphQL Playground UI (default: true)
	EnablePlayground bool `json:"enable_playground" schema:"type:bool,description:Enable GraphQL Playground,default:true,category:basic"`

	// EnableCORS enables CORS headers (default: true)
	EnableCORS bool `json:"enable_cors" schema:"type:bool,description:Enable CORS,default:true,category:advanced"`

	// CORSOrigins lists allowed CORS origins (default: ["*"])
	CORSOrigins []string `json:"cors_origins,omitempty" schema:"type:array,description:Allowed CORS origins,category:advanced"`

	// TimeoutStr is the default query timeout (default: "30s")
	TimeoutStr string `json:"timeout,omitempty" schema:"type:string,description:Query timeout,default:30s,category:advanced"`

	// MaxQueryDepth limits GraphQL query nesting depth (default: 10)
	MaxQueryDepth int `json:"max_query_depth,omitempty" schema:"type:int,description:Maximum query depth,default:10,category:advanced"`

	// NATSSubjects configures NATS subject mappings
	NATSSubjects NATSSubjectsConfig `json:"nats_subjects" schema:"type:object,description:NATS subject configuration,category:basic"`

	// timeout is the parsed duration (internal use)
	timeout time.Duration
}

// NATSSubjectsConfig defines NATS subject mappings for GraphQL operations
type NATSSubjectsConfig struct {
	// EntityQuery is the NATS subject for querying a single entity by ID
	EntityQuery string `json:"entity_query" schema:"type:string,description:Entity query subject,default:graph.query.entity,category:basic"`

	// EntitiesQuery is the NATS subject for batch querying entities by IDs
	EntitiesQuery string `json:"entities_query" schema:"type:string,description:Entities batch query subject,default:graph.query.entities,category:basic"`

	// TypeQuery is the NATS subject for querying entities by type/predicate
	TypeQuery string `json:"type_query" schema:"type:string,description:Type/predicate query subject,default:graph.query.type,category:basic"`

	// RelationshipQuery is the NATS subject for querying relationships
	RelationshipQuery string `json:"relationship_query" schema:"type:string,description:Relationship query subject,default:graph.query.relationships,category:basic"`

	// SemanticSearch is the NATS subject for semantic search
	SemanticSearch string `json:"semantic_search" schema:"type:string,description:Semantic search subject,default:graph.query.semantic,category:basic"`
}

// Validate ensures the configuration is valid
func (c *Config) Validate() error {
	// Validate bind address
	if c.BindAddress == "" {
		c.BindAddress = ":8080"
	}

	// Validate path
	if c.Path == "" {
		c.Path = "/graphql"
	}
	if len(c.Path) == 0 || c.Path[0] != '/' {
		return errors.WrapInvalid(errors.ErrInvalidConfig, "Config", "Validate",
			"path must start with /")
	}

	// Validate timeout
	if c.TimeoutStr == "" {
		c.timeout = 30 * time.Second
	} else {
		timeout, err := time.ParseDuration(c.TimeoutStr)
		if err != nil {
			return errors.WrapInvalid(err, "Config", "Validate",
				fmt.Sprintf("invalid timeout format: %s", c.TimeoutStr))
		}
		if timeout < 100*time.Millisecond || timeout > 5*time.Minute {
			return errors.WrapInvalid(errors.ErrInvalidConfig, "Config", "Validate",
				"timeout must be between 100ms and 5m")
		}
		c.timeout = timeout
	}

	// Validate max query depth
	if c.MaxQueryDepth == 0 {
		c.MaxQueryDepth = 10
	}
	if c.MaxQueryDepth < 1 || c.MaxQueryDepth > 50 {
		return errors.WrapInvalid(errors.ErrInvalidConfig, "Config", "Validate",
			"max_query_depth must be between 1 and 50")
	}

	// Set CORS defaults
	if c.EnableCORS && len(c.CORSOrigins) == 0 {
		c.CORSOrigins = []string{"*"}
	}

	// Validate NATS subjects
	if err := c.NATSSubjects.Validate(); err != nil {
		return errors.WrapInvalid(err, "Config", "Validate", "NATS subjects validation")
	}

	return nil
}

// Timeout returns the parsed timeout duration
func (c *Config) Timeout() time.Duration {
	return c.timeout
}

// Validate ensures NATS subject configuration is valid
func (n *NATSSubjectsConfig) Validate() error {
	// Set defaults if empty
	if n.EntityQuery == "" {
		n.EntityQuery = "graph.query.entity"
	}
	if n.EntitiesQuery == "" {
		n.EntitiesQuery = "graph.query.entities"
	}
	if n.TypeQuery == "" {
		n.TypeQuery = "graph.query.type"
	}
	if n.RelationshipQuery == "" {
		n.RelationshipQuery = "graph.query.relationships"
	}
	if n.SemanticSearch == "" {
		n.SemanticSearch = "graph.query.semantic"
	}

	// Validate subject format (basic check - not empty)
	if n.EntityQuery == "" || n.EntitiesQuery == "" || n.TypeQuery == "" ||
		n.RelationshipQuery == "" || n.SemanticSearch == "" {
		return errors.WrapInvalid(errors.ErrInvalidConfig, "NATSSubjectsConfig", "Validate",
			"all NATS subjects must be configured")
	}

	return nil
}

// DefaultConfig returns default GraphQL gateway configuration
func DefaultConfig() Config {
	return Config{
		BindAddress:      ":8080",
		Path:             "/graphql",
		EnablePlayground: true,
		EnableCORS:       true,
		CORSOrigins:      []string{"*"},
		TimeoutStr:       "30s",
		MaxQueryDepth:    10,
		NATSSubjects: NATSSubjectsConfig{
			EntityQuery:       "graph.query.entity",
			EntitiesQuery:     "graph.query.entities",
			TypeQuery:         "graph.query.type",
			RelationshipQuery: "graph.query.relationships",
			SemanticSearch:    "graph.query.semantic",
		},
	}
}
