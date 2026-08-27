package embedding

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/nats-io/nats.go/jetstream"
)

// replayKV is a memKV whose WatchAll REPLAYS the current key set as the initial
// snapshot (each current value, then the nil end-of-snapshot sentinel), modelling a real
// jetstream WatchAll's DeliverLastPerSubject. watchableKV deliberately delivers an EMPTY
// snapshot (tests seed after Start there), so it cannot exercise ScanFailed's collection.
type replayKV struct {
	*memKV
}

func (r *replayKV) WatchAll(_ context.Context, _ ...jetstream.WatchOpt) (jetstream.KeyWatcher, error) {
	r.memKV.mu.Lock()
	entries := make([]jetstream.KeyValueEntry, 0, len(r.memKV.data))
	for k, v := range r.memKV.data {
		entries = append(entries, &memKVEntry{
			key: k, value: append([]byte(nil), v...), rev: r.memKV.revs[k], op: jetstream.KeyValuePut,
		})
	}
	r.memKV.mu.Unlock()

	ch := make(chan jetstream.KeyValueEntry, len(entries)+1)
	for _, e := range entries {
		ch <- e
	}
	ch <- nil // end-of-snapshot sentinel
	return &memWatcher{updates: ch}, nil
}

// TestScanFailed_SeedsFromSnapshot proves the current-failed bootstrap scan (#613)
// collects exactly the durable StatusFailed records from the WatchAll snapshot, with
// their bounded reasons, and ignores generated/pending records.
func TestScanFailed_SeedsFromSnapshot(t *testing.T) {
	ctx := context.Background()
	kv := &replayKV{memKV: newMemKV()}
	s := NewStorage(kv, newMemKV())

	save := func(rec *Record) {
		data, err := json.Marshal(rec)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if _, err := kv.Put(ctx, rec.EntityID, data); err != nil {
			t.Fatalf("put: %v", err)
		}
	}
	save(&Record{EntityID: "acme.ops.src.embed.record.fail-1", Status: StatusFailed, Reason: "connection_refused", ErrorMsg: "dial: refused"})
	save(&Record{EntityID: "acme.ops.src.embed.record.fail-2", Status: StatusFailed, Reason: "content_error"})
	save(&Record{EntityID: "acme.ops.src.embed.record.ok", Status: StatusGenerated, Vector: []float32{1, 2, 3}})
	save(&Record{EntityID: "acme.ops.src.embed.record.pending", Status: StatusPending, SourceText: "x"})

	got, err := s.ScanFailed(ctx)
	if err != nil {
		t.Fatalf("ScanFailed: %v", err)
	}

	byID := map[string]string{}
	for _, e := range got {
		byID[e.EntityID] = e.Reason
	}
	if len(byID) != 2 {
		t.Fatalf("ScanFailed returned %d entries (%v), want exactly the 2 failed records", len(byID), byID)
	}
	if byID["acme.ops.src.embed.record.fail-1"] != "connection_refused" || byID["acme.ops.src.embed.record.fail-2"] != "content_error" {
		t.Fatalf("failed reasons not seeded correctly: %v", byID)
	}
	if _, ok := byID["acme.ops.src.embed.record.ok"]; ok {
		t.Error("a generated record must not be seeded as failed")
	}
	if _, ok := byID["acme.ops.src.embed.record.pending"]; ok {
		t.Error("a pending record must not be seeded as failed")
	}
}
