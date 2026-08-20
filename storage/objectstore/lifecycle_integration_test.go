//go:build integration

package objectstore_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/storage/objectstore"
)

// createTestComponentForLifecycle creates a test instance for lifecycle testing.
// Uses the shared NATS client from store_integration_test.go TestMain.
func createTestComponentForLifecycle() component.LifecycleComponent {
	// Use default config with unique bucket name for this test
	config := objectstore.DefaultConfig()
	config.BucketName = "LIFECYCLE_TEST_MESSAGES"
	configJSON, err := json.Marshal(config)
	if err != nil {
		panic("failed to marshal lifecycle config: " + err.Error())
	}
	if sharedNATSClient == nil {
		panic("shared NATS client not initialized")
	}

	// Use the shared NATS client from TestMain in store_integration_test.go
	deps := component.Dependencies{
		NATSClient: sharedNATSClient,
	}

	comp, err := objectstore.NewComponent(configJSON, deps)
	if err != nil {
		panic("failed to create lifecycle component: " + err.Error())
	}

	lifecycleComponent, ok := comp.(component.LifecycleComponent)
	if !ok {
		panic("object store does not implement component.LifecycleComponent")
	}
	return lifecycleComponent
}

// TestObjectStore_OneShotLifecycle exercises the assembled ObjectStore owner.
func TestObjectStore_OneShotLifecycle(t *testing.T) {
	owner := createTestComponentForLifecycle()
	if err := owner.Initialize(); err != nil {
		t.Fatal(err)
	}
	if err := owner.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := owner.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := owner.Start(t.Context()); !errors.Is(err, errs.ErrAlreadyStarted) {
		t.Fatalf("restart error = %v", err)
	}
	if err := owner.Stop(context.Background()); err != nil {
		t.Fatalf("repeated terminal Stop: %v", err)
	}
}
