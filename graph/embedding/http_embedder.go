package embedding

import (
	"context"
	"fmt"
	"log/slog"
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
	client      *openai.Client
	model       string
	dimensions  int
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

	return &HTTPEmbedder{
		client:      client,
		model:       cfg.Model,
		dimensions:  384, // Will be detected on first call
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

			// Update dimensions on first call
			if h.dimensions == 384 && len(data.Embedding) > 0 {
				h.dimensions = len(data.Embedding)
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

// Dimensions returns the dimensionality of embeddings produced.
func (h *HTTPEmbedder) Dimensions() int {
	return h.dimensions
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
