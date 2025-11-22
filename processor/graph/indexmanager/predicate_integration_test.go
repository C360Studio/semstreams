//go:build integration

package indexmanager

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"regexp"
	"strings"
	"testing"

	"github.com/c360/semstreams/errors"
)

// TestPredicateIndexRealWorld tests the predicate indexing with real scenarios
func TestPredicateIndexRealWorld(t *testing.T) {
	// Real predicates from E2E tests that are failing
	realWorldPredicates := []string{
		"robotics.system.id",
		"robotics.system.type",
		"robotics.flight.armed",
		"robotics.battery.level",
		"robotics.position.latitude",
		"robotics.position.longitude",
		"robotics.velocity.groundspeed",
		"robotics.attitude.roll",
		"robotics.attitude.pitch",
		"robotics.attitude.yaw",
	}

	t.Run("predicate_sanitization", func(t *testing.T) {
		for _, predicate := range realWorldPredicates {
			sanitized := sanitizeNATSKey(predicate)

			// Should be identical since these are already valid
			assert.Equal(t, predicate, sanitized,
				"Real-world predicate %q should not need sanitization", predicate)

			// Should pass NATS validation
			validKeyRe := regexp.MustCompile(`^[-/_=\.a-zA-Z0-9]+$`)
			assert.True(t, validKeyRe.MatchString(sanitized),
				"Predicate %q should be valid NATS key", predicate)
		}
	})

	t.Run("predicate_index_put_operations", func(t *testing.T) {
		mockBucket := NewMockKeyValue()
		metrics := &InternalMetrics{}
		promMetrics := &PrometheusMetrics{}

		predicateIndex := NewPredicateIndex(mockBucket, nil, metrics, promMetrics, nil)
		ctx := context.Background()

		// Test each real-world predicate
		for _, predicate := range realWorldPredicates {
			entityID := "telemetry.robotics.drone.1"

			// Setup mock to succeed
			mockBucket.On("Get", ctx, predicate).Return(nil, jetstream.ErrKeyNotFound).Once()
			mockBucket.On("Create", ctx, predicate, mock.MatchedBy(func(data []byte) bool {
				// Verify the simple format: just []string of entity IDs
				var entities []string
				if err := json.Unmarshal(data, &entities); err != nil {
					t.Errorf("Failed to unmarshal predicate data as []string: %v", err)
					return false
				}

				if len(entities) != 1 || entities[0] != entityID {
					t.Errorf("Expected entities [%s], got %v", entityID, entities)
					return false
				}

				return true
			})).Return(uint64(1), nil).Once()

			// Test the actual function that's failing in E2E
			err := predicateIndex.addEntityToPredicateIndex(ctx, predicate, entityID)

			assert.NoError(t, err, "addEntityToPredicateIndex should succeed for predicate %q", predicate)
		}

		// Verify all mocks were called
		mockBucket.AssertExpectations(t)
	})

	t.Run("context_cancellation_handling", func(t *testing.T) {
		mockBucket := NewMockKeyValue()
		metrics := &InternalMetrics{}
		promMetrics := &PrometheusMetrics{}

		predicateIndex := NewPredicateIndex(mockBucket, nil, metrics, promMetrics, nil)

		// Test with cancelled context
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		predicate := "robotics.system.id"
		entityID := "telemetry.robotics.drone.1"

		// Mock should still be called even with cancelled context
		mockBucket.On("Get", ctx, predicate).Return(nil, jetstream.ErrKeyNotFound).Once()
		mockBucket.On("Create", ctx, predicate, mock.Anything).Return(uint64(0), context.Canceled).Once()

		err := predicateIndex.addEntityToPredicateIndex(ctx, predicate, entityID)

		// Should return context cancellation error (transient)
		assert.Error(t, err)
		assert.True(t, errors.IsTransient(err), "Context cancellation errors should be transient")

		mockBucket.AssertExpectations(t)
	})

	t.Run("bucket_put_error_handling", func(t *testing.T) {
		mockBucket := NewMockKeyValue()
		metrics := &InternalMetrics{}
		promMetrics := &PrometheusMetrics{}

		predicateIndex := NewPredicateIndex(mockBucket, nil, metrics, promMetrics, nil)
		ctx := context.Background()

		predicate := "robotics.system.id"
		entityID := "telemetry.robotics.drone.1"

		// Mock Get to return no existing data
		mockBucket.On("Get", ctx, predicate).Return(nil, jetstream.ErrKeyNotFound).Once()

		// Mock Create to return the exact "nats: invalid key" error
		mockBucket.On("Create", ctx, predicate, mock.Anything).
			Return(uint64(0), stderrors.New("nats: invalid key")).
			Once()

		err := predicateIndex.addEntityToPredicateIndex(ctx, predicate, entityID)

		// Should return NATS invalid key error (invalid data)
		assert.Error(t, err)
		assert.True(t, errors.IsInvalid(err), "Invalid key errors should be classified as invalid data")

		mockBucket.AssertExpectations(t)
	})
}

// TestPredicateIndexEdgeCases tests edge cases that might cause "invalid key" errors
func TestPredicateIndexEdgeCases(t *testing.T) {
	testCases := []struct {
		name        string
		predicate   string
		expectError bool
	}{
		{
			name:        "empty_predicate",
			predicate:   "",
			expectError: false, // sanitizeNATSKey converts to "unknown"
		},
		{
			name:        "whitespace_predicate",
			predicate:   "   ",
			expectError: false, // sanitizeNATSKey converts to "___"
		},
		{
			name:        "very_long_predicate",
			predicate:   "robotics." + strings.Repeat("a", 250) + ".predicate",
			expectError: false, // After sanitization, truncates to 255 chars which is valid
		},
		{
			name:        "unicode_predicate",
			predicate:   "robotics.système.françäis",
			expectError: false, // Unicode gets removed, should work
		},
		{
			name:        "special_chars_predicate",
			predicate:   "robotics@system#id$with%special^chars",
			expectError: false, // Special chars get removed
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mockBucket := NewMockKeyValue()
			metrics := &InternalMetrics{}
			promMetrics := &PrometheusMetrics{}

			predicateIndex := NewPredicateIndex(mockBucket, nil, metrics, promMetrics, nil)
			ctx := context.Background()
			entityID := "test.entity"

			if !tc.expectError {
				// Setup successful mocks
				sanitizedKey := sanitizeNATSKey(tc.predicate)
				mockBucket.On("Get", ctx, sanitizedKey).Return(nil, jetstream.ErrKeyNotFound).Maybe()
				mockBucket.On("Create", ctx, sanitizedKey, mock.Anything).Return(uint64(1), nil).Maybe()
			}

			err := predicateIndex.addEntityToPredicateIndex(ctx, tc.predicate, entityID)

			if tc.expectError {
				assert.Error(t, err)
				// Edge case errors (like key too long) should be invalid data
				assert.True(t, errors.IsInvalid(err), "Edge case errors should be invalid data errors")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestPredicateIndexFullWorkflow tests the complete indexing workflow
func TestPredicateIndexFullWorkflow(t *testing.T) {
	mockBucket := NewMockKeyValue()
	metrics := &InternalMetrics{}
	promMetrics := &PrometheusMetrics{}

	predicateIndex := NewPredicateIndex(mockBucket, nil, metrics, promMetrics, nil)
	ctx := context.Background()

	// Test the simplest case first - just call addEntityToPredicateIndex directly
	entityID := "c360.platform1.robotics.mav1.drone.0"
	predicate := "robotics.system.id"

	// Mock Get (no existing data)
	mockBucket.On("Get", mock.Anything, mock.Anything).Return(nil, jetstream.ErrKeyNotFound).Once()

	// Mock Create (not Put!) - when revision=0, implementation calls Create()
	mockBucket.On("Create", mock.Anything, mock.Anything, mock.Anything).Return(uint64(1), nil).Once()

	// Test addEntityToPredicateIndex directly
	err := predicateIndex.addEntityToPredicateIndex(ctx, predicate, entityID)
	require.NoError(t, err)

	// Verify all mocks were called
	mockBucket.AssertExpectations(t)
}
