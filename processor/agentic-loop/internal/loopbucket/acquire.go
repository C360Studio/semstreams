// Package loopbucket acquires the loop component's existing KV authority.
package loopbucket

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// AcquireOwner acquires and observes the loop authority before it is installed.
func AcquireOwner(ctx context.Context, js jetstream.KeyValueManager, name string) (jetstream.KeyValue, error) {
	if ctx == nil {
		return nil, fmt.Errorf("loop bucket %q: context is required", name)
	}
	bucket, err := js.KeyValue(ctx, name)
	if errors.Is(err, jetstream.ErrBucketNotFound) {
		bucket, err = js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: name, History: 10, TTL: 24 * time.Hour})
		if errors.Is(err, jetstream.ErrBucketExists) {
			bucket, err = js.KeyValue(ctx, name)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("acquire loop bucket %q: %w", name, err)
	}
	if bucket == nil {
		return nil, fmt.Errorf("loop bucket %q: acquisition returned no authority", name)
	}
	status, err := bucket.Status(ctx)
	if err != nil {
		return nil, fmt.Errorf("observe loop bucket %q: %w", name, err)
	}
	// The native status exposes its backing stream. Require that evidence from
	// the same observation as History/TTL, never zero-fill absent policy.
	backing, ok := status.(interface{ StreamInfo() *jetstream.StreamInfo })
	if !ok || backing.StreamInfo() == nil {
		return nil, fmt.Errorf("loop bucket %q: incomplete backing policy observation", name)
	}
	info := backing.StreamInfo()
	if status.History() != 10 || status.TTL() != 24*time.Hour || info.Config.MaxAge != 24*time.Hour || info.Config.MaxBytes > 0 {
		return nil, fmt.Errorf("loop bucket %q policy: observed History=%d TTL=%s MaxAge=%s MaxBytes=%d; require History=10 TTL=24h MaxAge=24h MaxBytes<=0 (no reconciliation)", name, status.History(), status.TTL(), info.Config.MaxAge, info.Config.MaxBytes)
	}
	return bucket, nil
}
