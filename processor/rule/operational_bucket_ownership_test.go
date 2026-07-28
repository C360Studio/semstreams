package rule

import (
	"context"
	"strings"
	"testing"
)

// operationalOwnedBuckets are the two framework operational buckets promoted to
// FrameworkOwnedBuckets() by framework-owned-bucket-guards F2/F3. They are
// written as literals here (not the graph.* constants) on purpose: the tests
// assert the write-ownership BEHAVIOR (a generic update_kv into either is
// rejected at load AND runtime) independent of the constants, mirroring
// entity_suffix_index_ownership_test.go.
//
//   - GRAPH_INGEST_APPLIED_SEQ: forging a redelivery-guard sequence stamp would
//     silently reopen the restart/cache-eviction overwrite the guard closes (#715).
//   - GRAPH_STATUS: forging a readiness envelope would let a rule fake "graph is
//     ready" and defeat the health gate (F3).
var operationalOwnedBuckets = []string{"GRAPH_INGEST_APPLIED_SEQ", "GRAPH_STATUS"}

// TestUpdateKV_RejectsOperationalOwnedBuckets_AtLoad proves the load-time
// write-ownership guard (config_validation.go) rejects a rule update_kv whose
// literal target bucket is a framework operational bucket.
func TestUpdateKV_RejectsOperationalOwnedBuckets_AtLoad(t *testing.T) {
	t.Parallel()
	for _, bucket := range operationalOwnedBuckets {
		bucket := bucket
		t.Run(bucket, func(t *testing.T) {
			t.Parallel()
			def := Definition{
				ID:   "op-owned-load-guard",
				Type: "expression",
				Actions: []Action{
					{
						Type:    ActionTypeUpdateKV,
						Bucket:  bucket,
						Key:     "some-key",
						Payload: map[string]any{"forged": true},
					},
				},
			}
			err := ValidateDefinition(def)
			if err == nil {
				t.Fatalf("ValidateDefinition must reject update_kv into %s, got nil", bucket)
			}
			if !strings.Contains(err.Error(), "framework-owned") {
				t.Errorf("error must name the framework-owned bucket, got: %v", err)
			}
		})
	}
}

// TestUpdateKV_RejectsOperationalOwnedBuckets_AtRuntime proves the runtime guard
// (actions.go executeUpdateKV) rejects the same write after substitution, so a
// dynamically-resolved bucket name cannot smuggle a write past the load-time
// literal check.
func TestUpdateKV_RejectsOperationalOwnedBuckets_AtRuntime(t *testing.T) {
	t.Parallel()
	for _, bucket := range operationalOwnedBuckets {
		bucket := bucket
		t.Run(bucket, func(t *testing.T) {
			t.Parallel()
			executor := &ActionExecutor{kvWriter: newMockKVWriter()}
			action := Action{
				Type:    ActionTypeUpdateKV,
				Bucket:  "$message.bucket",
				Key:     "some-key",
				Payload: map[string]any{"forged": true},
			}
			ec := &ExecutionContext{MessageData: map[string]any{"bucket": bucket}}

			err := executor.executeUpdateKV(context.Background(), action, ec)
			if err == nil {
				t.Fatalf("executeUpdateKV must reject a write into %s, got nil", bucket)
			}
			if !strings.Contains(err.Error(), "framework-owned") {
				t.Errorf("runtime error must name the framework-owned bucket, got: %v", err)
			}
		})
	}
}
