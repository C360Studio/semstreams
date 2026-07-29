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
