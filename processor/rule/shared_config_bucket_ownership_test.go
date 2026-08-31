package rule

import (
	"context"
	"strings"
	"testing"

	gtypes "github.com/c360studio/semstreams/graph"
)

// sharedConfigForgeryTargets are the two keys in the shared runtime
// configuration bucket that a generic update_kv must never reach. They are
// written as literals here (not the graph.* / config.* constants) on purpose:
// these tests assert the write-ownership BEHAVIOR, mirroring
// operational_bucket_ownership_test.go.
//
//   - platform_identity: create-once. A forged `id` whose org and stem match is
//     ADOPTED on the next boot — it is validated for grammar and byte budget,
//     nothing more — so a rule pack could move the authority every entity is
//     minted under. A forged id that does NOT match bricks the boot
//     permanently, because ADR-102 d7 forbids rewriting a minted authority.
//   - platform_identity_guard: overwriting the environment claim reopens the
//     concurrent-first-boot race it exists to decide (Codex B2).
//
// natsKVWriter.PutJSON plain-Puts, so neither key's create-once-ness protects
// it; the ownership guard is what protects it.
var sharedConfigForgeryTargets = []string{"platform_identity", "platform_identity_guard"}

const sharedConfigBucketLiteral = "semstreams_config"

// newMockKVWriterNATS builds the production writer with a NIL NATS client on
// purpose: every path that would touch it panics, so a test that passes proves
// the refusal came first.
func newMockKVWriterNATS() *natsKVWriter {
	return &natsKVWriter{}
}

// TestUpdateKV_RejectsSharedConfigBucket_AtLoad proves the load-time
// write-ownership guard rejects a rule update_kv whose literal target is the
// shared configuration bucket.
func TestUpdateKV_RejectsSharedConfigBucket_AtLoad(t *testing.T) {
	t.Parallel()
	for _, key := range sharedConfigForgeryTargets {
		t.Run(key, func(t *testing.T) {
			t.Parallel()
			def := Definition{
				ID:   "shared-config-load-guard",
				Type: "expression",
				Actions: []Action{
					{
						Type:    ActionTypeUpdateKV,
						Bucket:  sharedConfigBucketLiteral,
						Key:     key,
						Payload: map[string]any{"org": "acme", "stem": "dep", "id": "dep-forged"},
					},
				},
			}
			err := ValidateDefinition(def)
			if err == nil {
				t.Fatalf("ValidateDefinition must reject update_kv into %s/%s, got nil",
					sharedConfigBucketLiteral, key)
			}
			if !strings.Contains(err.Error(), "framework-owned") {
				t.Errorf("error must name the framework-owned bucket, got: %v", err)
			}
		})
	}
}

// TestUpdateKV_RejectsSharedConfigBucket_AtRuntime proves the runtime guard
// rejects the same write after variable substitution, so a dynamically
// resolved bucket name cannot smuggle a forged identity past the literal check.
func TestUpdateKV_RejectsSharedConfigBucket_AtRuntime(t *testing.T) {
	t.Parallel()
	for _, key := range sharedConfigForgeryTargets {
		t.Run(key, func(t *testing.T) {
			t.Parallel()
			executor := &ActionExecutor{kvWriter: newMockKVWriter()}
			action := Action{
				Type:    ActionTypeUpdateKV,
				Bucket:  "$message.bucket",
				Key:     key,
				Payload: map[string]any{"org": "acme", "stem": "dep", "id": "dep-forged"},
			}
			ec := &ExecutionContext{MessageData: map[string]any{"bucket": sharedConfigBucketLiteral}}

			err := executor.executeUpdateKV(context.Background(), action, ec)
			if err == nil {
				t.Fatalf("executeUpdateKV must reject a write into %s/%s, got nil",
					sharedConfigBucketLiteral, key)
			}
			if !strings.Contains(err.Error(), "framework-owned") {
				t.Errorf("error must name the framework-owned bucket, got: %v", err)
			}
		})
	}
}

// TestKVWriterRefusesCatalogedOwnerOnlyBucket is the BEHAVIORAL half of the
// generic-writer guard, and the one that bites when the acquisition path is
// bypassed rather than deleted.
//
// The contract-test scan (test/contract) proves the writer REFERENCES the
// catalog seam; a reference is satisfied by dead code. This proves the seam
// GOVERNS: getStore refuses a catalogued owner-only name before it can create
// or bind anything, which is the belt to executeUpdateKV's suspenders.
//
// The nil NATS client is the assertion: reaching it would panic, so a passing
// test proves the refusal happened before any acquisition was attempted.
func TestKVWriterRefusesCatalogedOwnerOnlyBucket(t *testing.T) {
	t.Parallel()
	writer := newMockKVWriterNATS()

	_, err := writer.acquireBucket(context.Background(), sharedConfigBucketLiteral)
	if err == nil {
		t.Fatalf("acquireBucket must refuse the catalogued owner-only bucket %s, got nil",
			sharedConfigBucketLiteral)
	}
	if !strings.Contains(err.Error(), "framework-owned") {
		t.Errorf("the refusal must name the framework-owned bucket, got: %v", err)
	}
}

// TestUpdateKV_StillAdmitsResearchEvidence pins the one shipped update_kv
// consumer against the ownership flip that closed the identity hole.
//
// RESEARCH_EVIDENCE (configs/rules/deep-research/02-collect-evidence.json) is
// the only bucket any shipped rule pack writes. It is deliberately NOT in the
// framework catalog — it is product state, outside the catalog by its own
// boundary rule — so both guards must keep admitting it and its acquisition
// keeps the writer's own create path. Narrowing a guard is only safe if you
// know what it was already admitting.
func TestUpdateKV_StillAdmitsResearchEvidence(t *testing.T) {
	t.Parallel()
	const researchEvidence = "RESEARCH_EVIDENCE"

	if gtypes.IsFrameworkOwnedBucket(researchEvidence) {
		t.Fatalf("%s must stay outside the framework-owned guard set", researchEvidence)
	}
	if _, catalogued := gtypes.SpecFor(researchEvidence); catalogued {
		t.Fatalf("%s must stay outside the catalog, so its acquisition path is unchanged", researchEvidence)
	}

	def := Definition{
		ID:   "research-evidence-admitted",
		Type: "expression",
		Actions: []Action{
			{
				Type:    ActionTypeUpdateKV,
				Bucket:  researchEvidence,
				Key:     "evidence-1",
				Payload: map[string]any{"claim": "x"},
			},
		},
	}
	if err := ValidateDefinition(def); err != nil {
		t.Fatalf("the shipped deep-research pack must still load: %v", err)
	}
}
