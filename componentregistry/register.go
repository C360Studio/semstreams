// Package componentregistry provides component registration for SemStreams framework.
// This package registers both protocol-level and semantic-level components.
package componentregistry

import (
	"errors"

	"github.com/c360/semstreams/component"
	pkgerrors "github.com/c360/semstreams/errors"
	gatewaygraphql "github.com/c360/semstreams/gateway/graphql"
	gatewayhttp "github.com/c360/semstreams/gateway/http"
	"github.com/c360/semstreams/input/udp"
	websocketinput "github.com/c360/semstreams/input/websocket"
	"github.com/c360/semstreams/output/file"
	"github.com/c360/semstreams/output/httppost"
	"github.com/c360/semstreams/output/websocket"
	"github.com/c360/semstreams/processor/context"
	"github.com/c360/semstreams/processor/graph"
	jsonfilter "github.com/c360/semstreams/processor/json_filter"
	jsongeneric "github.com/c360/semstreams/processor/json_generic"
	jsonmap "github.com/c360/semstreams/processor/json_map"
	jsontoentity "github.com/c360/semstreams/processor/json_to_entity"
	"github.com/c360/semstreams/storage/objectstore"
)

// Register registers all SemStreams framework components with the provided registry.
// This includes both protocol-layer and semantic-layer components:
//
// Protocol Layer (network/data agnostic):
//   - UDP input (network protocol)
//   - WebSocket input (federation)
//   - JSONGeneric processor (Plain JSON wrapper)
//   - JSONFilter processor (field-based filtering)
//   - JSONMap processor (field transformation)
//   - JSONToEntity processor (GenericJSON to Entity conversion)
//   - ObjectStore storage (NATS JetStream)
//   - File output (file system)
//   - HTTP POST output (webhooks)
//   - WebSocket output (broadcasting)
//   - HTTP gateway (bidirectional HTTP ↔ NATS request/reply)
//   - GraphQL gateway (schema-driven queries for AI agents)
//
// Semantic Layer (domain agnostic):
//   - Graph processor (entity graph operations)
//   - Context processor (context enrichment)
//
// Note: Domain-specific components (MAVLink, robotics, etc.) are registered
// in separate modules like streamkit-robotics.
func Register(registry *component.Registry) error {
	// CRITICAL: Nil registry is a programming error (fatal), not invalid input
	if registry == nil {
		return pkgerrors.WrapFatal(
			errors.New("registry cannot be nil"),
			"ComponentRegistry", "Register", "registry validation")
	}

	// Protocol Layer - Network Inputs
	if err := udp.Register(registry); err != nil {
		return pkgerrors.WrapInvalid(err, "ComponentRegistry", "Register", "UDP input component registration")
	}

	if err := websocketinput.Register(registry); err != nil {
		return pkgerrors.WrapInvalid(err, "ComponentRegistry", "Register", "WebSocket input component registration")
	}

	// Protocol Layer - Processors
	if err := jsongeneric.Register(registry); err != nil {
		return pkgerrors.WrapInvalid(
			err,
			"ComponentRegistry",
			"Register",
			"JSONGeneric processor component registration",
		)
	}

	if err := jsonfilter.Register(registry); err != nil {
		return pkgerrors.WrapInvalid(
			err,
			"ComponentRegistry",
			"Register",
			"JSONFilter processor component registration",
		)
	}

	if err := jsonmap.Register(registry); err != nil {
		return pkgerrors.WrapInvalid(err, "ComponentRegistry", "Register", "JSONMap processor component registration")
	}

	if err := jsontoentity.Register(registry); err != nil {
		return pkgerrors.WrapInvalid(err, "ComponentRegistry", "Register", "JSONToEntity processor component registration")
	}

	// Protocol Layer - Storage
	if err := objectstore.Register(registry); err != nil {
		return pkgerrors.WrapInvalid(err, "ComponentRegistry", "Register", "ObjectStore storage component registration")
	}

	// Protocol Layer - Outputs
	if err := file.Register(registry); err != nil {
		return pkgerrors.WrapInvalid(err, "ComponentRegistry", "Register", "File output component registration")
	}

	if err := httppost.Register(registry); err != nil {
		return pkgerrors.WrapInvalid(err, "ComponentRegistry", "Register", "HTTP POST output component registration")
	}

	if err := websocket.Register(registry); err != nil {
		return pkgerrors.WrapInvalid(err, "ComponentRegistry", "Register", "WebSocket output component registration")
	}

	// Protocol Layer - Gateways
	if err := gatewayhttp.Register(registry); err != nil {
		return pkgerrors.WrapInvalid(err, "ComponentRegistry", "Register", "HTTP gateway component registration")
	}

	if err := gatewaygraphql.Register(registry); err != nil {
		return pkgerrors.WrapInvalid(err, "ComponentRegistry", "Register", "GraphQL gateway component registration")
	}

	// Semantic Layer - Processors
	if err := graph.Register(registry); err != nil {
		return pkgerrors.WrapInvalid(err, "ComponentRegistry", "Register", "Graph processor component registration")
	}

	if err := context.Register(registry); err != nil {
		return pkgerrors.WrapInvalid(err, "ComponentRegistry", "Register", "Context processor component registration")
	}

	return nil
}
