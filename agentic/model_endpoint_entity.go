package agentic

import "github.com/c360studio/semstreams/message"

// CategoryModelEndpoint is the message category for the model-endpoint entity
// origin contract. It names the ENTITY type born when the agentic loop registers
// a model registry endpoint in the graph — distinct from any event payload.
// Mirrors CategoryLoopExecution.
const CategoryModelEndpoint = "model_endpoint"

// ModelEndpointMessageType returns the message.Type for the model-endpoint
// entity origin contract — key "agentic.model_endpoint.v1".
//
// Registry decision (mirrors LoopExecutionMessageType, ADR-056 typed-origin —
// intentionally pinned, not implicit): this type is MUTATION-ONLY. It is stamped
// on CreateEntityRequest.Entity.MessageType when WriteModelEndpoints births the
// endpoint entity, purely as producer identity; it is NEVER published as a BaseMessage payload and is therefore
// NOT registered in the payload registry (payload_registry.go) and never
// round-trips through payload decoding. A model endpoint is a config-derived
// FACT about the world, born once at startup via entity.create — not a
// wire message. If a future producer needs to PUBLISH a model_endpoint message
// over NATS (not merely stamp it at create), that producer must add the init()
// registration at that point — until then, registering it would advertise a
// decode path that does not exist.
func ModelEndpointMessageType() message.Type {
	return message.Type{
		Domain:   Domain,
		Category: CategoryModelEndpoint,
		Version:  SchemaVersion,
	}
}
