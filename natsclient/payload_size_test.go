package natsclient

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/pkg/errs"
)

// The guard semantics: equality passes (the server accepts exactly its limit),
// over refuses PERMANENT with the sentinel and the three operator facts.
func TestCheckPayloadSize(t *testing.T) {
	t.Parallel()

	if err := checkPayloadSize(10, 10, "seam", "target"); err != nil {
		t.Fatalf("payload at exactly the limit must pass, got %v", err)
	}
	if err := checkPayloadSize(5, 0, "seam", "target"); err != nil {
		t.Fatalf("limit<=0 disables the check, got %v", err)
	}

	err := checkPayloadSize(11, 10, "seam", "subject demo.subject")
	if err == nil {
		t.Fatal("oversized payload must refuse")
	}
	if !errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("refusal must carry ErrPayloadTooLarge, got %v", err)
	}
	if !errs.IsInvalid(err) {
		t.Fatalf("refusal must classify PERMANENT (Invalid) — transient was the gh#857 pathology; got class %v", errs.Classify(err))
	}
	for _, fact := range []string{"11", "10", "demo.subject"} {
		if !strings.Contains(err.Error(), fact) {
			t.Fatalf("refusal must name size, limit, and target; missing %q in %v", fact, err)
		}
	}
}

// A zero client has no connection: the limit falls back to the NATS default,
// never to "no check".
func TestServerPayloadLimitFallsBack(t *testing.T) {
	t.Parallel()
	c := &Client{}
	if got := c.serverPayloadLimit(); got != defaultMaxPayloadBytes {
		t.Fatalf("no-conn fallback = %d, want %d", got, defaultMaxPayloadBytes)
	}
	// The exported face (offload-threshold derivation, gh#857 D4) answers
	// identically — one derivation, two spellings.
	if got := c.ServerPayloadLimit(); got != defaultMaxPayloadBytes {
		t.Fatalf("ServerPayloadLimit no-conn fallback = %d, want %d", got, defaultMaxPayloadBytes)
	}
}

// Per-seam refusal tests. Each seam's guard runs BEFORE any connection or
// bucket I/O, so a zero-value client (no conn, nil bucket) proves both the
// refusal and its ordering: a permanent size refusal outranks transient
// connection state. Deleting one seam's guard call fails exactly that seam's
// test (task 1.4's mutation target).
func TestSeamGuards_RefuseOversizedBeforeIO(t *testing.T) {
	t.Parallel()

	oversized := make([]byte, defaultMaxPayloadBytes+1)
	c := &Client{}
	ctx := context.Background()

	t.Run("Publish", func(t *testing.T) {
		assertPayloadRefusal(t, c.Publish(ctx, "t.subject", oversized))
	})
	t.Run("PublishToStream", func(t *testing.T) {
		assertPayloadRefusal(t, c.PublishToStream(ctx, "t.subject", oversized))
	})
	t.Run("PublishToStreamWithMsgID", func(t *testing.T) {
		assertPayloadRefusal(t, c.PublishToStreamWithMsgID(ctx, "t.subject", oversized, "id-1"))
	})
	t.Run("PublishToStreamAsync", func(t *testing.T) {
		_, err := c.PublishToStreamAsync(ctx, "t.subject", oversized)
		assertPayloadRefusal(t, err)
	})
	t.Run("PublishBatchToStream", func(t *testing.T) {
		small := []byte("ok")
		err := c.PublishBatchToStream(ctx, "t.subject", [][]byte{small, oversized})
		assertPayloadRefusal(t, err)
		if !strings.Contains(err.Error(), "message 2 of 2") {
			t.Fatalf("batch refusal must name the offending message index: %v", err)
		}
	})
	t.Run("RequestWithHeaders", func(t *testing.T) {
		_, err := c.RequestWithHeaders(ctx, "t.subject", oversized, nil, 0)
		assertPayloadRefusal(t, err)
	})
	t.Run("CheckReplySize", func(t *testing.T) {
		assertPayloadRefusal(t, c.CheckReplySize(len(oversized), "t.subject"))
	})

	// KV lanes: nil bucket proves guard-before-I/O — reaching the bucket
	// would panic, so a returned refusal is the ordering proof.
	kv := c.NewKVStore(nil)
	t.Run("KVStore.Put", func(t *testing.T) {
		_, err := kv.Put(ctx, "k", oversized)
		assertPayloadRefusal(t, err)
	})
	t.Run("KVStore.Create", func(t *testing.T) {
		_, err := kv.Create(ctx, "k", oversized)
		assertPayloadRefusal(t, err)
	})
	t.Run("KVStore.Update", func(t *testing.T) {
		_, err := kv.Update(ctx, "k", oversized, 1)
		assertPayloadRefusal(t, err)
	})
}

// The explicit KVOptions override wins over the derived limit — the tests'
// and special cases' escape hatch, never a component-facing knob.
func TestKVStoreExplicitOverrideWins(t *testing.T) {
	t.Parallel()
	c := &Client{}
	kv := c.NewKVStore(nil, func(o *KVOptions) { o.MaxValueSize = 8 })
	if _, err := kv.Put(context.Background(), "k", []byte("123456789")); err == nil {
		t.Fatal("9 bytes must refuse under an 8-byte override")
	}
	if got := kv.effectiveValueLimit(); got != 8 {
		t.Fatalf("override must win over derived limit, got %d", got)
	}
}

func assertPayloadRefusal(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("oversized payload must refuse before any I/O")
	}
	if !errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("want ErrPayloadTooLarge (guard ran before conn/bucket state), got %v", err)
	}
	if !errs.IsInvalid(err) {
		t.Fatalf("refusal must classify Invalid, got %v", errs.Classify(err))
	}
}
