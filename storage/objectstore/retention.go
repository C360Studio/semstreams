package objectstore

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go/jetstream"
)

// objectStoreStreamPrefix is the NATS naming convention for an ObjectStore's
// backing stream: OBJ_<bucket> (nats.go jetstream/object.go, objNameTmpl =
// "OBJ_%s"). The store's lifecycle-eviction config — MaxAge (the ObjectStore
// TTL) and MaxBytes (a size cap) — lives on that stream's Config and is
// UpdateStream-mutable, which is what makes the D2 reconcile below possible.
const objectStoreStreamPrefix = "OBJ_"

// backingStreamRetention reads a content ObjectStore's backing-stream
// lifecycle-eviction config: maxAge (the ObjectStore TTL — age eviction) and
// maxBytes (size eviction; -1/0 = unlimited). It mirrors
// natsclient.BucketRetention but reaches the ObjectStore's backing stream
// (OBJ_<bucket>) through the JetStream context rather than a KV bucket status.
//
// Callers on content stores assert both are non-binding: age/size eviction is
// reachability-blind and would strand a live-graph entity pointing at content
// that silently expired (ADR-068; #600). Pairs with the pure
// natsclient.CheckNoLifecycleRetention for the actual classification.
func backingStreamRetention(
	ctx context.Context, js jetstream.JetStream, bucket string,
) (maxAge time.Duration, maxBytes int64, err error) {
	stream, err := js.Stream(ctx, objectStoreStreamPrefix+bucket)
	if err != nil {
		return 0, 0, errs.WrapTransient(err, "Store", "backingStreamRetention",
			fmt.Sprintf("get backing stream for content store %q", bucket))
	}
	info, err := stream.Info(ctx)
	if err != nil {
		return 0, 0, errs.WrapTransient(err, "Store", "backingStreamRetention",
			fmt.Sprintf("read backing stream info for content store %q", bucket))
	}
	return info.Config.MaxAge, info.Config.MaxBytes, nil
}

// reconcileNoLifecycleRetention is the boot-time D2 guard for a content
// ObjectStore — ADR-068's "no reference-blind lifecycle retention on state the
// live graph references" invariant, applied to content-addressed ObjectStores
// (#600/#616). It runs TWO steps IN ORDER on the backing stream (OBJ_<bucket>):
//
//  1. Reconcile (strip-and-log). If the backing stream carries a binding
//     MaxAge/MaxBytes (e.g. the historical 24h TTL from before this contract, or
//     an out-of-band NATS edit), clear it via UpdateStream and emit a WARN naming
//     the bucket and the removed retention. Stripping stops FUTURE time/size
//     eviction and deletes no stored object, so it self-heals legacy buckets that
//     the constructor's create-or-get path would otherwise never reconcile.
//
//  2. Assert (fail-closed). Re-read the backing stream fresh and run the pure
//     natsclient.CheckNoLifecycleRetention. If retention is STILL binding (the
//     UpdateStream was denied, or a concurrent writer re-set it), return a wrapped
//     fatal ErrGraphBucketRetention so startup fails closed rather than proceeding
//     to silently expire evidence a day later.
//
// The strip trigger and the final assert share ONE predicate
// (natsclient.CheckNoLifecycleRetention) so they can never drift on what "binding"
// means.
func reconcileNoLifecycleRetention(
	ctx context.Context, js jetstream.JetStream, bucket string, logger *slog.Logger,
) error {
	maxAge, maxBytes, err := backingStreamRetention(ctx, js, bucket)
	if err != nil {
		return err
	}

	// Clean boot: no binding retention, so there is nothing to strip and nothing to
	// re-assert. Return without a second read.
	if natsclient.CheckNoLifecycleRetention(bucket, maxAge, maxBytes) == nil {
		return nil
	}

	// Reconcile: strip the binding retention config in place. Non-destructive —
	// stops future eviction, deletes nothing.
	stream, gerr := js.Stream(ctx, objectStoreStreamPrefix+bucket)
	if gerr != nil {
		return errs.WrapTransient(gerr, "Store", "reconcileNoLifecycleRetention",
			fmt.Sprintf("get backing stream to reconcile content store %q", bucket))
	}
	info, ierr := stream.Info(ctx)
	if ierr != nil {
		return errs.WrapTransient(ierr, "Store", "reconcileNoLifecycleRetention",
			fmt.Sprintf("read backing stream info to reconcile content store %q", bucket))
	}
	cfg := info.Config
	cfg.MaxAge = 0
	cfg.MaxBytes = -1 // NATS "unlimited" sentinel; the ObjectStore backing stream's own default
	if _, uerr := js.UpdateStream(ctx, cfg); uerr != nil {
		// Do NOT abort here — fall through to the assert, which re-reads and fails
		// closed. A denied update must surface as the retention violation it is, not
		// as a transient update error.
		logger.Warn("could not strip lifecycle retention from content store; will re-assert fail-closed",
			slog.String("bucket", bucket),
			slog.Duration("max_age", maxAge),
			slog.Int64("max_bytes", maxBytes),
			slog.Any("error", uerr))
	} else {
		logger.Warn("removed lifecycle retention from content store",
			slog.String("bucket", bucket),
			slog.Duration("removed_max_age", maxAge),
			slog.Int64("removed_max_bytes", maxBytes))
	}

	// Assert: re-read fresh (authoritative — a denied update left the old values)
	// and fail closed if still binding.
	maxAge, maxBytes, err = backingStreamRetention(ctx, js, bucket)
	if err != nil {
		return err
	}
	if err := natsclient.CheckNoLifecycleRetention(bucket, maxAge, maxBytes); err != nil {
		return errs.WrapFatal(err, "Store", "reconcileNoLifecycleRetention",
			fmt.Sprintf("content store %q retains lifecycle eviction after reconcile", bucket))
	}
	return nil
}
