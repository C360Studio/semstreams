// Package rule - Integration tests for stateful rule evaluation
//go:build integration
// +build integration

package rule

import (
	"context"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
)

// TestStatefulEvaluator_Integration tests the full integration with NATS KV
func TestStatefulEvaluator_Integration(t *testing.T) {
	// This test requires a running NATS server with JetStream enabled
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create NATS test client with JetStream and KV enabled
	testClient := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithKV())
	defer testClient.Terminate()

	// Create processor with default config
	config := mustTestConfig(t, "rule-test-pack")
	config.PackID = "stateful-integration-test"
	processor, err := NewProcessor(testClient.Client, &config)
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}
	processor.SetPlatform(component.PlatformMeta{Org: "c360", Platform: "platform1"})

	// Initialize processor
	if err := processor.Initialize(); err != nil {
		t.Fatalf("Failed to initialize processor: %v", err)
	}

	// Start processor - this should initialize StateTracker
	if err := processor.Start(ctx); err != nil {
		t.Fatalf("Failed to start processor: %v", err)
	}
	defer processor.Stop(context.Background())

	// Verify StateTracker was initialized
	if processor.stateTracker == nil {
		t.Error("StateTracker was not initialized")
	}

	// Verify StatefulEvaluator was initialized
	if processor.statefulEvaluator == nil {
		t.Error("StatefulEvaluator was not initialized")
	}

	// Test basic state tracking
	if processor.stateTracker != nil {
		testState := MatchState{
			RuleID:         "test-rule",
			EntityKey:      "test-entity",
			IsMatching:     true,
			LastTransition: string(TransitionEntered),
			TransitionAt:   time.Now(),
			LastChecked:    time.Now(),
		}

		// Set state
		if err := processor.stateTracker.Set(ctx, testState); err != nil {
			t.Fatalf("Failed to set state: %v", err)
		}

		// Get state
		retrieved, err := processor.stateTracker.Get(ctx, "test-rule", "test-entity")
		if err != nil {
			t.Fatalf("Failed to get state: %v", err)
		}

		if !retrieved.IsMatching {
			t.Errorf("Retrieved state IsMatching = false, want true")
		}

		if retrieved.LastTransition != string(TransitionEntered) {
			t.Errorf("Retrieved state LastTransition = %v, want %v", retrieved.LastTransition, TransitionEntered)
		}

		// Clean up
		if err := processor.stateTracker.Delete(ctx, "test-rule", "test-entity"); err != nil {
			t.Fatalf("Failed to delete state: %v", err)
		}
	}
}
