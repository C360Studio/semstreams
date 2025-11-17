package embedding

import (
	"fmt"
	"math"
)

// CosineSimilarity computes the cosine similarity between two vectors.
//
// Returns a value between -1 and 1, where:
//   - 1 means vectors are identical
//   - 0 means vectors are orthogonal (unrelated)
//   - -1 means vectors are opposite
//
// Formula: cos(θ) = (A · B) / (||A|| × ||B||)
func CosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0.0
	}

	if len(a) == 0 {
		return 0.0
	}

	var dotProduct float64
	var magnitudeA float64
	var magnitudeB float64

	for i := 0; i < len(a); i++ {
		dotProduct += float64(a[i]) * float64(b[i])
		magnitudeA += float64(a[i]) * float64(a[i])
		magnitudeB += float64(b[i]) * float64(b[i])
	}

	// Avoid division by zero
	if magnitudeA == 0.0 || magnitudeB == 0.0 {
		return 0.0
	}

	return dotProduct / (math.Sqrt(magnitudeA) * math.Sqrt(magnitudeB))
}

// euclideanDistance computes the Euclidean distance between two vectors.
//
// Returns a non-negative value where:
//   - 0 means vectors are identical
//   - Higher values mean vectors are more different
//
// Formula: d = √(Σ(ai - bi)²)
func euclideanDistance(a, b []float64) (float64, error) {
	if len(a) != len(b) {
		return 0.0, fmt.Errorf("vectors must have same length: %d != %d", len(a), len(b))
	}

	sum := 0.0
	for i := 0; i < len(a); i++ {
		diff := a[i] - b[i]
		sum += diff * diff
	}

	return math.Sqrt(sum), nil
}

// dotProduct computes the dot product of two vectors.
//
// Formula: A · B = Σ(ai × bi)
func dotProduct(a, b []float64) (float64, error) {
	if len(a) != len(b) {
		return 0.0, fmt.Errorf("vectors must have same length: %d != %d", len(a), len(b))
	}

	sum := 0.0
	for i := 0; i < len(a); i++ {
		sum += a[i] * b[i]
	}

	return sum, nil
}

// magnitude computes the magnitude (L2 norm) of a vector.
//
// Formula: ||A|| = √(Σai²)
func magnitude(v []float64) float64 {
	sum := 0.0
	for _, val := range v {
		sum += val * val
	}
	return math.Sqrt(sum)
}

// normalize normalizes a vector to unit length.
//
// Returns a new vector with magnitude 1.
func normalize(v []float64) []float64 {
	mag := magnitude(v)
	if mag == 0.0 {
		return v
	}

	result := make([]float64, len(v))
	for i, val := range v {
		result[i] = val / mag
	}
	return result
}
