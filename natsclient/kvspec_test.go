package natsclient

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validNoLifecycleSpec() BucketSpec {
	return BucketSpec{
		Name:      "KVSPEC_TEST_BUCKET",
		Owner:     "kvspec-test-owner",
		Class:     ClassDerived,
		Retention: RetentionPolicy{Kind: RetentionNoLifecycle},
		Write:     WriteOwnerOnly,
		Posture:   PostureOwnerCreates,
		History:   1,
		Replicas:  1,
	}
}

// TestBucketSpec_Validate_UnknownKindFailsClosed is the fail-closed default
// arm: a retention Kind this binary does not know (a newer catalog on an older
// binary) must be an invalid-policy error — never a silent no-op that applies
// no policy.
func TestBucketSpec_Validate_UnknownKindFailsClosed(t *testing.T) {
	spec := validNoLifecycleSpec()
	spec.Retention.Kind = RetentionKind("discard-new-ceiling") // a future kind

	err := spec.Validate()
	require.Error(t, err, "an unknown retention kind must fail closed, never no-op")
	assert.True(t, errs.IsInvalid(err), "the failure must classify as invalid policy, not transient")
	assert.Contains(t, err.Error(), "discard-new-ceiling", "the error must name the unknown kind")
	assert.Contains(t, err.Error(), spec.Name, "the error must name the bucket")
}

// TestEnsureFrameworkBucket_UnknownKindFailsClosed proves the seam itself
// refuses an unknown Kind before touching NATS: acquisition with an
// unenforceable policy must error, not create-then-skip-reconcile.
func TestEnsureFrameworkBucket_UnknownKindFailsClosed(t *testing.T) {
	client, err := NewClient("nats://127.0.0.1:4222") // never connected; must not be reached
	require.NoError(t, err)
	spec := validNoLifecycleSpec()
	spec.Retention.Kind = RetentionKind("discard-new-ceiling")

	_, err = EnsureFrameworkBucket(context.Background(), client, spec)
	require.Error(t, err)
	assert.True(t, errs.IsInvalid(err), "unknown kind must be an invalid-policy error")
	assert.Contains(t, err.Error(), "discard-new-ceiling")
}

// TestOpenFrameworkBucket_UnknownKindFailsClosed: the reader seam validates
// the same way — a reader binding under an unenforceable descriptor is a
// config error, not a lucky read.
// TestEnsureFrameworkBucket_RequiresOwnerCreatesExactly: the owner seam
// demands Posture == owner-creates by EXACT match — reader-must-exist is
// rejected, and (belt, unreachable past Validate) so would any future arm.
func TestEnsureFrameworkBucket_RequiresOwnerCreatesExactly(t *testing.T) {
	client, err := NewClient("nats://127.0.0.1:4222") // never connected; must not be reached
	require.NoError(t, err)
	spec := validNoLifecycleSpec()
	spec.Posture = PostureReaderMustExist

	_, err = EnsureFrameworkBucket(context.Background(), client, spec)
	require.Error(t, err)
	assert.True(t, errs.IsInvalid(err))
	assert.Contains(t, err.Error(), string(PostureOwnerCreates),
		"the rejection must name the required posture")

	// A reader may bind an owner-creates bucket: Open validates both postures.
	// (No NATS here — Validate alone must not reject the combination; the
	// unreachable-NATS Get error is fine, a Validate error is not.)
	openSpec := validNoLifecycleSpec()
	_, err = OpenFrameworkBucket(context.Background(), client, openSpec)
	if err != nil {
		assert.False(t, errs.IsInvalid(err),
			"Open on an owner-creates spec must not fail validation (got: %v)", err)
	}
}

func TestOpenFrameworkBucket_UnknownKindFailsClosed(t *testing.T) {
	client, err := NewClient("nats://127.0.0.1:4222")
	require.NoError(t, err)
	spec := validNoLifecycleSpec()
	spec.Retention.Kind = RetentionKind("discard-new-ceiling")

	_, err = OpenFrameworkBucket(context.Background(), client, spec)
	require.Error(t, err)
	assert.True(t, errs.IsInvalid(err))
}

func TestBucketSpec_Validate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*BucketSpec)
		wantErr string // empty = valid
	}{
		{name: "valid no-lifecycle", mutate: func(*BucketSpec) {}},
		{
			name: "valid bounded-ttl",
			mutate: func(s *BucketSpec) {
				s.Retention = RetentionPolicy{Kind: RetentionBoundedTTL, TTL: 120 * time.Second}
			},
		},
		{
			name:   "valid unmanaged",
			mutate: func(s *BucketSpec) { s.Retention = RetentionPolicy{Kind: RetentionUnmanaged} },
		},
		{
			name:    "empty name",
			mutate:  func(s *BucketSpec) { s.Name = "" },
			wantErr: "empty Name",
		},
		{
			name:    "empty owner",
			mutate:  func(s *BucketSpec) { s.Owner = "" },
			wantErr: "empty Owner",
		},
		{
			name: "no-lifecycle with a TTL is contradictory",
			mutate: func(s *BucketSpec) {
				s.Retention = RetentionPolicy{Kind: RetentionNoLifecycle, TTL: time.Hour}
			},
			wantErr: "TTL is a bounded-ttl parameter",
		},
		{
			name: "bounded-ttl without a TTL",
			mutate: func(s *BucketSpec) {
				s.Retention = RetentionPolicy{Kind: RetentionBoundedTTL}
			},
			wantErr: "without a positive TTL",
		},
		{
			name:    "unknown kind fails closed",
			mutate:  func(s *BucketSpec) { s.Retention.Kind = "not-a-kind" },
			wantErr: "unknown retention kind",
		},
		// The descriptor grammar fails CLOSED on every discriminated field: a
		// zero value is not a default, it is an invalid descriptor. An empty
		// Write in particular would silently drop the bucket out of the
		// derived owned set (fail-open on the write guard).
		{
			name:    "empty write policy fails closed",
			mutate:  func(s *BucketSpec) { s.Write = "" },
			wantErr: "unknown write policy",
		},
		{
			name:    "typoed write policy fails closed",
			mutate:  func(s *BucketSpec) { s.Write = "owner-onIy" },
			wantErr: "unknown write policy",
		},
		{
			name:    "empty class fails closed",
			mutate:  func(s *BucketSpec) { s.Class = "" },
			wantErr: "unknown class",
		},
		{
			name:    "unknown class fails closed",
			mutate:  func(s *BucketSpec) { s.Class = "derivative" },
			wantErr: "unknown class",
		},
		{
			name:    "empty posture fails closed",
			mutate:  func(s *BucketSpec) { s.Posture = "" },
			wantErr: "unknown create posture",
		},
		{
			name:    "unknown posture fails closed",
			mutate:  func(s *BucketSpec) { s.Posture = "owner-create" },
			wantErr: "unknown create posture",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := validNoLifecycleSpec()
			tt.mutate(&spec)
			err := spec.Validate()
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.True(t, strings.Contains(err.Error(), tt.wantErr),
				"error %q must contain %q", err.Error(), tt.wantErr)
		})
	}
}

// TestEnsureFrameworkBucket_ReaderPostureRejected: Posture is enforced grammar
// — a reader-must-exist descriptor may never be provisioned through the owner
// seam (its binders go through OpenFrameworkBucket, which cannot create). This
// is the field's consumer; without it Posture would be a phantom.
func TestEnsureFrameworkBucket_ReaderPostureRejected(t *testing.T) {
	client, err := NewClient("nats://127.0.0.1:4222") // never connected; must not be reached
	require.NoError(t, err)
	spec := validNoLifecycleSpec()
	spec.Posture = PostureReaderMustExist

	_, err = EnsureFrameworkBucket(context.Background(), client, spec)
	require.Error(t, err)
	assert.True(t, errs.IsInvalid(err), "reader-posture provisioning must be an invalid-spec error")
	assert.Contains(t, err.Error(), string(PostureReaderMustExist))
}
