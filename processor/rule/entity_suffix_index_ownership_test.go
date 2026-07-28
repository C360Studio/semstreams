package rule

import (
	"context"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/internal/semantictest"
)

// entitySuffixIndexBucket is the graph-ingest-owned reverse index closed by
// framework-owned-bucket-guards inc-0. It is written as a literal here (not
// graph.BucketEntitySuffixIndex) on purpose: the test asserts the *behavior*
// (a generic update_kv into this bucket is rejected) independent of the
// constant's existence, so it is a valid failing-first probe before the bucket
// is added to graph.FrameworkOwnedBuckets().
const entitySuffixIndexBucket = "ENTITY_SUFFIX_INDEX"

// TestUpdateKV_RejectsEntitySuffixIndex_AtLoad proves the load-time
// write-ownership guard (config_validation.go) rejects a rule update_kv whose
// literal target bucket is ENTITY_SUFFIX_INDEX, closing the live hole where the
// bucket was absent from FrameworkOwnedBuckets().
func TestUpdateKV_RejectsEntitySuffixIndex_AtLoad(t *testing.T) {
	t.Parallel()
	def := Definition{
		ID:   "suffix-index-load-guard",
		Type: "expression",
		Actions: []Action{
			{
				Type:   ActionTypeUpdateKV,
				Bucket: entitySuffixIndexBucket,
				Key:    "some-suffix",
				Payload: map[string]any{
					"full_id": "acme.ops.robotics.gcs.drone.001",
				},
			},
		},
	}

	err := ValidateDefinition(def)
	if err == nil {
		t.Fatalf("ValidateDefinition must reject update_kv into %s, got nil", entitySuffixIndexBucket)
	}
	if !strings.Contains(err.Error(), "framework-owned") {
		t.Errorf("error must name the framework-owned bucket, got: %v", err)
	}
}

// TestUpdateKV_RejectsEntitySuffixIndex_AtRuntime proves the runtime guard
// (actions.go executeUpdateKV) rejects the same write after substitution, so a
// dynamically-resolved bucket name cannot smuggle a write past the load-time
// literal check.
func TestUpdateKV_RejectsEntitySuffixIndex_AtRuntime(t *testing.T) {
	t.Parallel()

	executor := &ActionExecutor{kvWriter: newMockKVWriter()}
	action := Action{
		Type:    ActionTypeUpdateKV,
		Bucket:  "$message.bucket",
		Key:     "some-suffix",
		Payload: map[string]any{"full_id": "acme.ops.robotics.gcs.drone.001"},
	}
	ec := &ExecutionContext{MessageData: map[string]any{"bucket": entitySuffixIndexBucket}}

	err := executor.executeUpdateKV(context.Background(), action, ec)
	if err == nil {
		t.Fatalf("executeUpdateKV must reject a write into %s, got nil", entitySuffixIndexBucket)
	}
	if !strings.Contains(err.Error(), "framework-owned") {
		t.Errorf("runtime error must name the framework-owned bucket, got: %v", err)
	}
}

// TestUpdateKV_PermitsNonOwnedBucket is the negative control: a generic
// update_kv into an application-owned bucket must still pass both the load-time
// validation and the runtime write, so the guard constrains only framework-owned
// buckets.
func TestUpdateKV_PermitsNonOwnedBucket(t *testing.T) {
	t.Parallel()

	const appBucket = "APP_PLAN_STATES"

	def := Definition{
		ID:   "app-bucket-allowed",
		Type: "expression",
		Actions: []Action{
			{
				Type:    ActionTypeUpdateKV,
				Bucket:  appBucket,
				Key:     "plan-1",
				Payload: map[string]any{"status": "drafting"},
			},
		},
	}
	if err := ValidateDefinition(def); err != nil {
		t.Fatalf("ValidateDefinition must permit update_kv into non-owned bucket %s, got: %v", appBucket, err)
	}

	kv := newMockKVWriter()
	executor := &ActionExecutor{kvWriter: kv}
	action := Action{
		Type:    ActionTypeUpdateKV,
		Bucket:  appBucket,
		Key:     "plan-1",
		Payload: map[string]any{"status": "drafting"},
	}
	ec := &ExecutionContext{EntityID: semantictest.EntityID(t, "test", "rule", "actions", "app-bucket", "plan", "001")}
	if err := executor.executeUpdateKV(context.Background(), action, ec); err != nil {
		t.Fatalf("executeUpdateKV must permit a write into non-owned bucket %s, got: %v", appBucket, err)
	}
	if _, ok := kv.data[appBucket]["plan-1"]; !ok {
		t.Errorf("expected a write into %s/plan-1", appBucket)
	}
}
