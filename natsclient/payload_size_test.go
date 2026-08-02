package natsclient

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/pkg/errs"
)

// testCachedLimit is the advertised limit the seam tests seed. It is a TEST
// FIXTURE standing in for a server's advertisement — the production tree
// carries no compiled-in copy of the wire limit (payload-bounds spec).
const testCachedLimit = 1024 * 1024

// newLimitedTestClient builds a connection-less client whose cached
// advertised limit is seeded to limit, as if a server had advertised it on a
// prior connection. The seams guard against the CACHED value while the
// connection itself is absent — exactly the disconnected-after-connect state.
func newLimitedTestClient(limit int64) *Client {
	c := &Client{}
	c.advertisedPayloadLimit.Store(limit)
	return c
}

// The guard semantics: equality passes (the server accepts exactly its limit),
// over refuses PERMANENT with the sentinel and the three operator facts.
func TestCheckPayloadSize(t *testing.T) {
	t.Parallel()

	if err := checkPayloadSize(10, 10, "seam", "target"); err != nil {
		t.Fatalf("payload at exactly the limit must pass, got %v", err)
	}
	if err := checkPayloadSize(5, 0, "seam", "target"); err != nil {
		t.Fatalf("limit<=0 (unknown) disables the check, got %v", err)
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

// An UNKNOWN limit must never produce a permanent size verdict (payload-bounds
// spec). A never-connected client has no advertisement to guard against: the
// limit is 0, the guard disables, and an oversized send surfaces the honest
// CONNECTION-state error — never ErrPayloadTooLarge against a server we never
// talked to. This is the false-permanent regression test: the retired
// compiled-in fallback turned exactly this case into a permanent refusal.
func TestServerPayloadLimitUnknown_NoFalsePermanentVerdict(t *testing.T) {
	t.Parallel()
	c := &Client{}
	if got := c.serverPayloadLimit(); got != 0 {
		t.Fatalf("never-connected limit must be 0 (unknown), got %d", got)
	}
	if got := c.ServerPayloadLimit(); got != 0 {
		t.Fatalf("ServerPayloadLimit never-connected must be 0 (unknown), got %d", got)
	}

	oversized := make([]byte, testCachedLimit+1)
	err := c.Publish(context.Background(), "t.subject", oversized)
	if err == nil {
		t.Fatal("publish on a never-connected client must fail")
	}
	if errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("unknown limit produced a permanent size verdict: %v", err)
	}
	if !errors.Is(err, ErrNotConnected) {
		t.Fatalf("connection state must win while the limit is unknown, got %v", err)
	}
}

// Once a server HAS advertised, the cached value answers across disconnects —
// causal, never invented.
func TestServerPayloadLimitCached_AnswersWhileDisconnected(t *testing.T) {
	t.Parallel()
	c := newLimitedTestClient(4096)
	if got := c.serverPayloadLimit(); got != 4096 {
		t.Fatalf("cached advertisement must answer while disconnected, got %d", got)
	}
	if got := c.ServerPayloadLimit(); got != 4096 {
		t.Fatalf("ServerPayloadLimit must report the cached advertisement, got %d", got)
	}
}

// A RAISED server limit propagates: with 8MiB cached, a 2MiB payload passes
// the size guard — the error that surfaces on this conn-less client is
// ErrNotConnected, proving the guard did not manufacture a permanent verdict
// from a stale smaller number. (The retired 1MB fallback would have refused
// this payload permanently.)
func TestSeamGuards_RaisedLimitPassesLargerPayload(t *testing.T) {
	t.Parallel()
	c := newLimitedTestClient(8 << 20)
	payload := make([]byte, 2<<20)

	err := c.Publish(context.Background(), "t.subject", payload)
	if errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("2MiB under a cached 8MiB advertisement must pass the guard, got %v", err)
	}
	if !errors.Is(err, ErrNotConnected) {
		t.Fatalf("expected the connection-state error past the guard, got %v", err)
	}

	if err := c.PublishToStream(context.Background(), "t.subject", payload); errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("stream funnel must honor the raised cached limit, got %v", err)
	}
}

// Per-seam refusal tests. Each seam's guard runs BEFORE any connection or
// bucket I/O, so a connection-less client with a seeded cached limit (no
// conn, nil bucket) proves both the refusal and its ordering: a permanent
// size refusal outranks transient connection state. Deleting one seam's
// guard call fails exactly that seam's test (task 1.4's mutation target):
// without the guard the seam returns ErrNotConnected/panics instead of
// ErrPayloadTooLarge.
func TestSeamGuards_RefuseOversizedBeforeIO(t *testing.T) {
	t.Parallel()

	oversized := make([]byte, testCachedLimit+1)
	c := newLimitedTestClient(testCachedLimit)
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
	t.Run("PublishToStreamWithAck", func(t *testing.T) {
		_, err := c.PublishToStreamWithAck(ctx, "t.subject", oversized)
		assertPayloadRefusal(t, err)
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
	t.Run("Request", func(t *testing.T) {
		_, err := c.Request(ctx, "t.subject", oversized, 0)
		assertPayloadRefusal(t, err)
	})
	t.Run("RequestWithRetry", func(t *testing.T) {
		_, err := c.requestMsgWithRetry(ctx, "t.subject", oversized, 0, DefaultRetryConfig())
		assertPayloadRefusal(t, err)
	})
	t.Run("RequestReady", func(t *testing.T) {
		_, err := c.requestMsgReady(ctx, "t.subject", oversized, 0, 0)
		assertPayloadRefusal(t, err)
	})
	t.Run("ReplyWithHeaders", func(t *testing.T) {
		assertPayloadRefusal(t, c.ReplyWithHeaders(ctx, "_INBOX.t", oversized, nil))
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
// and special cases' escape hatch, never a component-facing knob. Its refusal
// names the LOCAL admission bound as the source, not the server: the operator
// set this number, and the message must send them to the right knob.
func TestKVStoreExplicitOverrideWins(t *testing.T) {
	t.Parallel()
	c := &Client{}
	kv := c.NewKVStore(nil, func(o *KVOptions) { o.MaxValueSize = 8 })
	_, err := kv.Put(context.Background(), "k", []byte("123456789"))
	if err == nil {
		t.Fatal("9 bytes must refuse under an 8-byte override")
	}
	if !strings.Contains(err.Error(), "local admission bound") {
		t.Fatalf("override refusal must name the local admission bound, not the server: %v", err)
	}
	if strings.Contains(err.Error(), "server limit") {
		t.Fatalf("override refusal must not blame the server for a local number: %v", err)
	}
	if got := kv.effectiveValueLimit(); got != 8 {
		t.Fatalf("override must win over derived limit, got %d", got)
	}
}

// A KVStore whose owning client has never seen an advertisement (and no
// override) has NO known ceiling: the pre-send check disables and the write
// surfaces bucket/connection state, never a permanent size verdict.
func TestKVStoreUnknownLimit_NoCheck(t *testing.T) {
	t.Parallel()
	c := &Client{}
	kv := c.NewKVStore(nil)
	if got := kv.effectiveValueLimit(); got != 0 {
		t.Fatalf("unknown limit must resolve to 0 (no check), got %d", got)
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
