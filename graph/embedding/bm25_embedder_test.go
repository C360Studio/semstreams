package embedding

import (
	"context"
	"math"
	"testing"
)

func TestBM25Embedder_Generate(t *testing.T) {
	tests := []struct {
		name  string
		texts []string
		want  int // Expected number of embeddings
	}{
		{
			name:  "empty input",
			texts: []string{},
			want:  0,
		},
		{
			name:  "single text",
			texts: []string{"hello world"},
			want:  1,
		},
		{
			name:  "multiple texts",
			texts: []string{"hello world", "goodbye world", "hello goodbye"},
			want:  3,
		},
		{
			name:  "empty text",
			texts: []string{""},
			want:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			embedder := NewBM25Embedder(BM25Config{})
			embeddings, err := embedder.Generate(context.Background(), tt.texts)

			if err != nil {
				t.Fatalf("Generate() error = %v", err)
			}

			if len(embeddings) != tt.want {
				t.Errorf("Generate() got %d embeddings, want %d", len(embeddings), tt.want)
			}

			// Check dimensions
			for i, emb := range embeddings {
				if len(emb) != embedder.Dimensions() {
					t.Errorf("Embedding %d has dimension %d, want %d", i, len(emb), embedder.Dimensions())
				}
			}
		})
	}
}

// TestBM25Embedder_GenerateQuerySymmetric: BM25 has no query/document asymmetry, so
// GenerateQuery must produce the same vector as Generate for the same text (gh#438).
func TestBM25Embedder_GenerateQuerySymmetric(t *testing.T) {
	embedder := NewBM25Embedder(BM25Config{Dimensions: 384})
	ctx := context.Background()
	texts := []string{"retry with backoff on transient failure"}

	doc, err := embedder.Generate(ctx, texts)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	qry, err := embedder.GenerateQuery(ctx, texts)
	if err != nil {
		t.Fatalf("GenerateQuery: %v", err)
	}
	if len(doc) != 1 || len(qry) != 1 || len(doc[0]) != len(qry[0]) {
		t.Fatalf("shape mismatch: doc=%d qry=%d", len(doc), len(qry))
	}
	for i := range doc[0] {
		if doc[0][i] != qry[0][i] {
			t.Fatalf("BM25 GenerateQuery diverged from Generate at dim %d: %v vs %v", i, doc[0][i], qry[0][i])
		}
	}
}

func TestBM25Embedder_Dimensions(t *testing.T) {
	tests := []struct {
		name       string
		dimensions int
		want       int
	}{
		{
			name:       "default dimensions",
			dimensions: 0,
			want:       384,
		},
		{
			name:       "custom dimensions",
			dimensions: 128,
			want:       128,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			embedder := NewBM25Embedder(BM25Config{Dimensions: tt.dimensions})
			if got := embedder.Dimensions(); got != tt.want {
				t.Errorf("Dimensions() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestBM25Embedder_Model(t *testing.T) {
	embedder := NewBM25Embedder(BM25Config{})
	model := embedder.Model()

	if model == "" {
		t.Error("Model() returned empty string")
	}

	if model != "bm25-go-k1.5-b0.75" {
		t.Errorf("Model() = %q, expected to contain bm25", model)
	}
}

func TestBM25Embedder_Tokenize(t *testing.T) {
	embedder := NewBM25Embedder(BM25Config{})

	tests := []struct {
		name string
		text string
		want []string
	}{
		{
			name: "simple text",
			text: "hello world",
			want: []string{"hello", "world"},
		},
		{
			name: "mixed case",
			text: "Hello World",
			want: []string{"hello", "world"},
		},
		{
			name: "with punctuation",
			text: "Hello, world!",
			want: []string{"hello", "world"},
		},
		{
			name: "with numbers",
			text: "test123 abc456",
			want: []string{"test123", "abc456"},
		},
		{
			name: "filters short tokens",
			text: "a bb ccc",
			want: []string{"bb", "ccc"}, // "a" is filtered (< 2 chars)
		},
		{
			name: "empty text",
			text: "",
			want: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := embedder.tokenize(tt.text)
			if len(got) != len(tt.want) {
				t.Errorf("tokenize() got %d tokens, want %d", len(got), len(tt.want))
				t.Logf("got: %v", got)
				t.Logf("want: %v", tt.want)
				return
			}

			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("tokenize() token[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestBM25Embedder_Normalization(t *testing.T) {
	embedder := NewBM25Embedder(BM25Config{Dimensions: 128})

	// Generate embeddings
	texts := []string{"hello world", "test document"}
	embeddings, err := embedder.Generate(context.Background(), texts)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	// Check that vectors are normalized (L2 norm ≈ 1.0)
	for i, emb := range embeddings {
		var sumSquares float64
		for _, v := range emb {
			sumSquares += float64(v * v)
		}
		norm := math.Sqrt(sumSquares)

		// Should be normalized (within floating point tolerance)
		if emb[0] == 0 && norm == 0 {
			// Empty vector is OK
			continue
		}

		if math.Abs(norm-1.0) > 0.01 {
			t.Errorf("Embedding %d has L2 norm %f, expected ~1.0", i, norm)
		}
	}
}

func TestBM25Embedder_Similarity(t *testing.T) {
	embedder := NewBM25Embedder(BM25Config{Dimensions: 384})

	// Generate embeddings for similar and dissimilar texts
	texts := []string{
		"machine learning algorithms",
		"machine learning models",    // Similar to first
		"cooking recipes for dinner", // Dissimilar
	}

	embeddings, err := embedder.Generate(context.Background(), texts)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	// Compute cosine similarities
	sim01 := CosineSimilarity(embeddings[0], embeddings[1])
	sim02 := CosineSimilarity(embeddings[0], embeddings[2])

	// Similar texts should have higher similarity than dissimilar texts
	if sim01 <= sim02 {
		t.Errorf("Similar texts similarity (%f) should be higher than dissimilar texts (%f)", sim01, sim02)
	}

	// Similarities should be in reasonable range [0, 1] for normalized vectors
	if sim01 < 0 || sim01 > 1 {
		t.Errorf("Similarity %f is out of range [0, 1]", sim01)
	}
	if sim02 < 0 || sim02 > 1 {
		t.Errorf("Similarity %f is out of range [0, 1]", sim02)
	}
}

func TestBM25Embedder_IncrementalLearning(t *testing.T) {
	embedder := NewBM25Embedder(BM25Config{Dimensions: 128})

	// First batch - use "forklift" which doesn't get stemmed
	texts1 := []string{"forklift alpha", "forklift beta"}
	_, err := embedder.Generate(context.Background(), texts1)
	if err != nil {
		t.Fatalf("Generate() batch 1 error = %v", err)
	}

	if embedder.docCount != 2 {
		t.Errorf("After batch 1, docCount = %d, want 2", embedder.docCount)
	}

	// Second batch - should accumulate statistics
	texts2 := []string{"forklift gamma"}
	_, err = embedder.Generate(context.Background(), texts2)
	if err != nil {
		t.Fatalf("Generate() batch 2 error = %v", err)
	}

	if embedder.docCount != 3 {
		t.Errorf("After batch 2, docCount = %d, want 3", embedder.docCount)
	}

	// Check that term document counts accumulated
	embedder.mu.RLock()
	docTermCount := embedder.termDocCount["forklift"]
	embedder.mu.RUnlock()

	if docTermCount != 3 {
		t.Errorf("Term 'forklift' appears in %d documents, want 3", docTermCount)
	}
}

func TestBM25Embedder_EmptyDocument(t *testing.T) {
	embedder := NewBM25Embedder(BM25Config{Dimensions: 128})

	texts := []string{"", "   ", "!@#$%"}
	embeddings, err := embedder.Generate(context.Background(), texts)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	// All should produce zero vectors (no valid tokens)
	for i, emb := range embeddings {
		var sum float64
		for _, v := range emb {
			sum += float64(v)
		}
		if sum != 0 {
			t.Errorf("Empty document %d produced non-zero vector (sum=%f)", i, sum)
		}
	}
}

func TestBM25Embedder_Consistency(t *testing.T) {
	embedder := NewBM25Embedder(BM25Config{Dimensions: 128})

	text := "consistent test document"

	// Generate embedding twice
	emb1, err := embedder.Generate(context.Background(), []string{text})
	if err != nil {
		t.Fatalf("Generate() first call error = %v", err)
	}

	emb2, err := embedder.Generate(context.Background(), []string{text})
	if err != nil {
		t.Fatalf("Generate() second call error = %v", err)
	}

	// Embeddings should be similar (but not identical due to incremental IDF updates)
	similarity := CosineSimilarity(emb1[0], emb2[0])

	// Should have very high similarity (> 0.9)
	if similarity < 0.9 {
		t.Errorf("Same text generated embeddings with low similarity: %f", similarity)
	}
}

func TestBM25Embedder_BatchProcessing(t *testing.T) {
	embedder := NewBM25Embedder(BM25Config{Dimensions: 256})

	// Generate embeddings in batch
	texts := make([]string, 100)
	for i := range texts {
		texts[i] = "document number " + string(rune(i))
	}

	embeddings, err := embedder.Generate(context.Background(), texts)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	if len(embeddings) != 100 {
		t.Errorf("Generated %d embeddings, want 100", len(embeddings))
	}

	// All embeddings should have correct dimensions
	for i, emb := range embeddings {
		if len(emb) != 256 {
			t.Errorf("Embedding %d has dimension %d, want 256", i, len(emb))
		}
	}
}

func TestBM25Embedder_Close(t *testing.T) {
	embedder := NewBM25Embedder(BM25Config{})

	err := embedder.Close()
	if err != nil {
		t.Errorf("Close() error = %v, want nil", err)
	}
}

// Benchmark BM25 embedding generation
func BenchmarkBM25Embedder_Generate(b *testing.B) {
	embedder := NewBM25Embedder(BM25Config{Dimensions: 384})

	texts := []string{
		"The quick brown fox jumps over the lazy dog",
		"Machine learning models require large amounts of training data",
		"Natural language processing is a subfield of artificial intelligence",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := embedder.Generate(context.Background(), texts)
		if err != nil {
			b.Fatalf("Generate() error = %v", err)
		}
	}
}

func BenchmarkBM25Embedder_SingleText(b *testing.B) {
	embedder := NewBM25Embedder(BM25Config{Dimensions: 384})

	text := "Natural language processing with machine learning algorithms"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := embedder.Generate(context.Background(), []string{text})
		if err != nil {
			b.Fatalf("Generate() error = %v", err)
		}
	}
}

func TestBM25Embedder_StopwordRemoval(t *testing.T) {
	embedder := NewBM25Embedder(BM25Config{})

	tests := []struct {
		name     string
		text     string
		contains []string
		excludes []string
	}{
		{
			name:     "question words removed",
			text:     "What documents mention forklift",
			contains: []string{"document", "forklift"}, // documents → document (stemmed)
			excludes: []string{"what", "mention"},
		},
		{
			name:     "articles removed",
			text:     "the quick brown fox",
			contains: []string{"quick", "brown", "fox"},
			excludes: []string{"the"},
		},
		{
			name:     "prepositions removed",
			text:     "sensors in zone with readings",
			contains: []string{"sensor", "zone", "reading"}, // stemmed
			excludes: []string{"in", "with"},
		},
		{
			name:     "pronouns removed",
			text:     "they found it quickly",
			contains: []string{"found", "quick"}, // quickly → quick (stemmed)
			excludes: []string{"they", "it"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := embedder.tokenize(tt.text)

			// Check that expected tokens are present
			for _, expected := range tt.contains {
				found := false
				for _, token := range tokens {
					if token == expected {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("tokenize(%q) should contain %q, got %v", tt.text, expected, tokens)
				}
			}

			// Check that stopwords are excluded
			for _, excluded := range tt.excludes {
				for _, token := range tokens {
					if token == excluded {
						t.Errorf("tokenize(%q) should not contain stopword %q, got %v", tt.text, excluded, tokens)
					}
				}
			}
		})
	}
}

func TestBM25Embedder_Stemming(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"documents", "document"},
		{"running", "run"}, // Doubled consonant handling: running → runn → run
		{"mentioned", "mention"},
		{"safely", "safe"},
		{"equipment", "equip"},
		{"queries", "query"},
		{"operation", "opera"},         // -tion removed: operation (9) → opera (5)
		{"maintenance", "maintenance"}, // No matching suffix
		{"forklift", "forklift"},       // No suffix to remove
		{"cold", "cold"},               // Short word unchanged
		{"boxes", "box"},
		{"safety", "safety"}, // -ty not matched, -y doesn't meet length requirement
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := stem(tt.input)
			if got != tt.expected {
				t.Errorf("stem(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestBM25Embedder_NaturalLanguageQuerySimilarity(t *testing.T) {
	embedder := NewBM25Embedder(BM25Config{Dimensions: 384})

	// Natural language query vs document title
	query := "What documents mention forklift safety"
	doc := "Forklift Operation Manual"

	// Generate embeddings
	queryEmb, err := embedder.Generate(context.Background(), []string{query})
	if err != nil {
		t.Fatalf("Generate(query) error = %v", err)
	}
	docEmb, err := embedder.Generate(context.Background(), []string{doc})
	if err != nil {
		t.Fatalf("Generate(doc) error = %v", err)
	}

	// Calculate similarity
	score := CosineSimilarity(queryEmb[0], docEmb[0])

	// With stopwords removed and stemming, query tokens are:
	// ["document", "forklift", "safety"] (what, mention filtered)
	// Document tokens: ["forklift", "operat", "manual"]
	// "forklift" should match, giving reasonable similarity
	if score < 0.2 {
		t.Errorf("Query-document similarity = %f, expected >= 0.2 (forklift should match)", score)
		t.Logf("Query: %q", query)
		t.Logf("Query tokens: %v", embedder.tokenize(query))
		t.Logf("Doc: %q", doc)
		t.Logf("Doc tokens: %v", embedder.tokenize(doc))
	}
}
