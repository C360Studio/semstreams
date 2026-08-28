// Package platform holds the platform-identity struct used across the
// SemStreams codebase. It lives at pkg/ so leaf packages (message,
// vocabulary) can reference platform identity without dragging in the
// full config tree.
//
// Historically PlatformConfig lived in package config. That created a
// transitive import cycle: any message package that wanted to label messages
// with platform identity had to import
// config, which forced config to stay free of imports from packages
// like component — even though config legitimately needs to consume
// component port definitions for stream derivation. Promoting the
// struct to a leaf package breaks the cycle without spreading
// platform identity across multiple definitions.
package platform

// Config defines platform identity and capabilities.
//
// Accessed as platform.Config to match the convention used by sibling
// leaf packages (pkg/security/config.go: security.Config). The
// config-package alias config.PlatformConfig is preserved for backward
// compatibility with existing call sites.
//
// Six-part federated entity IDs are anchored on Org and ID: they are
// positions 1-2 of every identity this deployment mints
// (org.platform.system.domain.type.instance, ADR-102), so ID is the
// minting deployment authority and nothing else names it. The struct
// is JSON-shaped so it round-trips cleanly through config files and
// embedded message metadata.
type Config struct {
	Org          string   `json:"org"`                    // Organization namespace (e.g., "c360", "noaa")
	ID           string   `json:"id"`                     // Platform identifier (e.g., "platform1")
	Type         string   `json:"type"`                   // vessel, shore, buoy, satellite
	Region       string   `json:"region,omitempty"`       // gulf_mexico, atlantic, pacific
	Capabilities []string `json:"capabilities,omitempty"` // radar, ctd, deployment, etc.

	Environment string `json:"environment,omitempty"` // "prod", "dev", "test"
}
