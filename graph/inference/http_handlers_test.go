// Package inference provides structural anomaly detection for missing relationships.
package inference

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTTPHandler_ParseAnomalyPath(t *testing.T) {
	h := NewHTTPHandler(nil, nil, slog.Default())

	tests := []struct {
		name       string
		path       string
		wantID     string
		wantReview bool
	}{
		{
			name:       "simple anomaly ID",
			path:       "/inference/anomalies/abc123",
			wantID:     "abc123",
			wantReview: false,
		},
		{
			name:       "anomaly review path",
			path:       "/inference/anomalies/abc123/review",
			wantID:     "abc123",
			wantReview: true,
		},
		{
			name:       "with leading slash only",
			path:       "/anomalies/xyz789",
			wantID:     "xyz789",
			wantReview: false,
		},
		{
			name:       "no leading slash",
			path:       "anomalies/test-id",
			wantID:     "test-id",
			wantReview: false,
		},
		{
			name:       "trailing slash",
			path:       "/inference/anomalies/abc123/",
			wantID:     "abc123",
			wantReview: false,
		},
		{
			name:       "empty path",
			path:       "",
			wantID:     "",
			wantReview: false,
		},
		{
			name:       "no anomaly segment",
			path:       "/inference/stats",
			wantID:     "",
			wantReview: false,
		},
		{
			name:       "anomalies segment but no ID",
			path:       "/inference/anomalies/",
			wantID:     "",
			wantReview: false,
		},
		{
			name:       "uuid-style ID",
			path:       "/inference/anomalies/550e8400-e29b-41d4-a716-446655440000",
			wantID:     "550e8400-e29b-41d4-a716-446655440000",
			wantReview: false,
		},
		{
			name:       "uuid-style ID with review",
			path:       "/inference/anomalies/550e8400-e29b-41d4-a716-446655440000/review",
			wantID:     "550e8400-e29b-41d4-a716-446655440000",
			wantReview: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotID, gotReview := h.parseAnomalyPath(tt.path)
			assert.Equal(t, tt.wantID, gotID)
			assert.Equal(t, tt.wantReview, gotReview)
		})
	}
}

func TestHTTPHandler_HandleListPending(t *testing.T) {
	// Create test storage with some anomalies
	storage := NewNATSAnomalyStorage(nil, slog.Default())

	// Add pending anomalies
	ctx := context.Background()
	anomaly1 := &StructuralAnomaly{
		ID:         "pending-1",
		Type:       AnomalySemanticStructuralGap,
		Status:     StatusHumanReview,
		EntityA:    "test.inference.graph.http.entity.a",
		EntityB:    "test.inference.graph.http.entity.b",
		Confidence: 0.75,
		DetectedAt: time.Now(),
	}
	anomaly2 := &StructuralAnomaly{
		ID:         "pending-2",
		Type:       AnomalyCoreIsolation,
		Status:     StatusHumanReview,
		EntityA:    "test.inference.graph.http.entity.c",
		Confidence: 0.65,
		DetectedAt: time.Now(),
	}
	// Add one that's not pending
	anomaly3 := &StructuralAnomaly{
		ID:         "applied-1",
		Type:       AnomalyTransitivityGap,
		Status:     StatusApplied,
		EntityA:    "test.inference.graph.http.entity.d",
		Confidence: 0.90,
		DetectedAt: time.Now(),
	}

	require.NoError(t, storage.Save(ctx, anomaly1))
	require.NoError(t, storage.Save(ctx, anomaly2))
	require.NoError(t, storage.Save(ctx, anomaly3))

	h := NewHTTPHandler(storage, nil, slog.Default())

	// Test GET request
	req := httptest.NewRequest(http.MethodGet, "/inference/anomalies/pending", nil)
	w := httptest.NewRecorder()

	h.handleListPending(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var result []*StructuralAnomaly
	err := json.Unmarshal(w.Body.Bytes(), &result)
	require.NoError(t, err)

	// Should only return human_review status anomalies
	assert.Len(t, result, 2)

	// Test wrong method
	req = httptest.NewRequest(http.MethodPost, "/inference/anomalies/pending", nil)
	w = httptest.NewRecorder()
	h.handleListPending(w, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestHTTPHandler_HandleGetAnomaly(t *testing.T) {
	storage := NewNATSAnomalyStorage(nil, slog.Default())

	ctx := context.Background()
	anomaly := &StructuralAnomaly{
		ID:         "test-anomaly-1",
		Type:       AnomalySemanticStructuralGap,
		Status:     StatusPending,
		EntityA:    "test.inference.graph.http.entity.test-a",
		EntityB:    "test.inference.graph.http.entity.test-b",
		Confidence: 0.80,
		DetectedAt: time.Now(),
	}
	require.NoError(t, storage.Save(ctx, anomaly))

	h := NewHTTPHandler(storage, nil, slog.Default())

	// Test GET existing anomaly
	req := httptest.NewRequest(http.MethodGet, "/inference/anomalies/test-anomaly-1", nil)
	w := httptest.NewRecorder()

	h.handleGetAnomaly(w, req, "test-anomaly-1")

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var result StructuralAnomaly
	err := json.Unmarshal(w.Body.Bytes(), &result)
	require.NoError(t, err)
	assert.Equal(t, "test-anomaly-1", result.ID)
	assert.Equal(t, AnomalySemanticStructuralGap, result.Type)

	// Test GET non-existent anomaly
	req = httptest.NewRequest(http.MethodGet, "/inference/anomalies/nonexistent", nil)
	w = httptest.NewRecorder()
	h.handleGetAnomaly(w, req, "nonexistent")
	assert.Equal(t, http.StatusNotFound, w.Code)

	// Test wrong method
	req = httptest.NewRequest(http.MethodPost, "/inference/anomalies/test-anomaly-1", nil)
	w = httptest.NewRecorder()
	h.handleGetAnomaly(w, req, "test-anomaly-1")
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestHTTPHandler_HandleReviewAnomaly(t *testing.T) {
	storage := NewNATSAnomalyStorage(nil, slog.Default())
	applier := &mockApplier{}

	ctx := context.Background()
	anomaly := &StructuralAnomaly{
		ID:         "review-test-1",
		Type:       AnomalySemanticStructuralGap,
		Status:     StatusHumanReview,
		EntityA:    "test.inference.graph.http.entity.review-a",
		EntityB:    "test.inference.graph.http.entity.review-b",
		Confidence: 0.75,
		DetectedAt: time.Now(),
		Suggestion: &RelationshipSuggestion{
			FromEntity: "test.inference.graph.http.entity.review-a",
			ToEntity:   "test.inference.graph.http.entity.review-b",
			Predicate:  "inference.relation.related-to",
			Confidence: 0.75,
		},
	}
	require.NoError(t, storage.Save(ctx, anomaly))

	h := NewHTTPHandler(storage, applier, slog.Default())

	// Test approve decision
	reviewReq := ReviewRequest{
		Decision:   "approved",
		Notes:      "Looks good",
		ReviewedBy: "test-user",
	}
	body, _ := json.Marshal(reviewReq)

	req := httptest.NewRequest(http.MethodPost, "/inference/anomalies/review-test-1/review", bytes.NewReader(body))
	w := httptest.NewRecorder()

	h.handleReviewAnomaly(w, req, "review-test-1")

	assert.Equal(t, http.StatusOK, w.Code)

	var result StructuralAnomaly
	err := json.Unmarshal(w.Body.Bytes(), &result)
	require.NoError(t, err)
	assert.Equal(t, StatusApplied, result.Status)
	assert.Equal(t, "test-user", result.ReviewedBy)
	assert.Equal(t, "Looks good", result.ReviewNotes)

	// Verify applier was called
	applier.mu.Lock()
	assert.Len(t, applier.applied, 1)
	applier.mu.Unlock()
}

func TestHTTPHandler_HandleReviewAnomaly_Reject(t *testing.T) {
	storage := NewNATSAnomalyStorage(nil, slog.Default())

	ctx := context.Background()
	anomaly := &StructuralAnomaly{
		ID:         "reject-test-1",
		Type:       AnomalyCoreIsolation,
		Status:     StatusHumanReview,
		EntityA:    "test.inference.graph.http.entity.reject-a",
		Confidence: 0.50,
		DetectedAt: time.Now(),
	}
	require.NoError(t, storage.Save(ctx, anomaly))

	h := NewHTTPHandler(storage, nil, slog.Default())

	// Test reject decision
	reviewReq := ReviewRequest{
		Decision: "rejected",
		Notes:    "Not a valid relationship",
	}
	body, _ := json.Marshal(reviewReq)

	req := httptest.NewRequest(http.MethodPost, "/inference/anomalies/reject-test-1/review", bytes.NewReader(body))
	w := httptest.NewRecorder()

	h.handleReviewAnomaly(w, req, "reject-test-1")

	assert.Equal(t, http.StatusOK, w.Code)

	var result StructuralAnomaly
	err := json.Unmarshal(w.Body.Bytes(), &result)
	require.NoError(t, err)
	assert.Equal(t, StatusRejected, result.Status)
}

func TestHTTPHandler_HandleReviewAnomaly_InvalidDecision(t *testing.T) {
	storage := NewNATSAnomalyStorage(nil, slog.Default())

	ctx := context.Background()
	anomaly := &StructuralAnomaly{
		ID:         "invalid-decision-test",
		Type:       AnomalySemanticStructuralGap,
		Status:     StatusHumanReview,
		EntityA:    "test.inference.graph.http.entity.a",
		DetectedAt: time.Now(),
	}
	require.NoError(t, storage.Save(ctx, anomaly))

	h := NewHTTPHandler(storage, nil, slog.Default())

	// Test invalid decision
	reviewReq := ReviewRequest{
		Decision: "maybe",
	}
	body, _ := json.Marshal(reviewReq)

	req := httptest.NewRequest(http.MethodPost, "/inference/anomalies/invalid-decision-test/review", bytes.NewReader(body))
	w := httptest.NewRecorder()

	h.handleReviewAnomaly(w, req, "invalid-decision-test")

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHTTPHandler_HandleReviewAnomaly_WrongState(t *testing.T) {
	storage := NewNATSAnomalyStorage(nil, slog.Default())

	ctx := context.Background()
	anomaly := &StructuralAnomaly{
		ID:         "already-applied",
		Type:       AnomalySemanticStructuralGap,
		Status:     StatusApplied, // Already processed
		EntityA:    "test.inference.graph.http.entity.a",
		DetectedAt: time.Now(),
	}
	require.NoError(t, storage.Save(ctx, anomaly))

	h := NewHTTPHandler(storage, nil, slog.Default())

	reviewReq := ReviewRequest{
		Decision: "rejected",
	}
	body, _ := json.Marshal(reviewReq)

	req := httptest.NewRequest(http.MethodPost, "/inference/anomalies/already-applied/review", bytes.NewReader(body))
	w := httptest.NewRecorder()

	h.handleReviewAnomaly(w, req, "already-applied")

	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestHTTPHandler_HandleReviewAnomaly_EmptyToEntityRequiresTargetEntity(t *testing.T) {
	storage := NewNATSAnomalyStorage(nil, slog.Default())
	applier := &mockApplier{}

	ctx := context.Background()
	// Anomaly with Suggestion but empty ToEntity (common for core isolation anomalies)
	anomaly := &StructuralAnomaly{
		ID:         "empty-toentity-test",
		Type:       AnomalyCoreIsolation,
		Status:     StatusHumanReview,
		EntityA:    "test.inference.graph.http.entity.isolated",
		Confidence: 0.75,
		DetectedAt: time.Now(),
		Suggestion: &RelationshipSuggestion{
			FromEntity: "test.inference.graph.http.entity.isolated",
			ToEntity:   "", // Empty - needs to be filled by reviewer
			Predicate:  "inference.suggested.peer",
			Confidence: 0.75,
		},
	}
	require.NoError(t, storage.Save(ctx, anomaly))

	h := NewHTTPHandler(storage, applier, slog.Default())

	// Try to approve without providing TargetEntity
	reviewReq := ReviewRequest{
		Decision:   "approved",
		ReviewedBy: "test-user",
	}
	body, _ := json.Marshal(reviewReq)

	req := httptest.NewRequest(http.MethodPost, "/inference/anomalies/empty-toentity-test/review", bytes.NewReader(body))
	w := httptest.NewRecorder()

	h.handleReviewAnomaly(w, req, "empty-toentity-test")

	// Should fail because ToEntity is empty and no TargetEntity provided
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Target entity required")

	// Verify applier was NOT called
	applier.mu.Lock()
	assert.Len(t, applier.applied, 0)
	applier.mu.Unlock()
}

func TestHTTPHandler_HandleReviewAnomaly_TargetEntityFillsEmptyToEntity(t *testing.T) {
	storage := NewNATSAnomalyStorage(nil, slog.Default())
	applier := &mockApplier{}

	ctx := context.Background()
	// Anomaly with Suggestion but empty ToEntity
	anomaly := &StructuralAnomaly{
		ID:         "fill-toentity-test",
		Type:       AnomalyCoreIsolation,
		Status:     StatusHumanReview,
		EntityA:    "test.inference.graph.http.entity.isolated",
		Confidence: 0.75,
		DetectedAt: time.Now(),
		Suggestion: &RelationshipSuggestion{
			FromEntity: "test.inference.graph.http.entity.isolated",
			ToEntity:   "", // Empty - will be filled by TargetEntity
			Predicate:  "inference.suggested.peer",
			Confidence: 0.75,
		},
	}
	require.NoError(t, storage.Save(ctx, anomaly))

	h := NewHTTPHandler(storage, applier, slog.Default())

	// Approve with TargetEntity to fill empty ToEntity
	reviewReq := ReviewRequest{
		Decision:     "approved",
		TargetEntity: "test.inference.graph.http.entity.peer-hub",
		ReviewedBy:   "test-user",
	}
	body, _ := json.Marshal(reviewReq)

	req := httptest.NewRequest(http.MethodPost, "/inference/anomalies/fill-toentity-test/review", bytes.NewReader(body))
	w := httptest.NewRecorder()

	h.handleReviewAnomaly(w, req, "fill-toentity-test")

	assert.Equal(t, http.StatusOK, w.Code)

	var result StructuralAnomaly
	err := json.Unmarshal(w.Body.Bytes(), &result)
	require.NoError(t, err)
	assert.Equal(t, StatusApplied, result.Status)

	// Verify applier was called with filled ToEntity
	applier.mu.Lock()
	require.Len(t, applier.applied, 1)
	assert.Equal(t, "test.inference.graph.http.entity.peer-hub", applier.applied[0].ToEntity)
	assert.Equal(t, "test.inference.graph.http.entity.isolated", applier.applied[0].FromEntity)
	applier.mu.Unlock()
}

func TestHTTPHandler_HandleStats(t *testing.T) {
	storage := NewNATSAnomalyStorage(nil, slog.Default())

	ctx := context.Background()
	// Add anomalies with various statuses
	require.NoError(t, storage.Save(ctx, &StructuralAnomaly{
		ID: "pending-1", Status: StatusPending, Type: AnomalySemanticStructuralGap, DetectedAt: time.Now(),
	}))
	require.NoError(t, storage.Save(ctx, &StructuralAnomaly{
		ID: "pending-2", Status: StatusPending, Type: AnomalySemanticStructuralGap, DetectedAt: time.Now(),
	}))
	require.NoError(t, storage.Save(ctx, &StructuralAnomaly{
		ID: "human-review-1", Status: StatusHumanReview, Type: AnomalyCoreIsolation, DetectedAt: time.Now(),
	}))
	require.NoError(t, storage.Save(ctx, &StructuralAnomaly{
		ID: "applied-1", Status: StatusApplied, Type: AnomalyTransitivityGap, DetectedAt: time.Now(),
	}))
	require.NoError(t, storage.Save(ctx, &StructuralAnomaly{
		ID: "rejected-1", Status: StatusRejected, Type: AnomalySemanticStructuralGap, DetectedAt: time.Now(),
	}))

	h := NewHTTPHandler(storage, nil, slog.Default())

	req := httptest.NewRequest(http.MethodGet, "/inference/stats", nil)
	w := httptest.NewRecorder()

	h.handleStats(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var stats StatsResponse
	err := json.Unmarshal(w.Body.Bytes(), &stats)
	require.NoError(t, err)

	assert.Equal(t, 5, stats.TotalDetected)
	assert.Equal(t, 2, stats.PendingReview)
	assert.Equal(t, 1, stats.HumanReview)
	assert.Equal(t, 1, stats.Applied)
	assert.Equal(t, 1, stats.HumanRejected)

	// Test wrong method
	req = httptest.NewRequest(http.MethodPost, "/inference/stats", nil)
	w = httptest.NewRecorder()
	h.handleStats(w, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestHTTPHandler_HandleAnomalyByID_ReservedPaths(t *testing.T) {
	storage := NewNATSAnomalyStorage(nil, slog.Default())
	h := NewHTTPHandler(storage, nil, slog.Default())

	// Test that reserved paths are rejected when hitting anomalyByID handler
	tests := []struct {
		name string
		path string
	}{
		{"pending path", "/inference/anomalies/pending"},
		{"stats path", "/inference/anomalies/stats"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			req.URL.Path = tt.path
			w := httptest.NewRecorder()

			h.handleAnomalyByID(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
		})
	}
}

func TestHTTPHandler_RegisterHTTPHandlers(t *testing.T) {
	storage := NewNATSAnomalyStorage(nil, slog.Default())
	h := NewHTTPHandler(storage, nil, slog.Default())

	mux := http.NewServeMux()
	h.RegisterHTTPHandlers("/inference", mux)

	// Verify handlers are registered by making requests
	// We can't directly inspect ServeMux, but we can verify it doesn't panic

	// This is a basic smoke test - actual handler behavior is tested above
	assert.NotNil(t, mux)
}
