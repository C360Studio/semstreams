// Package vocabulary provides semantic vocabulary definitions and mappings.
package vocabulary

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/c360/semstreams/config"
)

// Base IRI constants for the SemStreams vocabulary
const (
	SemStreamsBase  = "https://semstreams.c360.io"
	GraphNamespace  = SemStreamsBase + "/graph"
	SystemNamespace = SemStreamsBase + "/system"
)

// EntityTypeIRI converts a dotted entity type to an IRI format for RDF export.
//
// Input format: "domain.type" using EntityType.Key() (e.g., "robotics.drone")
// Output format: "https://semstreams.c360.io/domain#type"
//
// This function is intended for RDF/Turtle export at API boundaries only.
// Internal code should always use dotted notation.
//
// Returns empty string for invalid input formats.
//
// Example:
//
//	entityType := message.EntityType{Domain: "robotics", Type: "drone"}
//	iri := EntityTypeIRI(entityType.Key())  // "https://semstreams.c360.io/robotics#drone"
func EntityTypeIRI(dottedType string) string {
	if dottedType == "" {
		return ""
	}

	// Remove any leading/trailing whitespace
	dottedType = strings.TrimSpace(dottedType)
	if dottedType == "" {
		return ""
	}

	// Split on dot - expect exactly 2 parts (domain.type)
	parts := strings.Split(dottedType, ".")
	if len(parts) != 2 {
		return ""
	}

	domain := strings.TrimSpace(parts[0])
	entityType := strings.TrimSpace(parts[1])

	// Both parts must be non-empty
	if domain == "" || entityType == "" {
		return ""
	}

	// Capitalize first letter of type for IRI fragment (convention)
	if len(entityType) > 0 {
		entityType = strings.ToUpper(entityType[:1]) + entityType[1:]
	}

	return fmt.Sprintf("%s/%s#%s", SemStreamsBase, domain, entityType)
}

// EntityIRI generates an IRI for a specific entity instance for RDF export.
// This creates a unique identifier for entities in federated scenarios.
//
// Input format: "domain.type" using EntityType.Key() (e.g., "robotics.drone")
// Output format: "https://semstreams.c360.io/entities/{platform_id}[/{region}]/{domain}/{type}/{local_id}"
//
// This function is intended for RDF/Turtle export at API boundaries only.
// Internal code should always use EntityID.Key() for dotted notation.
//
// Examples:
//   - With region: "https://semstreams.c360.io/entities/us-west-prod/gulf_mexico/robotics/drone/drone_1"
//   - Without region: "https://semstreams.c360.io/entities/standalone/robotics/battery/battery_main"
//
// Returns empty string if platform.ID, localID is empty, or dottedType is invalid.
//
// Example:
//
//	entityType := message.EntityType{Domain: "robotics", Type: "drone"}
//	iri := EntityIRI(entityType.Key(), platform, "drone_001")
func EntityIRI(dottedType string, platform config.PlatformConfig, localID string) string {
	if platform.ID == "" || localID == "" {
		return ""
	}

	// Parse and validate domain type (dotted notation)
	parts := strings.Split(dottedType, ".")
	if len(parts) != 2 {
		return ""
	}

	domain := strings.TrimSpace(parts[0])
	entityType := strings.TrimSpace(parts[1])

	if domain == "" || entityType == "" {
		return ""
	}

	// Build the IRI path (lowercase for consistency)
	if platform.Region != "" {
		return fmt.Sprintf("%s/entities/%s/%s/%s/%s/%s",
			SemStreamsBase, platform.ID, platform.Region, domain, entityType, localID)
	}

	return fmt.Sprintf("%s/entities/%s/%s/%s/%s",
		SemStreamsBase, platform.ID, domain, entityType, localID)
}

// RelationshipIRI converts relationship types to IRI format.
// Handles various naming conventions and converts them to kebab-case.
//
// Examples:
//   - "POWERED_BY" -> "https://semstreams.c360.io/relationships#powered-by"
//   - "HAS_COMPONENT" -> "https://semstreams.c360.io/relationships#has-component"
//   - "PoweredBy" -> "https://semstreams.c360.io/relationships#powered-by"
//
// Returns empty string for empty input.
func RelationshipIRI(relType string) string {
	if relType == "" {
		return ""
	}

	// Convert to kebab-case
	kebabCase := toKebabCase(relType)
	if kebabCase == "" {
		return ""
	}

	return fmt.Sprintf("%s/relationships#%s", SemStreamsBase, kebabCase)
}

// SubjectIRI converts NATS subject strings to IRI format.
// Converts dot-separated subjects to path-separated IRIs.
//
// Examples:
//   - "semantic.robotics.heartbeat" -> "https://semstreams.c360.io/subjects/semantic/robotics/heartbeat"
//   - "raw.udp.mavlink" -> "https://semstreams.c360.io/subjects/raw/udp/mavlink"
//
// Returns empty string for empty input or malformed subjects (leading/trailing dots).
func SubjectIRI(subject string) string {
	if subject == "" {
		return ""
	}

	// Check for malformed subjects (leading or trailing dots)
	if strings.HasPrefix(subject, ".") || strings.HasSuffix(subject, ".") {
		return ""
	}

	// Split on dots and validate no empty segments
	parts := strings.Split(subject, ".")
	for _, part := range parts {
		if part == "" {
			return ""
		}
	}

	// Convert dots to forward slashes
	path := strings.ReplaceAll(subject, ".", "/")

	return fmt.Sprintf("%s/subjects/%s", SemStreamsBase, path)
}

// toKebabCase converts various naming conventions to kebab-case.
// Handles:
// - SCREAMING_SNAKE_CASE -> kebab-case
// - PascalCase -> kebab-case
// - camelCase -> kebab-case
// - Already kebab-case -> unchanged
func toKebabCase(input string) string {
	if input == "" {
		return ""
	}

	// Handle SCREAMING_SNAKE_CASE
	if strings.Contains(input, "_") {
		parts := strings.Split(input, "_")
		var result []string
		for _, part := range parts {
			if part != "" {
				result = append(result, strings.ToLower(part))
			}
		}
		return strings.Join(result, "-")
	}

	// Handle PascalCase and camelCase
	// Insert hyphens before uppercase letters (except the first character)
	re := regexp.MustCompile(`([a-z])([A-Z])`)
	kebab := re.ReplaceAllString(input, "${1}-${2}")

	// Convert to lowercase
	return strings.ToLower(kebab)
}
