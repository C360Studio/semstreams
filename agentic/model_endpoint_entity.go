package agentic

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/c360studio/semstreams/message"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
)

// CategoryModelEndpoint is the message category for the model-endpoint entity
// origin contract. It names the ENTITY type born when the agentic loop registers
// a model registry endpoint in the graph — distinct from any event payload.
// Mirrors CategoryLoopExecution.
const CategoryModelEndpoint = "model_endpoint"

// ModelEndpointMessageType returns the message.Type for the model-endpoint
// entity — key "agentic.model_endpoint.v1". Registered by RegisterPayloads with
// floor control (ADR-103): stamped on CreateEntityRequest.Entity.MessageType
// when WriteModelEndpoints births the endpoint entity at startup, and decodes
// on the fact lane as *ModelEndpointEntity. A model endpoint is a
// config-derived FACT about the world, born once via entity.create.
func ModelEndpointMessageType() message.Type {
	return message.Type{
		Domain:   Domain,
		Category: CategoryModelEndpoint,
		Version:  SchemaVersion,
	}
}

// ModelEndpointEntity is the registered Graphable payload describing one model
// registry endpoint (ADR-103). The fields are the plain values of the
// endpoint configuration — this package does not import model — and every
// triple object is a field.
type ModelEndpointEntity struct {
	Org                    string  `json:"org"`
	Platform               string  `json:"platform"`
	Name                   string  `json:"name"`
	Provider               string  `json:"provider"`
	Model                  string  `json:"model"`
	URL                    string  `json:"url,omitempty"`
	SupportsTools          bool    `json:"supports_tools"`
	MaxTokens              int     `json:"max_tokens,omitempty"`
	InputPricePer1MTokens  float64 `json:"input_price_per_1m_tokens,omitempty"`
	OutputPricePer1MTokens float64 `json:"output_price_per_1m_tokens,omitempty"`
	RequestsPerMinute      int     `json:"requests_per_minute,omitempty"`
}

// EntityID returns the canonical endpoint entity ID, or "" when the identity
// fields cannot form one (graph-ingest rejects an empty ID; a decoded payload
// must never panic the consumer).
func (e *ModelEndpointEntity) EntityID() string {
	id, err := tryModelEndpointEntityID(e.Org, e.Platform, e.Name)
	if err != nil {
		// entity-id-audit:classify intentional-sentinel "" line=59 column=10 surface=go-return:EntityID entity_id_invalid:empty documented model endpoint failure return; graph-ingest rejects an empty ID and a decoded payload must not panic
		return ""
	}
	return id
}

// Triples returns the full set of triples describing the endpoint, byte-identical
// to the former processor/agentic-loop builder: provider, name, and
// supports-tools always; max-tokens, input-price, output-price, endpoint-url,
// and rate-limit only when their zero value carries no information. Objects
// keep their Go types (bool, int, float64, string).
func (e *ModelEndpointEntity) Triples() []message.Triple {
	entityID := e.EntityID()
	now := time.Now()
	triple := func(predicate string, object any) message.Triple {
		return message.Triple{
			Subject:    entityID,
			Predicate:  predicate,
			Object:     object,
			Source:     loopExecutionSource,
			Timestamp:  now,
			Confidence: 1.0,
		}
	}

	triples := []message.Triple{
		triple(agvocab.ModelProvider, e.Provider),
		triple(agvocab.ModelName, e.Model),
		triple(agvocab.ModelSupportsTools, e.SupportsTools),
	}

	if e.MaxTokens > 0 {
		triples = append(triples, triple(agvocab.ModelMaxTokens, e.MaxTokens))
	}
	if e.InputPricePer1MTokens > 0 {
		triples = append(triples, triple(agvocab.ModelInputPrice, e.InputPricePer1MTokens))
	}
	if e.OutputPricePer1MTokens > 0 {
		triples = append(triples, triple(agvocab.ModelOutputPrice, e.OutputPricePer1MTokens))
	}
	if e.URL != "" {
		triples = append(triples, triple(agvocab.ModelEndpointURL, e.URL))
	}
	if e.RequestsPerMinute > 0 {
		triples = append(triples, triple(agvocab.ModelRateLimit, e.RequestsPerMinute))
	}

	return triples
}

// Schema implements message.Payload.
func (e *ModelEndpointEntity) Schema() message.Type {
	return ModelEndpointMessageType()
}

// Validate implements message.Payload and IS the endpoint writer's contract —
// derived from, and on the JSON-reachable domain no stronger than, the config
// owner's validateEndpoint (model/registry.go): model required, max_tokens
// and both prices non-negative (the NaN/Inf half of the price guard is
// unreachable from JSON config). Provider is deliberately NOT required — the
// registry permits an empty provider (registry.go:533 reads
// `ep.Provider != "" && !validProviders[...]`) and AdapterFor("") returns the
// generic adapter — and RequestsPerMinute is deliberately unchecked,
// mirroring its absence upstream: a payload contract stronger than its writer
// converts a boot-accepted config into a silently missing graph entity. The
// closed provider vocabulary stays the config owner's; this contract does not
// carry it.
// BaseMessage.MarshalJSON refuses a payload that fails this;
// WriteModelEndpoints delegates here before birthing an endpoint.
func (e *ModelEndpointEntity) Validate() error {
	if _, err := tryModelEndpointEntityID(e.Org, e.Platform, e.Name); err != nil {
		return err
	}
	if e.Model == "" {
		return errors.New("model is required and must be a non-empty string")
	}
	if e.MaxTokens < 0 {
		return fmt.Errorf("max_tokens must not be negative, got %d", e.MaxTokens)
	}
	for _, price := range []struct {
		name  string
		value float64
	}{{"input_price_per_1m_tokens", e.InputPricePer1MTokens}, {"output_price_per_1m_tokens", e.OutputPricePer1MTokens}} {
		if math.IsNaN(price.value) || math.IsInf(price.value, 0) || price.value < 0 {
			return fmt.Errorf("%s must be a finite non-negative number, got %g", price.name, price.value)
		}
	}
	return nil
}

// MarshalJSON implements json.Marshaler with the alias idiom.
func (e *ModelEndpointEntity) MarshalJSON() ([]byte, error) {
	type alias ModelEndpointEntity
	return json.Marshal((*alias)(e))
}

// UnmarshalJSON implements json.Unmarshaler with the alias idiom.
func (e *ModelEndpointEntity) UnmarshalJSON(data []byte) error {
	type alias ModelEndpointEntity
	return json.Unmarshal(data, (*alias)(e))
}
