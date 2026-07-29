// kvspec.go is the MECHANISM half of the framework KV bucket catalog
// (framework-bucket-catalog): a name-free descriptor type (BucketSpec) plus the
// two-function acquisition seam every framework bucket goes through. The
// POPULATION half — the one literal catalog of 22 descriptors — lives in
// graph/kvcatalog.go, which is deliberately NOT here: product bucket names do
// not belong in the transport layer.
//
// Owners acquire through EnsureFrameworkBucket (create-or-open, reconcile to
// the declared policy, verify, fail the owner's Start closed on divergence).
// Readers bind through OpenFrameworkBucket (must-exist, NEVER creates, never
// reconciles). Owner enforcement is call-site selection — owners call Ensure,
// readers call Open; the framework does not verify caller identity at runtime.

package natsclient

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go/jetstream"
)

// BucketClass classifies what kind of state a framework bucket holds. It is
// descriptive (operator/reviewer-facing); no enforcement derives from it.
type BucketClass string

// Bucket classes.
const (
	// ClassAuthoritative is canonical domain state (ENTITY_STATES).
	ClassAuthoritative BucketClass = "authoritative"
	// ClassDerived is state rebuilt from authoritative state (the indexes).
	ClassDerived BucketClass = "derived"
	// ClassOperational is framework-internal correctness state (redelivery
	// stamps, readiness envelopes, ownership epochs).
	ClassOperational BucketClass = "operational"
	// ClassDiagnostic is observability-only state nothing in production reads.
	ClassDiagnostic BucketClass = "diagnostic"
)

// WritePolicy declares who may write a framework bucket. The framework-owned
// write-guard set is DERIVED from this field (Write == WriteOwnerOnly), never
// hand-maintained.
type WritePolicy string

// Write policies.
const (
	// WriteOwnerOnly means only the declared owner writes; a generic KV writer
	// (rule update_kv) is rejected at load and at runtime.
	WriteOwnerOnly WritePolicy = "owner-only"
	// WriteOpen means any component may write (e.g. COMPONENT_STATUS).
	WriteOpen WritePolicy = "open"
)

// CreatePosture declares how a bucket comes into existence.
type CreatePosture string

// Create postures.
const (
	// PostureOwnerCreates: the declared owner provisions the bucket through
	// EnsureFrameworkBucket; readers must-exist via OpenFrameworkBucket.
	PostureOwnerCreates CreatePosture = "owner-creates"
	// PostureReaderMustExist: no in-process owner provisions it; every binding
	// is must-exist. No shipped descriptor uses this posture today — it exists
	// because the catalog grammar declares both arms.
	PostureReaderMustExist CreatePosture = "reader-must-exist"
)

// RetentionKind discriminates a bucket's declared retention policy. Every
// switch over it MUST carry a default arm that fails closed: a Kind this
// binary does not know (a newer catalog on an older binary) is an invalid
// policy, never a silent no-op.
type RetentionKind string

// Retention kinds. All three are populated by shipped catalog rows; adding a
// future kind (e.g. bounded-storage's DiscardNew ceiling) is a new constant +
// params on RetentionPolicy + a reconcile arm — no shape change.
const (
	// RetentionNoLifecycle: no NATS TTL (MaxAge) or binding MaxBytes, ever —
	// correctness-critical no-eviction state (ADR-068). Acquisition strips a
	// foreign retention in place or fails closed.
	RetentionNoLifecycle RetentionKind = "no-lifecycle"
	// RetentionBoundedTTL: the declared TTL IS the contract (e.g.
	// OWNER_PRESENCE liveness). Acquisition converges MaxAge TO the declared
	// TTL — preserved, never stripped.
	RetentionBoundedTTL RetentionKind = "bounded-ttl"
	// RetentionUnmanaged: the framework guarantees no retention posture — an
	// explicit catalog fact (COMPONENT_STATUS), not an omission. Acquisition
	// does not reconcile retention.
	RetentionUnmanaged RetentionKind = "unmanaged"
)

// RetentionPolicy is a discriminated Kind plus its parameters — a struct, not
// a bare enum, so a future kind adds params without reshaping every consumer.
type RetentionPolicy struct {
	Kind RetentionKind
	// TTL is the declared bucket TTL. Meaningful (and required non-zero) for
	// RetentionBoundedTTL only.
	TTL time.Duration
}

// BucketSpec is one framework bucket's full declaration: identity, ownership,
// policy, and bucket configuration. The catalog (graph.KVCatalog) is a slice
// of these; every enforcement set the framework uses is a derived view.
type BucketSpec struct {
	Name        string
	Owner       string // the component/subsystem that provisions and writes it
	Description string // stamped on the bucket so `nats kv ls` self-describes

	Class     BucketClass
	Retention RetentionPolicy
	Write     WritePolicy
	Posture   CreatePosture

	History  uint8 // KV history depth (stream MaxMsgsPerSubject); 0 = 1
	Replicas int
}

// ErrorCodeBucketNotReady is the classified error code OpenFrameworkBucket
// returns for an absent must-exist bucket. Its value deliberately equals
// graph.ErrorCodeIndexNotReady (the code graph/query already emits for a
// not-sound-to-read index) so classified consumers handle "the bucket's owner
// has not provisioned it yet" with the same retry-with-backoff posture; a
// cross-pin test in graph asserts the two can never drift.
const ErrorCodeBucketNotReady = "index_not_ready"

// Validate fails closed on a descriptor this binary cannot enforce. The
// default arm is the load-bearing one: an unknown RetentionKind — a newer
// catalog on an older binary — must be an invalid-policy error, never a
// silently unapplied policy.
func (s BucketSpec) Validate() error {
	if s.Name == "" {
		return errs.WrapInvalid(errors.New("bucket spec has empty Name (not found in the catalog?)"),
			"KVSpec", "Validate", "validate framework bucket spec")
	}
	if s.Owner == "" {
		return errs.WrapInvalid(fmt.Errorf("bucket spec %q has empty Owner", s.Name),
			"KVSpec", "Validate", "validate framework bucket spec")
	}
	switch s.Retention.Kind {
	case RetentionNoLifecycle, RetentionUnmanaged:
		if s.Retention.TTL != 0 {
			return errs.WrapInvalid(
				fmt.Errorf("bucket spec %q declares retention kind %q with a TTL (%s); TTL is a bounded-ttl parameter",
					s.Name, s.Retention.Kind, s.Retention.TTL),
				"KVSpec", "Validate", "validate framework bucket spec")
		}
	case RetentionBoundedTTL:
		if s.Retention.TTL <= 0 {
			return errs.WrapInvalid(
				fmt.Errorf("bucket spec %q declares bounded-ttl retention without a positive TTL", s.Name),
				"KVSpec", "Validate", "validate framework bucket spec")
		}
	default:
		// FAIL CLOSED: an unknown kind must never degrade to "no policy".
		return errs.WrapInvalid(
			fmt.Errorf("bucket spec %q carries unknown retention kind %q; this binary cannot enforce its policy",
				s.Name, s.Retention.Kind),
			"KVSpec", "Validate", "validate framework bucket spec")
	}
	return nil
}

// kvConfigFor translates a spec into the creation config. Only a bounded-ttl
// descriptor carries a TTL into the bucket config.
func kvConfigFor(spec BucketSpec) jetstream.KeyValueConfig {
	cfg := jetstream.KeyValueConfig{
		Bucket:      spec.Name,
		Description: spec.Description,
		History:     spec.History,
		Replicas:    spec.Replicas,
	}
	if spec.Retention.Kind == RetentionBoundedTTL {
		cfg.TTL = spec.Retention.TTL
	}
	return cfg
}

// EnsureFrameworkBucket is the OWNER acquisition seam: create-or-open the
// bucket, reconcile the live bucket to the declared policy (retention per
// Kind; History to the declared value), verify by re-read, and return the
// handle. Any failure returns to the caller's Start, which the composition
// root fails closed (the component-start barrier) — that composition is why
// no post-start boot sweep exists anymore.
//
// Reconciling at acquisition (rather than at boot only) is what closes the
// post-boot-cutoff class: a dynamic component add/edit re-acquires its buckets
// through this seam and reconciles them right there.
func EnsureFrameworkBucket(ctx context.Context, c *Client, spec BucketSpec) (jetstream.KeyValue, error) {
	if c == nil {
		return nil, errs.WrapInvalid(errors.New("nil NATS client"),
			"KVSpec", "EnsureFrameworkBucket", "acquire framework bucket")
	}
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	// Posture is enforced grammar, not decoration: a reader-must-exist
	// descriptor may never be provisioned through the owner seam — its binders
	// go through OpenFrameworkBucket, which cannot create.
	if spec.Posture == PostureReaderMustExist {
		return nil, errs.WrapInvalid(
			fmt.Errorf("bucket spec %q declares posture %q; only OpenFrameworkBucket may bind it",
				spec.Name, spec.Posture),
			"KVSpec", "EnsureFrameworkBucket", "acquire framework bucket")
	}

	// Create-or-open through the proven concurrent-create resolution: an
	// existing bucket is adopted as-is here and reconciled to spec below.
	bucket, err := c.CreateKeyValueBucket(ctx, kvConfigFor(spec))
	if err != nil {
		return nil, err
	}

	js, err := c.JetStream()
	if err != nil {
		return nil, errs.WrapTransient(err, "KVSpec", "EnsureFrameworkBucket", "get JetStream context")
	}
	logger := c.logger
	if logger == nil {
		logger = slog.Default()
	}

	// Reconcile retention per declared Kind. Same seam, opposite outcomes,
	// driven by the descriptor: a no-lifecycle bucket has a foreign TTL
	// stripped; a bounded-ttl bucket has its declared TTL converged to.
	switch spec.Retention.Kind {
	case RetentionNoLifecycle:
		if rerr := ReconcileNoLifecycleRetention(ctx, js, spec.Name, logger); rerr != nil {
			return nil, rerr
		}
	case RetentionBoundedTTL:
		if rerr := reconcileBoundedTTL(ctx, js, spec.Name, spec.Retention.TTL, logger); rerr != nil {
			return nil, rerr
		}
	case RetentionUnmanaged:
		// Declared no-posture: the framework guarantees nothing about this
		// bucket's retention, so acquisition does not touch it.
	default:
		// FAIL CLOSED (unreachable after Validate, kept so a future refactor
		// that drops the Validate call cannot turn an unknown kind into a
		// silent no-op).
		return nil, errs.WrapInvalid(
			fmt.Errorf("bucket %q: unknown retention kind %q at reconcile", spec.Name, spec.Retention.Kind),
			"KVSpec", "EnsureFrameworkBucket", "reconcile retention")
	}

	// Reconcile History (stream MaxMsgsPerSubject) to the declared value. An
	// adopted bucket created by another path with a divergent History is
	// converged so bucket configuration is no longer decided by boot order
	// (the ENTITY_STATES History race, F1).
	if err := reconcileHistory(ctx, js, spec.Name, spec.History, logger); err != nil {
		return nil, err
	}

	return bucket, nil
}

// OpenFrameworkBucket is the READER acquisition seam: bind must-exist. It
// NEVER creates (a reader that creates is an emitter of divergent
// configuration — the #714 class) and NEVER reconciles (a reader that "fixes"
// stream config is the same bug class). An absent bucket yields a classified
// not-ready error naming the catalog Owner, so the operator reads "wait for /
// deploy the owner", not "the reader is broken".
func OpenFrameworkBucket(ctx context.Context, c *Client, spec BucketSpec) (jetstream.KeyValue, error) {
	if c == nil {
		return nil, errs.WrapInvalid(errors.New("nil NATS client"),
			"KVSpec", "OpenFrameworkBucket", "open framework bucket")
	}
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	bucket, err := c.GetKeyValueBucket(ctx, spec.Name)
	if err != nil {
		if errors.Is(err, jetstream.ErrBucketNotFound) {
			return nil, errs.ClassifiedCode(errs.ErrorTransient, ErrorCodeBucketNotReady,
				fmt.Errorf("framework bucket %q is not ready: its owner (%s) has not provisioned it in this deployment",
					spec.Name, spec.Owner))
		}
		return nil, err
	}
	return bucket, nil
}

// reconcileBoundedTTL converges a bucket's backing-stream MaxAge TO the
// declared TTL — the read → Update → re-read → assert shape of
// ReconcileNoLifecycleRetention, with the opposite goal: here the TTL is the
// contract (e.g. OWNER_PRESENCE liveness staleness floor) and must be
// preserved, never stripped.
func reconcileBoundedTTL(
	ctx context.Context, js jetstream.JetStream, bucket string, ttl time.Duration, logger *slog.Logger,
) error {
	if logger == nil {
		logger = slog.Default()
	}
	maxAge, _, err := kvBackingStreamRetention(ctx, js, bucket)
	if err != nil {
		return err
	}
	if maxAge == ttl {
		return nil
	}

	stream, gerr := js.Stream(ctx, kvStreamPrefix+bucket)
	if gerr != nil {
		return errs.WrapTransient(gerr, "KVSpec", "reconcileBoundedTTL",
			fmt.Sprintf("get backing stream to reconcile KV bucket %q", bucket))
	}
	info, ierr := stream.Info(ctx)
	if ierr != nil {
		return errs.WrapTransient(ierr, "KVSpec", "reconcileBoundedTTL",
			fmt.Sprintf("read backing stream info to reconcile KV bucket %q", bucket))
	}
	cfg := info.Config
	cfg.MaxAge = ttl
	if _, uerr := js.UpdateStream(ctx, cfg); uerr != nil {
		// Fall through to the assert: a denied update surfaces as the policy
		// violation it is, not a transient update error.
		logger.Warn("could not converge bounded-ttl framework bucket to its declared TTL; will re-assert fail-closed",
			slog.String("bucket", bucket),
			slog.Duration("declared_ttl", ttl),
			slog.Duration("actual_max_age", maxAge),
			slog.Any("error", uerr))
	} else {
		logger.Warn("converged bounded-ttl framework bucket to its declared TTL",
			slog.String("bucket", bucket),
			slog.Duration("declared_ttl", ttl),
			slog.Duration("previous_max_age", maxAge))
	}

	maxAge, _, err = kvBackingStreamRetention(ctx, js, bucket)
	if err != nil {
		return err
	}
	if maxAge != ttl {
		return errs.WrapFatal(
			fmt.Errorf("KV bucket %q MaxAge is %s after reconcile, declared bounded-ttl is %s", bucket, maxAge, ttl),
			"KVSpec", "reconcileBoundedTTL",
			fmt.Sprintf("KV bucket %q diverges from its declared bounded-ttl after reconcile", bucket))
	}
	return nil
}

// reconcileHistory converges a bucket's backing-stream MaxMsgsPerSubject (the
// KV History depth) to the declared value, WARNing with both values when an
// adopted bucket diverged. Reconciling DOWN discards stored revisions beyond
// the new depth — destructive-but-unread for the one shipped case
// (ENTITY_STATES adopted at History 3 from the retired tool-registration
// create; nothing reads depth > 1) — which is why the WARN names old and new.
func reconcileHistory(
	ctx context.Context, js jetstream.JetStream, bucket string, history uint8, logger *slog.Logger,
) error {
	if logger == nil {
		logger = slog.Default()
	}
	declared := int64(history)
	if declared == 0 {
		declared = 1 // KV semantics: History 0 means depth 1
	}

	stream, gerr := js.Stream(ctx, kvStreamPrefix+bucket)
	if gerr != nil {
		return errs.WrapTransient(gerr, "KVSpec", "reconcileHistory",
			fmt.Sprintf("get backing stream for KV bucket %q", bucket))
	}
	info, ierr := stream.Info(ctx)
	if ierr != nil {
		return errs.WrapTransient(ierr, "KVSpec", "reconcileHistory",
			fmt.Sprintf("read backing stream info for KV bucket %q", bucket))
	}
	if info.Config.MaxMsgsPerSubject == declared {
		return nil
	}

	logger.Warn("framework bucket History diverges from its catalog declaration; reconciling",
		slog.String("bucket", bucket),
		slog.Int64("adopted_history", info.Config.MaxMsgsPerSubject),
		slog.Int64("declared_history", declared))

	cfg := info.Config
	cfg.MaxMsgsPerSubject = declared
	if _, uerr := js.UpdateStream(ctx, cfg); uerr != nil {
		logger.Warn("could not reconcile framework bucket History; will re-assert fail-closed",
			slog.String("bucket", bucket),
			slog.Any("error", uerr))
	}

	// Verify by fresh re-read; still divergent → fatal, the owner must not
	// proceed on configuration it did not declare.
	info, ierr = stream.Info(ctx)
	if ierr != nil {
		return errs.WrapTransient(ierr, "KVSpec", "reconcileHistory",
			fmt.Sprintf("re-read backing stream info for KV bucket %q", bucket))
	}
	if info.Config.MaxMsgsPerSubject != declared {
		return errs.WrapFatal(
			fmt.Errorf("KV bucket %q History is %d after reconcile, catalog declares %d",
				bucket, info.Config.MaxMsgsPerSubject, declared),
			"KVSpec", "reconcileHistory",
			fmt.Sprintf("KV bucket %q History diverges from the catalog after reconcile", bucket))
	}
	return nil
}
