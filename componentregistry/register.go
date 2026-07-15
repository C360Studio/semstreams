// Package componentregistry registers the SemStreams core component set.
// Optional capabilities and product adapters deliberately live in separate
// import roots so importing this package cannot pull them into a core binary.
package componentregistry

import (
	"errors"

	"github.com/c360studio/semstreams/component"
	graphgateway "github.com/c360studio/semstreams/gateway/graph-gateway"
	gatewayhttp "github.com/c360studio/semstreams/gateway/http"
	lifecyclegateway "github.com/c360studio/semstreams/gateway/lifecycle-gateway"
	fileinput "github.com/c360studio/semstreams/input/file"
	"github.com/c360studio/semstreams/input/udp"
	websocketinput "github.com/c360studio/semstreams/input/websocket"
	"github.com/c360studio/semstreams/output/file"
	"github.com/c360studio/semstreams/output/httppost"
	"github.com/c360studio/semstreams/output/websocket"
	pkgerrs "github.com/c360studio/semstreams/pkg/errs"
	agenticdispatch "github.com/c360studio/semstreams/processor/agentic-dispatch"
	agenticgovernance "github.com/c360studio/semstreams/processor/agentic-governance"
	agenticloop "github.com/c360studio/semstreams/processor/agentic-loop"
	agenticmodel "github.com/c360studio/semstreams/processor/agentic-model"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
	gateddagexec "github.com/c360studio/semstreams/processor/gated-dag"
	graphclustering "github.com/c360studio/semstreams/processor/graph-clustering"
	graphembedding "github.com/c360studio/semstreams/processor/graph-embedding"
	graphindex "github.com/c360studio/semstreams/processor/graph-index"
	graphindexspatial "github.com/c360studio/semstreams/processor/graph-index-spatial"
	graphindextemporal "github.com/c360studio/semstreams/processor/graph-index-temporal"
	graphingest "github.com/c360studio/semstreams/processor/graph-ingest"
	graphquery "github.com/c360studio/semstreams/processor/graph-query"
	jsonfilter "github.com/c360studio/semstreams/processor/json_filter"
	jsongeneric "github.com/c360studio/semstreams/processor/json_generic"
	jsonmap "github.com/c360studio/semstreams/processor/json_map"
	"github.com/c360studio/semstreams/processor/rule"
	"github.com/c360studio/semstreams/storage/objectstore"
)

// Register registers all SemStreams framework components with the provided registry.
// This includes protocol-layer, semantic-layer, and agentic-layer components:
//
// Protocol Layer (network/data agnostic):
//   - UDP input (network protocol)
//   - WebSocket input
//   - JSONGeneric processor (Plain JSON wrapper)
//   - JSONFilter processor (field-based filtering)
//   - JSONMap processor (field transformation)
//   - ObjectStore storage (NATS JetStream)
//   - File output (file system)
//   - HTTP POST output (webhooks)
//   - WebSocket output (broadcasting)
//   - HTTP gateway (bidirectional HTTP ↔ NATS request/reply)
//
// Semantic Layer - Graph Components (modular architecture):
//
//	Core (all tiers):
//	- graph-ingest (entity/triple CRUD, hierarchy inference)
//	- graph-index (OUTGOING, INCOMING, ALIAS, PREDICATE indexes)
//	- graph-gateway (GraphQL + MCP HTTP servers)
//
//	Statistical/Semantic tier:
//	- graph-embedding (vector embedding generation)
//	- graph-clustering (community detection, structural analysis, anomaly detection, LLM enhancement)
//
//	Optional indexes:
//	- graph-index-spatial (geospatial indexing)
//	- graph-index-temporal (time-based indexing)
//
// Semantic Layer - Rule Processing:
//   - Rule processor (rule-based transformations)
//
// Agentic Layer - LLM-powered autonomous agents:
//   - agentic-model (OpenAI-compatible LLM endpoint caller)
//   - agentic-tools (tool execution dispatcher)
//   - agentic-loop (state machine orchestrator with trajectory capture)
//
// Note: Domain-specific and example components (IoT sensor, document, MAVLink,
// robotics, etc.) are registered by their respective binaries under cmd/examples/
// or in separate modules like streamkit-robotics, not in this core registry.
func Register(registry *component.Registry) error {
	// CRITICAL: Nil registry is a programming error (fatal), not invalid input
	if registry == nil {
		return pkgerrs.WrapFatal(
			errors.New("registry cannot be nil"),
			"ComponentRegistry", "Register", "registry validation")
	}

	if err := registerProtocolLayer(registry); err != nil {
		return err
	}

	if err := registerSemanticLayer(registry); err != nil {
		return err
	}

	return registerAgenticLayer(registry)
}

// registerProtocolLayer registers protocol-layer components (inputs, processors, outputs, gateways).
func registerProtocolLayer(registry *component.Registry) error {
	// Network Inputs
	if err := udp.Register(registry); err != nil {
		return pkgerrs.WrapInvalid(err, "ComponentRegistry", "Register", "UDP input component registration")
	}

	if err := websocketinput.Register(registry); err != nil {
		return pkgerrs.WrapInvalid(err, "ComponentRegistry", "Register", "WebSocket input component registration")
	}

	if err := fileinput.Register(registry); err != nil {
		return pkgerrs.WrapInvalid(err, "ComponentRegistry", "Register", "File input component registration")
	}

	// Processors
	if err := jsongeneric.Register(registry); err != nil {
		return pkgerrs.WrapInvalid(err, "ComponentRegistry", "Register", "JSONGeneric processor component registration")
	}

	if err := jsonfilter.Register(registry); err != nil {
		return pkgerrs.WrapInvalid(err, "ComponentRegistry", "Register", "JSONFilter processor component registration")
	}

	if err := jsonmap.Register(registry); err != nil {
		return pkgerrs.WrapInvalid(err, "ComponentRegistry", "Register", "JSONMap processor component registration")
	}

	// Storage
	if err := objectstore.Register(registry); err != nil {
		return pkgerrs.WrapInvalid(err, "ComponentRegistry", "Register", "ObjectStore storage component registration")
	}

	// Outputs
	if err := file.Register(registry); err != nil {
		return pkgerrs.WrapInvalid(err, "ComponentRegistry", "Register", "File output component registration")
	}

	if err := httppost.Register(registry); err != nil {
		return pkgerrs.WrapInvalid(err, "ComponentRegistry", "Register", "HTTP POST output component registration")
	}

	if err := websocket.Register(registry); err != nil {
		return pkgerrs.WrapInvalid(err, "ComponentRegistry", "Register", "WebSocket output component registration")
	}

	// Gateways
	if err := gatewayhttp.Register(registry); err != nil {
		return pkgerrs.WrapInvalid(err, "ComponentRegistry", "Register", "HTTP gateway component registration")
	}

	// Lifecycle harness operator gateway (ADR-047 PR 3) — operator
	// HTTP+WebSocket surface over pkg/lifecycle.Manager. Requires
	// deps.LifecycleManager wired at service init; the factory
	// loud-fails at component construction if missing.
	if err := lifecyclegateway.Register(registry); err != nil {
		return pkgerrs.WrapInvalid(err, "ComponentRegistry", "Register", "lifecycle-gateway component registration")
	}

	return nil
}

// registerSemanticLayer registers semantic-layer components (graph and rule processors).
func registerSemanticLayer(registry *component.Registry) error {
	// Graph Components - Core (required for all tiers)
	if err := graphingest.Register(registry); err != nil {
		return pkgerrs.WrapInvalid(err, "ComponentRegistry", "Register", "graph-ingest component registration")
	}

	if err := graphindex.Register(registry); err != nil {
		return pkgerrs.WrapInvalid(err, "ComponentRegistry", "Register", "graph-index component registration")
	}

	if err := graphgateway.Register(registry); err != nil {
		return pkgerrs.WrapInvalid(err, "ComponentRegistry", "Register", "graph-gateway component registration")
	}

	// Query coordinator (orchestrates queries across components)
	if err := graphquery.Register(registry); err != nil {
		return pkgerrs.WrapInvalid(err, "ComponentRegistry", "Register", "graph-query component registration")
	}

	// Statistical/Semantic tier components (enabled via config)
	if err := graphembedding.Register(registry); err != nil {
		return pkgerrs.WrapInvalid(err, "ComponentRegistry", "Register", "graph-embedding component registration")
	}

	if err := graphclustering.Register(registry); err != nil {
		return pkgerrs.WrapInvalid(err, "ComponentRegistry", "Register", "graph-clustering component registration")
	}

	// Optional index components (enabled via config)
	if err := graphindexspatial.Register(registry); err != nil {
		return pkgerrs.WrapInvalid(err, "ComponentRegistry", "Register", "graph-index-spatial component registration")
	}

	if err := graphindextemporal.Register(registry); err != nil {
		return pkgerrs.WrapInvalid(err, "ComponentRegistry", "Register", "graph-index-temporal component registration")
	}

	// Rule processor
	if err := rule.Register(registry); err != nil {
		return pkgerrs.WrapInvalid(err, "ComponentRegistry", "Register", "Rule processor component registration")
	}

	// Gated-DAG dispatch executor (ADR-046 Phase 2): dispatches DAG units in
	// dependency order with restart recovery, failure isolation, stall detection.
	if err := gateddagexec.Register(registry); err != nil {
		return pkgerrs.WrapInvalid(err, "ComponentRegistry", "Register", "gated-dag executor component registration")
	}

	return nil
}

// registerAgenticLayer registers agentic-layer components (LLM-powered autonomous agents).
func registerAgenticLayer(registry *component.Registry) error {
	if err := agenticdispatch.Register(registry); err != nil {
		return pkgerrs.WrapInvalid(err, "ComponentRegistry", "Register", "agentic-dispatch component registration")
	}

	if err := agenticgovernance.Register(registry); err != nil {
		return pkgerrs.WrapInvalid(err, "ComponentRegistry", "Register", "agentic-governance component registration")
	}

	if err := agenticmodel.Register(registry); err != nil {
		return pkgerrs.WrapInvalid(err, "ComponentRegistry", "Register", "agentic-model component registration")
	}

	if err := agentictools.Register(registry); err != nil {
		return pkgerrs.WrapInvalid(err, "ComponentRegistry", "Register", "agentic-tools component registration")
	}

	if err := agenticloop.Register(registry); err != nil {
		return pkgerrs.WrapInvalid(err, "ComponentRegistry", "Register", "agentic-loop component registration")
	}

	return nil
}
