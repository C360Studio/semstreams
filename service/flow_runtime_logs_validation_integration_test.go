//go:build integration

package service

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleRuntimeLogs_InvalidFlowID(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, flowStore, nc := createTestFlowService(t)
	baseService := NewBaseServiceWithOptions("test-flow-service", nil, WithNATS(nc), WithLogger(slog.Default()))
	fs := &FlowService{
		BaseService: baseService,
		flowStore:   flowStore,
		config: FlowServiceConfig{
			LogStreamBufferSize: 100,
		},
	}

	// Test various invalid flow IDs
	invalidFlowIDs := []string{
		"flow-123>",
		"flow-*",
		"flow.123",
		"",
	}

	for _, invalidID := range invalidFlowIDs {
		t.Run(fmt.Sprintf("invalid_id_%s", invalidID), func(t *testing.T) {
			req := httptest.NewRequest("GET", fmt.Sprintf("/flows/%s/runtime/logs", invalidID), nil)
			req = req.WithContext(ctx)
			req.SetPathValue("id", invalidID)

			rec := httptest.NewRecorder()

			fs.handleRuntimeLogs(rec, req)

			assert.Equal(t, http.StatusBadRequest, rec.Code, "Should reject invalid flow ID")
			assert.Contains(t, rec.Body.String(), "Invalid flow ID", "Error message should mention invalid flow ID")
		})
	}
}

func TestHandleRuntimeLogs_InvalidComponentName(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, flowStore, nc := createTestFlowService(t)
	baseService := NewBaseServiceWithOptions("test-flow-service", nil, WithNATS(nc), WithLogger(slog.Default()))
	fs := &FlowService{
		BaseService: baseService,
		flowStore:   flowStore,
		config: FlowServiceConfig{
			LogStreamBufferSize: 100,
		},
	}

	flowID := createTestFlowInStore(t, ctx, flowStore)

	// Test various invalid component names
	invalidComponentNames := []string{
		"component>",
		"component*",
		"component.name",
	}

	for _, invalidName := range invalidComponentNames {
		t.Run(fmt.Sprintf("invalid_component_%s", invalidName), func(t *testing.T) {
			req := httptest.NewRequest("GET", fmt.Sprintf("/flows/%s/runtime/logs?component=%s", flowID, invalidName), nil)
			req = req.WithContext(ctx)
			req.SetPathValue("id", flowID)

			rec := httptest.NewRecorder()

			fs.handleRuntimeLogs(rec, req)

			assert.Equal(t, http.StatusBadRequest, rec.Code, "Should reject invalid component name")
			assert.Contains(t, rec.Body.String(), "Invalid component name", "Error message should mention invalid component name")
		})
	}
}

func TestHandleRuntimeLogs_SSEReconnectionHeaders(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, flowStore, nc := createTestFlowService(t)
	baseService := NewBaseServiceWithOptions("test-flow-service", nil, WithNATS(nc), WithLogger(slog.Default()))
	fs := &FlowService{
		BaseService: baseService,
		flowStore:   flowStore,
		config: FlowServiceConfig{
			LogStreamBufferSize: 100,
		},
	}

	flowID := createTestFlowInStore(t, ctx, flowStore)

	// Create request with Last-Event-ID header
	reqCtx, reqCancel := context.WithCancel(ctx)
	defer reqCancel()

	req := httptest.NewRequest("GET", fmt.Sprintf("/flows/%s/runtime/logs", flowID), nil)
	req = req.WithContext(reqCtx)
	req.SetPathValue("id", flowID)
	req.Header.Set("Last-Event-ID", "42")

	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		fs.handleRuntimeLogs(rec, req)
	}()

	// Wait for SSE headers
	require.Eventually(t, func() bool {
		return rec.Header().Get("Content-Type") == "text/event-stream"
	}, 2*time.Second, 10*time.Millisecond, "Handler should set SSE headers")

	// Wait for some data
	time.Sleep(100 * time.Millisecond)

	reqCancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Handler did not finish in time")
	}

	// Verify retry directive is present
	body := rec.Body.String()
	assert.Contains(t, body, "retry: 5000", "Should include retry directive")

	// Verify event IDs are present
	assert.Contains(t, body, "id:", "Should include event IDs")
}
