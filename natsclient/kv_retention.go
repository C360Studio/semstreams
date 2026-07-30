package natsclient

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go/jetstream"
)

// kvBackingStreamRetention reads a KV bucket's backing-stream lifecycle-eviction
// config: maxAge (the bucket TTL — age eviction) and maxBytes (size eviction;
// -1/0 = unlimited). That config lives on the KVStreamPrefix+<bucket> backing
// stream and is UpdateStream-mutable, which is what makes the reconcile below
// possible (prefix defined once in backing_stream_prefix.go). It mirrors
// backingStreamRetention in storage/objectstore/retention.go but reaches the KV
// backing stream through the JetStream context so the caller can subsequently
// UpdateStream to strip a binding config. Pairs with the pure
// CheckNoLifecycleRetention for the actual classification.
func kvBackingStreamRetention(
	ctx context.Context, js jetstream.JetStream, bucket string,
) (maxAge time.Duration, maxBytes int64, err error) {
	stream, err := js.Stream(ctx, KVStreamPrefix+bucket)
	if err != nil {
		return 0, 0, errs.WrapTransient(err, "KVStore", "kvBackingStreamRetention",
			fmt.Sprintf("get backing stream for KV bucket %q", bucket))
	}
	info, err := stream.Info(ctx)
	if err != nil {
		return 0, 0, errs.WrapTransient(err, "KVStore", "kvBackingStreamRetention",
			fmt.Sprintf("read backing stream info for KV bucket %q", bucket))
	}
	return info.Config.MaxAge, info.Config.MaxBytes, nil
}

// ReconcileNoLifecycleRetention is the boot-time D1 guard for a framework-owned
// KV bucket — ADR-068's "no reference-blind lifecycle retention on state the
// live graph references" invariant, applied to the derived-KV plane
// (framework-owned-bucket-guards; #622). It is the KV analogue of the shipped
// ObjectStore guard (storage/objectstore/retention.go) and runs TWO steps IN
// ORDER on the backing stream (KV_<bucket>):
//
//  1. Reconcile (strip-and-log). If the backing stream carries a binding
//     MaxAge/MaxBytes (e.g. a foreign 7-day TTL from a process that won the
//     get-or-create race, as in #610/#611, or an out-of-band NATS edit), clear it
//     via UpdateStream and emit a WARN naming the bucket and the removed
//     retention. Stripping stops FUTURE time/size eviction and deletes no stored
//     key, so it self-heals legacy buckets that a create-or-get path would
//     otherwise never reconcile.
//
//  2. Assert (fail-closed). Re-read the backing stream fresh and run the pure
//     CheckNoLifecycleRetention. If retention is STILL binding (the UpdateStream
//     was denied, or a concurrent writer re-set it), return a wrapped fatal
//     ErrGraphBucketRetention so startup fails closed rather than proceeding to
//     silently expire graph state a day later.
//
// The strip trigger and the final assert share ONE predicate
// (CheckNoLifecycleRetention) with the ObjectStore guard, so KV and ObjectStore
// can never diverge on what "binding" means.
func ReconcileNoLifecycleRetention(
	ctx context.Context, js jetstream.JetStream, bucket string, logger *slog.Logger,
) error {
	if logger == nil {
		logger = slog.Default()
	}

	maxAge, maxBytes, err := kvBackingStreamRetention(ctx, js, bucket)
	if err != nil {
		return err
	}

	// Clean boot: no binding retention, so there is nothing to strip and nothing
	// to re-assert. Return without a second read.
	if CheckNoLifecycleRetention(bucket, maxAge, maxBytes) == nil {
		return nil
	}

	// Reconcile: strip the binding retention config in place. Non-destructive —
	// stops future eviction, deletes nothing.
	stream, gerr := js.Stream(ctx, KVStreamPrefix+bucket)
	if gerr != nil {
		return errs.WrapTransient(gerr, "KVStore", "ReconcileNoLifecycleRetention",
			fmt.Sprintf("get backing stream to reconcile KV bucket %q", bucket))
	}
	info, ierr := stream.Info(ctx)
	if ierr != nil {
		return errs.WrapTransient(ierr, "KVStore", "ReconcileNoLifecycleRetention",
			fmt.Sprintf("read backing stream info to reconcile KV bucket %q", bucket))
	}
	cfg := info.Config
	cfg.MaxAge = 0
	cfg.MaxBytes = -1 // NATS "unlimited" sentinel (matches the ObjectStore precedent); CheckNoLifecycleRetention treats only MaxBytes>0 as binding
	if _, uerr := js.UpdateStream(ctx, cfg); uerr != nil {
		// Do NOT abort here — fall through to the assert, which re-reads and fails
		// closed. A denied update must surface as the retention violation it is, not
		// as a transient update error.
		logger.Warn("could not strip lifecycle retention from framework-owned KV bucket; will re-assert fail-closed",
			slog.String("bucket", bucket),
			slog.Duration("max_age", maxAge),
			slog.Int64("max_bytes", maxBytes),
			slog.Any("error", uerr))
	} else {
		logger.Warn("removed lifecycle retention from framework-owned KV bucket",
			slog.String("bucket", bucket),
			slog.Duration("removed_max_age", maxAge),
			slog.Int64("removed_max_bytes", maxBytes))
	}

	// Assert: re-read fresh (authoritative — a denied update left the old values)
	// and fail closed if still binding.
	maxAge, maxBytes, err = kvBackingStreamRetention(ctx, js, bucket)
	if err != nil {
		return err
	}
	if err := CheckNoLifecycleRetention(bucket, maxAge, maxBytes); err != nil {
		return errs.WrapFatal(err, "KVStore", "ReconcileNoLifecycleRetention",
			fmt.Sprintf("KV bucket %q retains lifecycle eviction after reconcile", bucket))
	}
	return nil
}
