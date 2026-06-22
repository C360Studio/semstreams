// Package inference provides structural anomaly detection for missing relationships.
package inference

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
)

// RelationshipApplier creates new relationships from approved anomaly suggestions.
type RelationshipApplier interface {
	// ApplyRelationship publishes a new relationship triple to the entity stream.
	ApplyRelationship(ctx context.Context, suggestion *RelationshipSuggestion) error
}

// NATSRelationshipApplier publishes relationship triples to the entity stream.
// The normal ingestion pipeline then indexes the new relationships.
type NATSRelationshipApplier struct {
	js      jetstream.JetStream
	subject string
	logger  *slog.Logger
}

// NewNATSRelationshipApplier creates a new applier that publishes to NATS.
func NewNATSRelationshipApplier(
	js jetstream.JetStream,
	subject string,
	logger *slog.Logger,
) *NATSRelationshipApplier {
	if logger == nil {
		logger = slog.Default()
	}
	return &NATSRelationshipApplier{
		js:      js,
		subject: subject,
		logger:  logger,
	}
}

// inferredTriple represents the message format for inferred relationships.
// This matches the standard triple format used by the ingestion pipeline.
type inferredTriple struct {
	Subject   string `json:"subject"`
	Predicate string `json:"predicate"`
	Object    string `json:"object"`
	Context   string `json:"context,omitempty"`
}

// ApplyRelationship publishes a new relationship triple to the entity stream.
func (a *NATSRelationshipApplier) ApplyRelationship(
	ctx context.Context,
	suggestion *RelationshipSuggestion,
) error {
	if suggestion == nil {
		return errs.WrapInvalid(nil, "RelationshipApplier", "ApplyRelationship",
			"suggestion is nil")
	}

	if suggestion.FromEntity == "" || suggestion.ToEntity == "" {
		return errs.WrapInvalid(nil, "RelationshipApplier", "ApplyRelationship",
			"from_entity and to_entity are required")
	}

	if suggestion.Predicate == "" {
		return errs.WrapInvalid(nil, "RelationshipApplier", "ApplyRelationship",
			"predicate is required")
	}

	// Build triple message
	triple := &inferredTriple{
		Subject:   suggestion.FromEntity,
		Predicate: suggestion.Predicate,
		Object:    suggestion.ToEntity,
		Context:   "inference.structural", // Mark source as structural inference
	}

	// Serialize
	data, err := json.Marshal(triple)
	if err != nil {
		return errs.WrapInvalid(err, "RelationshipApplier", "ApplyRelationship",
			"failed to serialize triple")
	}

	// Publish to entity stream
	_, err = a.js.Publish(ctx, a.subject, data)
	if err != nil {
		return errs.WrapTransient(err, "RelationshipApplier", "ApplyRelationship",
			fmt.Sprintf("failed to publish triple: %s -> %s -> %s",
				suggestion.FromEntity, suggestion.Predicate, suggestion.ToEntity))
	}

	a.logger.Debug("Applied inferred relationship",
		"from", suggestion.FromEntity,
		"predicate", suggestion.Predicate,
		"to", suggestion.ToEntity,
		"confidence", suggestion.Confidence)

	return nil
}

// NoOpApplier is a no-op implementation for testing or disabled mode.
type NoOpApplier struct {
	logger *slog.Logger
}

// NewNoOpApplier creates an applier that logs but doesn't persist.
func NewNoOpApplier(logger *slog.Logger) *NoOpApplier {
	if logger == nil {
		logger = slog.Default()
	}
	return &NoOpApplier{logger: logger}
}

// ApplyRelationship logs the suggestion but doesn't persist it.
func (a *NoOpApplier) ApplyRelationship(
	_ context.Context,
	suggestion *RelationshipSuggestion,
) error {
	a.logger.Debug("NoOpApplier: would apply relationship",
		"from", suggestion.FromEntity,
		"predicate", suggestion.Predicate,
		"to", suggestion.ToEntity,
		"confidence", suggestion.Confidence)
	return nil
}

// TripleAdder defines the interface for adding triples directly to the graph.
// This interface is satisfied by DataManager.
type TripleAdder interface {
	AddTriple(ctx context.Context, triple message.Triple) error
}

// DirectRelationshipApplier adds triples directly via DataManager.
// Used for intra-processor mutations, matching the community inference pattern.
// This avoids NATS roundtrips for operations within the same processor.
type DirectRelationshipApplier struct {
	adder  TripleAdder
	logger *slog.Logger
}

// NewDirectRelationshipApplier creates an applier that uses DataManager directly.
func NewDirectRelationshipApplier(adder TripleAdder, logger *slog.Logger) *DirectRelationshipApplier {
	if logger == nil {
		logger = slog.Default()
	}
	return &DirectRelationshipApplier{
		adder:  adder,
		logger: logger,
	}
}

// ApplyRelationship adds a triple directly via DataManager.
func (a *DirectRelationshipApplier) ApplyRelationship(
	ctx context.Context,
	suggestion *RelationshipSuggestion,
) error {
	if suggestion == nil {
		return errs.WrapInvalid(errs.ErrInvalidData, "DirectRelationshipApplier", "ApplyRelationship",
			"suggestion is nil")
	}

	if suggestion.FromEntity == "" || suggestion.ToEntity == "" {
		return errs.WrapInvalid(errs.ErrInvalidData, "DirectRelationshipApplier", "ApplyRelationship",
			"from_entity and to_entity are required")
	}

	if suggestion.Predicate == "" {
		return errs.WrapInvalid(errs.ErrInvalidData, "DirectRelationshipApplier", "ApplyRelationship",
			"predicate is required")
	}

	// Build triple using message.Triple (same pattern as community inference)
	triple := message.Triple{
		Subject:    suggestion.FromEntity,
		Predicate:  suggestion.Predicate,
		Object:     suggestion.ToEntity,
		Source:     "inference.structural",
		Timestamp:  time.Now(),
		Confidence: suggestion.Confidence,
		Context:    "auto-applied",
	}

	if err := a.adder.AddTriple(ctx, triple); err != nil {
		return errs.WrapTransient(err, "DirectRelationshipApplier", "ApplyRelationship",
			fmt.Sprintf("failed to add triple: %s -> %s -> %s",
				suggestion.FromEntity, suggestion.Predicate, suggestion.ToEntity))
	}

	a.logger.Debug("Applied inferred relationship",
		"from", suggestion.FromEntity,
		"predicate", suggestion.Predicate,
		"to", suggestion.ToEntity,
		"confidence", suggestion.Confidence)

	return nil
}

// MutationRelationshipApplier sends triple add requests to graph-ingest via NATS request/reply.
// This is the preferred applier for cross-processor mutations as it goes through the
// proper ingestion pipeline (graph.mutation.triple.add -> graph-ingest -> indexing).
type MutationRelationshipApplier struct {
	natsClient *natsclient.Client
	logger     *slog.Logger
}

// NewMutationRelationshipApplier creates an applier that uses the graph-ingest mutation API.
func NewMutationRelationshipApplier(natsClient *natsclient.Client, logger *slog.Logger) *MutationRelationshipApplier {
	if logger == nil {
		logger = slog.Default()
	}
	return &MutationRelationshipApplier{
		natsClient: natsClient,
		logger:     logger,
	}
}

// ApplyRelationship sends an add triple request to graph-ingest.
func (a *MutationRelationshipApplier) ApplyRelationship(
	ctx context.Context,
	suggestion *RelationshipSuggestion,
) error {
	if suggestion == nil {
		return errs.WrapInvalid(errs.ErrInvalidData, "MutationRelationshipApplier", "ApplyRelationship",
			"suggestion is nil")
	}

	if suggestion.FromEntity == "" || suggestion.ToEntity == "" {
		return errs.WrapInvalid(errs.ErrInvalidData, "MutationRelationshipApplier", "ApplyRelationship",
			"from_entity and to_entity are required")
	}

	if suggestion.Predicate == "" {
		return errs.WrapInvalid(errs.ErrInvalidData, "MutationRelationshipApplier", "ApplyRelationship",
			"predicate is required")
	}

	// Build triple using message.Triple
	triple := message.Triple{
		Subject:    suggestion.FromEntity,
		Predicate:  suggestion.Predicate,
		Object:     suggestion.ToEntity,
		Source:     "inference.semantic",
		Timestamp:  time.Now(),
		Confidence: suggestion.Confidence,
		Context:    "auto-applied",
	}

	req := graph.AddTripleRequest{Triple: triple}
	data, err := json.Marshal(req)
	if err != nil {
		return errs.WrapInvalid(err, "MutationRelationshipApplier", "ApplyRelationship",
			"failed to serialize request")
	}

	// RequestWithRetryClassified handles transient "no responders" errors when
	// graph-gateway is restarting or its subscription hasn't yet
	// propagated. Inference-emitted triples are idempotent on the
	// responder side (same triple twice = same graph state), so
	// retry is safe and avoids silently dropping inferred edges.
	// gh#93 Phase 2: Classified variant surfaces handler errors via err
	// rather than silently mis-decoding legacy "error: " body as success JSON.
	resp, err := a.natsClient.RequestWithRetryClassified(ctx, "graph.mutation.triple.add", data, 5*time.Second, natsclient.DefaultRetryConfig())
	if err != nil {
		return errs.WrapTransient(err, "MutationRelationshipApplier", "ApplyRelationship",
			fmt.Sprintf("mutation request failed: %s -> %s -> %s",
				suggestion.FromEntity, suggestion.Predicate, suggestion.ToEntity))
	}

	var result graph.AddTripleResponse
	if err := json.Unmarshal(resp, &result); err != nil {
		return errs.WrapInvalid(err, "MutationRelationshipApplier", "ApplyRelationship",
			"failed to parse response")
	}

	if !result.Success {
		return errors.New(result.Error)
	}

	a.logger.Debug("Applied inferred relationship via mutation API",
		"from", suggestion.FromEntity,
		"predicate", suggestion.Predicate,
		"to", suggestion.ToEntity,
		"confidence", suggestion.Confidence)

	return nil
}
