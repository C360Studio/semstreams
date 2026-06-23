// Package llm provides LLM client and prompt templates for graph processing.
package llm

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/text"
	"github.com/c360studio/semstreams/storage/objectstore"
)

// NATSContentFetcher implements ContentFetcher using NATS request/reply
// to fetch content from ObjectStore via its API subject.
type NATSContentFetcher struct {
	natsClient *natsclient.Client
	subject    string
	timeout    time.Duration
	logger     *slog.Logger

	// Abstract fallback configuration
	abstractFallback bool // Generate abstract from body if abstract role is empty
	abstractMaxChars int  // Max chars for generated abstract (default: 250)
}

// ContentFetcherOption configures a NATSContentFetcher.
// Options return errors for validation (following natsclient pattern).
type ContentFetcherOption func(*NATSContentFetcher) error

// WithContentSubject sets the ObjectStore API subject.
// Default: "storage.objectstore.api"
func WithContentSubject(subject string) ContentFetcherOption {
	return func(f *NATSContentFetcher) error {
		if subject == "" {
			return errors.New("content subject cannot be empty")
		}
		f.subject = subject
		return nil
	}
}

// WithContentTimeout sets the timeout for content fetch requests.
// Default: 2 seconds
func WithContentTimeout(timeout time.Duration) ContentFetcherOption {
	return func(f *NATSContentFetcher) error {
		if timeout <= 0 {
			timeout = 2 * time.Second
		}
		f.timeout = timeout
		return nil
	}
}

// WithContentLogger sets the logger for the content fetcher.
// Default: slog.Default()
func WithContentLogger(logger *slog.Logger) ContentFetcherOption {
	return func(f *NATSContentFetcher) error {
		if logger != nil {
			f.logger = logger
		}
		return nil
	}
}

// WithAbstractFallback configures abstract generation from body text.
// When enabled and abstract role is empty, extracts first N chars from body.
// Default: enabled=true, maxChars=250
func WithAbstractFallback(enabled bool, maxChars int) ContentFetcherOption {
	return func(f *NATSContentFetcher) error {
		f.abstractFallback = enabled
		if maxChars <= 0 {
			maxChars = 250
		}
		f.abstractMaxChars = maxChars
		return nil
	}
}

// NewNATSContentFetcher creates a new ContentFetcher that uses NATS request/reply
// to fetch content from ObjectStore.
func NewNATSContentFetcher(
	natsClient *natsclient.Client,
	opts ...ContentFetcherOption,
) (*NATSContentFetcher, error) {
	if natsClient == nil {
		return nil, errors.New("natsClient is required")
	}

	f := &NATSContentFetcher{
		natsClient:       natsClient,
		subject:          "storage.objectstore.api",
		timeout:          2 * time.Second,
		logger:           slog.Default(),
		abstractFallback: true, // Enabled by default
		abstractMaxChars: 250,  // Default 250 chars
	}

	for _, opt := range opts {
		if err := opt(f); err != nil {
			return nil, err
		}
	}

	return f, nil
}

// FetchEntityContent retrieves title and abstract for entities with StorageRefs.
// Uses ObjectStore's "get" action to fetch StoredContent, then extracts
// title and abstract fields. This is where LLM-specific filtering happens,
// keeping ObjectStore generic.
//
// Returns partial results - entities without StorageRef or fetch errors are skipped.
func (f *NATSContentFetcher) FetchEntityContent(
	ctx context.Context,
	entities []*gtypes.EntityState,
) (map[string]*EntityContent, error) {
	result := make(map[string]*EntityContent)

	for _, entity := range entities {
		// Skip entities without storage reference
		if entity.StorageRef == nil {
			continue
		}

		content, err := f.fetchSingleEntity(ctx, entity)
		if err != nil {
			f.logger.Debug("Content fetch failed",
				"entity_id", entity.ID,
				"key", entity.StorageRef.Key,
				"error", err)
			continue
		}

		if content != nil {
			result[entity.ID] = content
		}
	}

	if len(result) > 0 {
		f.logger.Debug("Fetched entity content",
			"fetched_count", len(result),
			"total_entities", len(entities))
	}

	return result, nil
}

// fetchSingleEntity fetches content for a single entity via NATS request/reply.
func (f *NATSContentFetcher) fetchSingleEntity(
	ctx context.Context,
	entity *gtypes.EntityState,
) (*EntityContent, error) {
	// Build request for ObjectStore "get" action
	req := objectstore.Request{
		Action: "get",
		Key:    entity.StorageRef.Key,
	}
	reqData, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	// NATS request/reply with timeout. RequestClassified surfaces a
	// transport/handler failure via err (ADR-060) instead of an "error: <msg>"
	// body that would mis-decode as a zero objectstore.Response. The Success
	// field below is the ObjectStore envelope's own contract, distinct from the
	// graph mutation lane — kept.
	respData, err := f.natsClient.RequestClassified(ctx, f.subject, reqData, f.timeout)
	if err != nil {
		return nil, err
	}

	// Parse ObjectStore response
	var resp objectstore.Response
	if err := json.Unmarshal(respData, &resp); err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, errors.New("objectstore request failed")
	}

	// Parse StoredContent from response data
	var stored objectstore.StoredContent
	if err := json.Unmarshal(resp.Data, &stored); err != nil {
		return nil, err
	}

	// Extract title and abstract using semantic role mapping
	// This is where LLM-specific filtering happens (not in ObjectStore)
	title := stored.GetFieldByRole("title")
	abstract := stored.GetFieldByRole("abstract")

	// Abstract fallback: generate from body if abstract is empty
	if abstract == "" && f.abstractFallback {
		body := stored.GetFieldByRole("body")
		if body != "" {
			abstract = text.TruncateAtWord(body, f.abstractMaxChars)
			f.logger.Debug("Generated abstract from body",
				"entity_id", entity.ID,
				"body_len", len(body),
				"abstract_len", len(abstract))
		}
	}

	// Only return if we have at least one useful field
	if title == "" && abstract == "" {
		return nil, nil
	}

	return &EntityContent{
		Title:    title,
		Abstract: abstract,
	}, nil
}

// Ensure NATSContentFetcher implements ContentFetcher
var _ ContentFetcher = (*NATSContentFetcher)(nil)
