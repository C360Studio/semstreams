package message

import (
	"time"

	"github.com/c360/semstreams/config"
	"github.com/google/uuid"
)

// FederationMeta extends Meta with federation support for multi-platform deployments.
// It adds a global unique identifier (UID) and platform information to enable
// entity correlation across distributed SemStreams instances.
//
// The UID is a proper UUID that provides global uniqueness, while the Platform
// information identifies the specific instance/region where the message originated.
//
// This enables several federation patterns:
//   - Cross-platform entity correlation (same drone tracked by multiple stations)
//   - Data aggregation from multiple regions
//   - Entity resolution across federated deployments
//   - Distributed graph queries
type FederationMeta interface {
	Meta

	// UID returns the global unique identifier for this message.
	// This is a proper UUID that ensures uniqueness across all platforms.
	UID() uuid.UUID

	// Platform returns information about the originating platform.
	// This identifies which SemStreams instance created this message.
	Platform() config.PlatformConfig
}

// DefaultFederationMeta provides the standard implementation of FederationMeta.
// It embeds DefaultMeta and adds federation-specific fields.
type DefaultFederationMeta struct {
	*DefaultMeta
	uid      uuid.UUID
	platform config.PlatformConfig
}

// NewFederationMeta creates a new DefaultFederationMeta with auto-generated UID.
//
// Parameters:
//   - source: The service or component creating this message
//   - platform: The platform configuration identifying the origin instance
//
// The UID is automatically generated as a new UUID, and timestamps are set
// to the current time.
func NewFederationMeta(source string, platform config.PlatformConfig) *DefaultFederationMeta {
	return &DefaultFederationMeta{
		DefaultMeta: NewDefaultMeta(time.Now(), source),
		uid:         uuid.New(),
		platform:    platform,
	}
}

// NewFederationMetaWithTime creates a new DefaultFederationMeta with specific creation time.
// Useful for historical data import or testing.
func NewFederationMetaWithTime(
	source string,
	platform config.PlatformConfig,
	createdAt time.Time,
) *DefaultFederationMeta {
	return &DefaultFederationMeta{
		DefaultMeta: NewDefaultMeta(createdAt, source),
		uid:         uuid.New(),
		platform:    platform,
	}
}

// UID returns the global unique identifier for this message.
func (m *DefaultFederationMeta) UID() uuid.UUID {
	return m.uid
}

// Platform returns the originating platform configuration.
func (m *DefaultFederationMeta) Platform() config.PlatformConfig {
	return m.platform
}

// GetPlatform is a helper function to extract platform information from any message.
// It returns the platform config and true if the message has federation metadata,
// or an empty config and false otherwise.
//
// This allows code to gracefully handle both federated and non-federated messages:
//
//	if platform, ok := GetPlatform(msg); ok {
//	    // Handle federated message with platform info
//	    globalID := BuildGlobalID(entityID, platform)
//	} else {
//	    // Handle non-federated message
//	    localID := entityID
//	}
func GetPlatform(msg Message) (config.PlatformConfig, bool) {
	if fedMeta, ok := msg.Meta().(FederationMeta); ok {
		return fedMeta.Platform(), true
	}
	return config.PlatformConfig{}, false
}

// GetUID is a helper function to extract the UID from any message.
// It returns the UID and true if the message has federation metadata,
// or a zero UUID and false otherwise.
func GetUID(msg Message) (uuid.UUID, bool) {
	if fedMeta, ok := msg.Meta().(FederationMeta); ok {
		return fedMeta.UID(), true
	}
	return uuid.UUID{}, false
}
