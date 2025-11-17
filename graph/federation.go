package graph

import (
	"fmt"
	"strings"

	"github.com/c360/semstreams/config"
	"github.com/c360/semstreams/message"
	"github.com/c360/semstreams/vocabulary"
	"github.com/google/uuid"
)

// BuildGlobalID creates a structured global identifier for an entity.
// The format is designed to be human-readable while ensuring uniqueness
// across federated deployments.
//
// Format: "platform_id:region:local_id"
// Example: "us-west-prod:gulf_mexico:drone_1"
//
// If region is empty, format becomes: "platform_id:local_id"
// Example: "standalone:drone_1"
//
// Returns empty string if platform.ID or localID is empty.
func BuildGlobalID(platform config.PlatformConfig, localID string) string {
	if platform.ID == "" || localID == "" {
		return ""
	}
	if platform.Region != "" {
		return fmt.Sprintf("%s:%s:%s", platform.ID, platform.Region, localID)
	}
	return fmt.Sprintf("%s:%s", platform.ID, localID)
}

// ParseGlobalID extracts the components from a global ID.
// Returns platformID, region, localID, and whether the parse was successful.
//
// Examples:
//   - "us-west:gulf_mexico:drone_1" → "us-west", "gulf_mexico", "drone_1", true
//   - "standalone:drone_1" → "standalone", "", "drone_1", true
//   - "drone_1" → "", "", "drone_1", false (not a global ID)
//   - "" → "", "", "", false (empty input)
func ParseGlobalID(globalID string) (platformID, region, localID string, ok bool) {
	if globalID == "" {
		return "", "", "", false
	}

	parts := strings.Split(globalID, ":")

	switch len(parts) {
	case 3:
		// Full format: platform:region:local
		// Validate required parts are non-empty (platform and local ID)
		// Note: region can be empty, but platform and local ID must be present
		if parts[0] == "" || parts[2] == "" {
			return "", "", globalID, false
		}
		return parts[0], parts[1], parts[2], true
	case 2:
		// No region: platform:local
		// Validate both parts are non-empty
		if parts[0] == "" || parts[1] == "" {
			return "", "", globalID, false
		}
		return parts[0], "", parts[1], true
	default:
		// Not a global ID (probably just a local ID)
		return "", "", globalID, false
	}
}

// FederatedEntity represents an entity with federation metadata.
// This is used when storing entities from federated messages.
type FederatedEntity struct {
	LocalID    string    `json:"local_id"`    // Original entity ID
	GlobalID   string    `json:"global_id"`   // Structured global ID
	PlatformID string    `json:"platform_id"` // Origin platform
	Region     string    `json:"region,omitempty"`
	MessageUID uuid.UUID `json:"message_uid"` // UID of originating message
}

// GetEntityIRI generates an IRI for this federated entity instance.
// This method provides federation-ready IRIs for future RDF export or cross-platform correlation.
//
// The IRI format follows the pattern:
// "https://semstreams.c360.io/entities/{platform_id}[/{region}]/{domain}/{type}/{local_id}"
//
// Returns empty string if the entity lacks required federation metadata (PlatformID or LocalID)
// or if the entityType is invalid.
//
// Example usage:
//
//	iri := federatedEntity.GetEntityIRI("robotics.drone")
//	// Returns: "https://semstreams.c360.io/entities/us-west-prod/gulf_mexico/robotics/drone/drone_1"
func (fe *FederatedEntity) GetEntityIRI(entityType string) string {
	if fe.PlatformID == "" || fe.LocalID == "" {
		return ""
	}

	platform := config.PlatformConfig{
		ID:     fe.PlatformID,
		Region: fe.Region,
	}

	return vocabulary.EntityIRI(entityType, platform, fe.LocalID)
}

// BuildFederatedEntity creates federation metadata for an entity.
// This extracts platform information from the message and constructs
// the appropriate global identifiers.
func BuildFederatedEntity(localID string, msg message.Message) *FederatedEntity {
	platform, hasPlatform := message.GetPlatform(msg)
	uid, hasUID := message.GetUID(msg)

	if !hasPlatform {
		// Non-federated message, return minimal info
		return &FederatedEntity{
			LocalID:  localID,
			GlobalID: localID, // No federation, global = local
		}
	}

	fe := &FederatedEntity{
		LocalID:    localID,
		GlobalID:   BuildGlobalID(platform, localID),
		PlatformID: platform.ID,
		Region:     platform.Region,
	}

	if hasUID {
		fe.MessageUID = uid
	}

	return fe
}

// EnrichEntityState adds federation metadata to an EntityState.
// This should be called when storing entities from federated messages.
func EnrichEntityState(state *EntityState, fed *FederatedEntity) {
	if state.Node.Properties == nil {
		state.Node.Properties = make(map[string]any)
	}

	// Add federation properties
	state.Node.Properties["local_id"] = fed.LocalID
	state.Node.Properties["global_id"] = fed.GlobalID
	state.Node.Properties["platform_id"] = fed.PlatformID

	if fed.Region != "" {
		state.Node.Properties["region"] = fed.Region
	}

	if fed.MessageUID != (uuid.UUID{}) {
		state.Node.Properties["message_uid"] = fed.MessageUID.String()
	}
}

// IsFederatedEntity checks if an EntityState has federation metadata.
func IsFederatedEntity(state *EntityState) bool {
	if state.Node.Properties == nil {
		return false
	}
	_, hasGlobalID := state.Node.Properties["global_id"]
	_, hasPlatformID := state.Node.Properties["platform_id"]
	return hasGlobalID && hasPlatformID
}

// GetFederationInfo extracts federation metadata from an EntityState.
// Returns nil if the entity is not federated.
func GetFederationInfo(state *EntityState) *FederatedEntity {
	if !IsFederatedEntity(state) {
		return nil
	}

	fed := &FederatedEntity{}

	if v, ok := state.Node.Properties["local_id"].(string); ok {
		fed.LocalID = v
	}
	if v, ok := state.Node.Properties["global_id"].(string); ok {
		fed.GlobalID = v
	}
	if v, ok := state.Node.Properties["platform_id"].(string); ok {
		fed.PlatformID = v
	}
	if v, ok := state.Node.Properties["region"].(string); ok {
		fed.Region = v
	}
	if v, ok := state.Node.Properties["message_uid"].(string); ok {
		if uid, err := uuid.Parse(v); err == nil {
			fed.MessageUID = uid
		}
	}

	return fed
}
