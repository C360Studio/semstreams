package embedding

import (
	"context"
	"fmt"
	"hash/fnv"
	"math"
	"strings"
	"sync"
	"unicode"
)

// BM25Embedder implements pure Go lexical embeddings using BM25 algorithm.
//
// This embedder provides a fallback when neural embedding services are unavailable.
// It uses BM25 (Best Matching 25) scoring - a term-frequency based ranking function
// widely used in information retrieval.
//
// The embedder generates fixed-dimension vectors by:
//  1. Tokenizing text (lowercase, split on non-alphanumeric)
//  2. Computing term frequencies
//  3. Hashing terms to fixed dimensions (feature hashing)
//  4. Applying BM25 weighting (TF with IDF and length normalization)
//  5. L2 normalizing for cosine similarity compatibility
//
// Parameters:
//   - k1: Controls term frequency saturation (default 1.5)
//   - b: Controls document length normalization (default 0.75)
//
// This is a lexical approach - it won't understand semantic similarity like neural
// models, but provides reasonable results for exact term matches and common phrases.
type BM25Embedder struct {
	dimensions int
	k1         float64 // Term frequency saturation parameter (typically 1.2-2.0)
	b          float64 // Length normalization parameter (typically 0.75)

	// Document statistics
	mu             sync.RWMutex
	docCount       int
	avgDocLength   float64
	termDocCount   map[string]int // Number of documents containing each term
	totalDocLength int
}

// BM25Config configures the BM25 embedder.
type BM25Config struct {
	// Dimensions is the output embedding dimension (default: 384 for compatibility)
	Dimensions int

	// K1 controls term frequency saturation (default: 1.5)
	// Higher values give more weight to term frequency
	K1 float64

	// B controls length normalization (default: 0.75)
	// B=1.0 means full normalization, B=0.0 means no normalization
	B float64
}

// NewBM25Embedder creates a new BM25-based embedder.
func NewBM25Embedder(cfg BM25Config) *BM25Embedder {
	if cfg.Dimensions == 0 {
		cfg.Dimensions = 384 // Match common neural embedding models
	}
	if cfg.K1 == 0 {
		cfg.K1 = 1.5 // Standard BM25 default
	}
	if cfg.B == 0 {
		cfg.B = 0.75 // Standard BM25 default
	}

	return &BM25Embedder{
		dimensions:   cfg.Dimensions,
		k1:           cfg.K1,
		b:            cfg.B,
		termDocCount: make(map[string]int),
	}
}

// Generate creates BM25-based embeddings for the given texts.
//
// This updates internal document statistics incrementally, so the embedder
// "learns" vocabulary and IDF scores from all texts it processes.
func (b *BM25Embedder) Generate(ctx context.Context, texts []string) ([][]float32, error) {
	// Check for cancellation before expensive operation
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	if len(texts) == 0 {
		return [][]float32{}, nil
	}

	embeddings := make([][]float32, len(texts))

	// First pass: tokenize all texts and prepare term frequencies
	type docInfo struct {
		tokens   []string
		termFreq map[string]int
	}
	docs := make([]docInfo, len(texts))

	for i, text := range texts {
		// Check for cancellation periodically
		if i%100 == 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}
		}

		tokens := b.tokenize(text)
		docs[i] = docInfo{
			tokens:   tokens,
			termFreq: b.computeTermFrequencies(tokens),
		}
	}

	// Second pass: compute embeddings and update stats incrementally
	for i, doc := range docs {
		// Check for cancellation periodically
		if i%100 == 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}
		}
		if len(doc.tokens) == 0 {
			// Empty document - return zero vector
			embeddings[i] = make([]float32, b.dimensions)
			continue
		}

		// Compute embedding with current statistics
		embedding := b.computeBM25Vector(doc.termFreq, len(doc.tokens))
		embeddings[i] = embedding

		// Update statistics for next iteration
		b.updateStats(doc.tokens)
	}

	return embeddings, nil
}

// Dimensions returns the dimensionality of embeddings.
func (b *BM25Embedder) Dimensions() int {
	return b.dimensions
}

// Model returns the model identifier.
func (b *BM25Embedder) Model() string {
	return fmt.Sprintf("bm25-go-k%.1f-b%.2f", b.k1, b.b)
}

// Close releases resources (no-op for BM25).
func (b *BM25Embedder) Close() error {
	return nil
}

// tokenize splits text into lowercase tokens.
//
// Simple tokenization: lowercase, split on non-alphanumeric, filter short tokens.
func (b *BM25Embedder) tokenize(text string) []string {
	// Lowercase
	text = strings.ToLower(text)

	// Split on non-alphanumeric
	var tokens []string
	var current strings.Builder

	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			_, _ = current.WriteRune(r)
		} else if current.Len() > 0 {
			token := current.String()
			if len(token) >= 2 { // Filter very short tokens
				tokens = append(tokens, token)
			}
			current.Reset()
		}
	}

	// Don't forget last token
	if current.Len() > 0 {
		token := current.String()
		if len(token) >= 2 {
			tokens = append(tokens, token)
		}
	}

	return tokens
}

// computeTermFrequencies counts term occurrences.
func (b *BM25Embedder) computeTermFrequencies(tokens []string) map[string]int {
	termFreq := make(map[string]int)
	for _, token := range tokens {
		termFreq[token]++
	}
	return termFreq
}

// updateStats updates document statistics incrementally.
func (b *BM25Embedder) updateStats(tokens []string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Update document count
	b.docCount++

	// Update total document length for average calculation
	docLength := len(tokens)
	b.totalDocLength += docLength
	b.avgDocLength = float64(b.totalDocLength) / float64(b.docCount)

	// Update term document counts (count each term once per document)
	seen := make(map[string]bool)
	for _, token := range tokens {
		if !seen[token] {
			b.termDocCount[token]++
			seen[token] = true
		}
	}
}

// computeBM25Vector generates embedding vector using BM25 scoring.
//
// Uses feature hashing to map terms to fixed dimensions, then applies
// BM25 weighting to each dimension.
func (b *BM25Embedder) computeBM25Vector(termFreq map[string]int, docLength int) []float32 {
	vector := make([]float32, b.dimensions)

	b.mu.RLock()
	defer b.mu.RUnlock()

	// Compute BM25 score for each term and accumulate into hashed dimensions
	for term, tf := range termFreq {
		// Hash term to dimension
		dim := b.hashTerm(term)

		// Compute IDF (inverse document frequency)
		// Use default IDF of 1.0 if we have no document statistics yet
		var idf float64
		if b.docCount == 0 {
			idf = 1.0 // Default for first document
		} else {
			df := b.termDocCount[term]
			if df == 0 {
				df = 1 // Smoothing for unseen terms
			}
			// BM25 IDF formula with Robertson-Sparck Jones weighting
			idf = math.Log((float64(b.docCount-df) + 0.5) / (float64(df) + 0.5))
			if idf < 0.01 {
				idf = 0.01 // Small positive value instead of zero
			}
		}

		// Compute BM25 term score
		// BM25(t,d) = IDF(t) * (tf * (k1 + 1)) / (tf + k1 * (1 - b + b * (|d| / avgdl)))
		numerator := float64(tf) * (b.k1 + 1)

		// Handle avgDocLength = 0 case (first document)
		avgDocLen := b.avgDocLength
		if avgDocLen == 0 {
			avgDocLen = float64(docLength) // Use current doc length as average
		}

		denominator := float64(tf) + b.k1*(1-b.b+b.b*(float64(docLength)/avgDocLen))
		bm25Score := idf * (numerator / denominator)

		// Accumulate score to hashed dimension
		vector[dim] += float32(bm25Score)
	}

	// L2 normalize for cosine similarity compatibility
	b.l2Normalize(vector)

	return vector
}

// hashTerm maps a term to a dimension using FNV-1a hash.
func (b *BM25Embedder) hashTerm(term string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(term))
	return int(h.Sum32() % uint32(b.dimensions))
}

// l2Normalize normalizes vector to unit length.
func (b *BM25Embedder) l2Normalize(vector []float32) {
	var sumSquares float64
	for _, v := range vector {
		sumSquares += float64(v * v)
	}

	if sumSquares == 0 {
		return // Zero vector
	}

	norm := math.Sqrt(sumSquares)
	for i := range vector {
		vector[i] /= float32(norm)
	}
}
