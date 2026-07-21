package embedding

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/c360studio/semstreams/model"
	"github.com/sashabaranov/go-openai"
)

// HTTPEmbedder calls an external OpenAI-compatible embedding service via HTTP.
//
// This implementation works with:
//   - Hugging Face TEI (Text Embeddings Inference) - recommended, containerized
//   - LocalAI (self-hosted)
//   - OpenAI (cloud)
//   - Any OpenAI-compatible embedding API
//
// Uses the standard OpenAI SDK for consistency and compatibility.
// See Dockerfile.tei and docker-compose.services.yml for ready-to-use TEI setup.
type HTTPEmbedder struct {
	client *openai.Client
	model  string

	// dimensions is the vector width this endpoint actually returns, discovered
	// from the first successful response and latched thereafter. 0 means NOT YET
	// RESOLVED and is reported as such by Dimensions().
	//
	// It is atomic because the writer (a Generate call on some worker goroutine)
	// and the readers are always on different goroutines: the component's
	// embedderIdentity() runs on the hop-1 ENTITY_STATES watcher, and the hop-2
	// worker pool is 5 goroutines by default.
	//
	// The zero sentinel replaced a hardcoded 384 placeholder that was overwritten
	// on the first response via `if dimensions == 384`. That could not tell
	// "undetected" from "genuinely 384", and for any other width it split the
	// dedup keyspace in two — entities queued before the first response keyed on
	// 384, entities after on the real width, with the boundary set by race timing.
	// Dimensions are resolved lazily rather than at construction so that a
	// temporarily unreachable endpoint degrades the component instead of failing
	// its Start.
	dimensions atomic.Int64

	cache       Cache
	logger      *slog.Logger
	queryPrefix string // prepended to query-side text only (gh#438)
}

// HTTPConfig configures the HTTP embedder.
type HTTPConfig struct {
	// BaseURL is the base URL of the embedding service.
	// Examples:
	//   - "http://localhost:8082" (TEI - Hugging Face Text Embeddings Inference)
	//   - "http://tei:8082" (TEI container by name)
	//   - "http://localhost:8080" (LocalAI)
	//   - "https://api.openai.com/v1" (OpenAI cloud)
	BaseURL string

	// Model is the embedding model to use.
	// Examples:
	//   - "all-MiniLM-L6-v2" (TEI default - 384 dims, fast)
	//   - "all-mpnet-base-v2" (TEI - 768 dims, higher quality)
	//   - "text-embedding-ada-002" (OpenAI)
	Model string

	// APIKey for authentication (optional for local services).
	// Required for OpenAI, optional for TEI/LocalAI.
	APIKey string

	// QueryPrefix is prepended to query-side text only (GenerateQuery), never to
	// documents. Set it to the model's query instruction for asymmetric retrieval
	// models — e.g. "Represent this sentence for searching relevant passages: " for
	// Snowflake arctic-embed / E5 (gh#438). Empty disables the prefix (symmetric use).
	QueryPrefix string

	// Timeout for HTTP requests (default: 30s).
	Timeout time.Duration

	// Cache for embedding results (optional but recommended).
	Cache Cache

	// Logger for error logging (optional, defaults to slog.Default()).
	Logger *slog.Logger

	// IdleConnTimeout, ResponseHeaderTimeout, and DisableKeepAlives
	// mirror the same fields on model.EndpointConfig and graph/llm's
	// OpenAIConfig — operator overrides for connection-hygiene
	// behaviour. Empty/false selects the framework default (see
	// model.NewHTTPClient).
	IdleConnTimeout       string
	ResponseHeaderTimeout string
	DisableKeepAlives     bool
}

// NewHTTPEmbedder creates a new HTTP-based embedder.
func NewHTTPEmbedder(cfg HTTPConfig) (*HTTPEmbedder, error) {
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("base_url is required")
	}
	if cfg.Model == "" {
		return nil, fmt.Errorf("model is required")
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	// Create OpenAI client config
	apiKey := cfg.APIKey
	if apiKey == "" {
		apiKey = "dummy-key" // Local services don't need real key
	}

	config := openai.DefaultConfig(apiKey)
	config.BaseURL = cfg.BaseURL
	// Route through the framework's canonical HTTP client builder so
	// embedding traffic gets the same connection-hygiene defaults
	// (HTTP/2 PINGs, tightened idle pool, etc.) as agentic-model and
	// graph/llm. Beta.43 architectural invariant: every LLM-class
	// HTTP client routes through model.NewHTTPClient.
	config.HTTPClient = model.NewHTTPClient(model.HTTPClientOptions{
		Timeout:               timeout,
		IdleConnTimeout:       cfg.IdleConnTimeout,
		ResponseHeaderTimeout: cfg.ResponseHeaderTimeout,
		DisableKeepAlives:     cfg.DisableKeepAlives,
	})

	client := openai.NewClientWithConfig(config)

	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	// dimensions is left at its zero value: unresolved until the first response.
	return &HTTPEmbedder{
		client:      client,
		model:       cfg.Model,
		cache:       cfg.Cache,
		logger:      logger,
		queryPrefix: cfg.QueryPrefix,
	}, nil
}

// Generate creates DOCUMENT-side embeddings by calling the external HTTP service.
//
// This method checks the cache first (if configured), then calls the
// embedding API for any cache misses.
func (h *HTTPEmbedder) Generate(ctx context.Context, texts []string) ([][]float32, error) {
	return h.embed(ctx, texts)
}

// GenerateQuery creates QUERY-side embeddings, prepending the configured query
// instruction prefix to each text (asymmetric retrieval models, gh#438). With no
// prefix configured it is identical to Generate. The cache keys on the prefixed
// text, so query and document embeddings of the same string never collide.
func (h *HTTPEmbedder) GenerateQuery(ctx context.Context, texts []string) ([][]float32, error) {
	if h.queryPrefix == "" {
		return h.embed(ctx, texts)
	}
	prefixed := make([]string, len(texts))
	for i, t := range texts {
		prefixed[i] = h.queryPrefix + t
	}
	return h.embed(ctx, prefixed)
}

// embed is the shared cache-then-API path used by both Generate and GenerateQuery.
func (h *HTTPEmbedder) embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return [][]float32{}, nil
	}

	// Track which texts need API calls
	embeddings := make([][]float32, len(texts))
	uncachedIndexes := []int{}
	uncachedTexts := []string{}

	// Check cache for each text (if cache enabled)
	if h.cache != nil {
		for i, text := range texts {
			hash := ContentHash(text)
			if cached, err := h.cache.Get(ctx, hash); err == nil {
				embeddings[i] = cached
			} else {
				uncachedIndexes = append(uncachedIndexes, i)
				uncachedTexts = append(uncachedTexts, text)
			}
		}
	} else {
		// No cache - all texts need API call
		uncachedIndexes = make([]int, len(texts))
		for i := range texts {
			uncachedIndexes[i] = i
		}
		uncachedTexts = texts
	}

	// Call API for uncached texts
	if len(uncachedTexts) > 0 {
		req := openai.EmbeddingRequest{
			Input: uncachedTexts,
			Model: openai.EmbeddingModel(h.model),
		}

		resp, err := h.client.CreateEmbeddings(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("embedding API call failed: %w", err)
		}

		if len(resp.Data) != len(uncachedTexts) {
			return nil, fmt.Errorf("API returned %d embeddings for %d texts", len(resp.Data), len(uncachedTexts))
		}

		// Store results and update cache
		for i, data := range resp.Data {
			originalIndex := uncachedIndexes[i]
			embeddings[originalIndex] = data.Embedding

			// Latch the real vector width on the first response that carries one.
			// CompareAndSwap makes this resolve-once: later responses cannot churn
			// the value, so every dedup key derived after resolution is stable.
			if len(data.Embedding) > 0 {
				if h.dimensions.CompareAndSwap(0, int64(len(data.Embedding))) {
					h.logger.Info("resolved embedding dimensions from endpoint",
						"model", h.model, "dimensions", len(data.Embedding))
				}
			}

			// Cache the embedding
			if h.cache != nil {
				hash := ContentHash(uncachedTexts[i])
				if err := h.cache.Put(ctx, hash, data.Embedding); err != nil {
					// Log but don't fail - cache is best-effort
					h.logger.Warn("embedding cache put failed", "hash", hash, "error", err)
				}
			}
		}
	}

	return embeddings, nil
}

// Dimensions returns the dimensionality of embeddings produced, or 0 if the
// endpoint has not answered yet.
//
// 0 is reported honestly rather than guessed. Callers that key durable state on
// the vector space (DedupKey) must refuse to build a key from an unresolved
// identity; inventing a width there is what partitioned EMBEDDING_DEDUP.
func (h *HTTPEmbedder) Dimensions() int {
	return int(h.dimensions.Load())
}

// Model returns the model identifier.
func (h *HTTPEmbedder) Model() string {
	return h.model
}

// Close releases resources (no-op for HTTP client).
func (h *HTTPEmbedder) Close() error {
	// HTTP client has no resources to release
	return nil
}
