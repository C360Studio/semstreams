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

// TestUpdateKV_RejectsCatalogDerivedBuckets_BothGuards is the
// derivation-not-snapshot proof at the production guards: OWNER_CLAIMS and
// OWNER_PRESENCE were NEVER members of the retired hand-written owned list —
// they are owned ONLY because the catalog declares them write-owner-only and
// both guards consume the DERIVED view. If either guard were a snapshot of the
// old list, these writes would pass.
func TestUpdateKV_RejectsCatalogDerivedBuckets_BothGuards(t *testing.T) {
	t.Parallel()
	for _, bucket := range []string{"OWNER_CLAIMS", "OWNER_PRESENCE"} {
		bucket := bucket
		t.Run(bucket, func(t *testing.T) {
			t.Parallel()
			// Load-time guard.
			def := Definition{
				ID:   "derived-owned-load-guard",
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
				t.Fatalf("ValidateDefinition must reject update_kv into %s (derived owned set), got nil", bucket)
			}
			if !strings.Contains(err.Error(), "framework-owned") {
				t.Errorf("error must name the framework-owned bucket, got: %v", err)
			}

			// Runtime guard, via substitution.
			executor := &ActionExecutor{kvWriter: newMockKVWriter()}
			action := Action{
				Type:    ActionTypeUpdateKV,
				Bucket:  "$message.bucket",
				Key:     "some-key",
				Payload: map[string]any{"forged": true},
			}
			ec := &ExecutionContext{MessageData: map[string]any{"bucket": bucket}}
			err = executor.executeUpdateKV(context.Background(), action, ec)
			if err == nil {
				t.Fatalf("executeUpdateKV must reject a write into %s (derived owned set), got nil", bucket)
			}
			if !strings.Contains(err.Error(), "framework-owned") {
				t.Errorf("runtime error must name the framework-owned bucket, got: %v", err)
			}
		})
	}
}

// TestUpdateKV_PermitsWriteOpenComponentStatus: COMPONENT_STATUS is a catalog
// member deliberately declared write-OPEN (#717: many cross-layer writers,
// zero production readers), so it must NOT appear in the derived owned set —
// a generic update_kv into it passes both guards. The guard constrains
// owner-only rows, not catalog membership.
func TestUpdateKV_PermitsWriteOpenComponentStatus(t *testing.T) {
	t.Parallel()
	const bucket = "COMPONENT_STATUS"

	def := Definition{
		ID:   "component-status-allowed",
		Type: "expression",
		Actions: []Action{
			{
				Type:    ActionTypeUpdateKV,
				Bucket:  bucket,
				Key:     "some-component",
				Payload: map[string]any{"stage": "idle"},
			},
		},
	}
	if err := ValidateDefinition(def); err != nil {
		t.Fatalf("ValidateDefinition must permit update_kv into write-open %s, got: %v", bucket, err)
	}

	kv := newMockKVWriter()
	executor := &ActionExecutor{kvWriter: kv}
	action := Action{
		Type:    ActionTypeUpdateKV,
		Bucket:  bucket,
		Key:     "some-component",
		Payload: map[string]any{"stage": "idle"},
	}
	if err := executor.executeUpdateKV(context.Background(), action, &ExecutionContext{}); err != nil {
		t.Fatalf("executeUpdateKV must permit a write into write-open %s, got: %v", bucket, err)
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
