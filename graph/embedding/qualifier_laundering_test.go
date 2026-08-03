package embedding

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/nats-io/nats.go/jetstream"
)

// TestQualifier_HopTwoFailureDoesNotLaunderAQualifiedSuccess drives the BLOCKING chain
// review found, link by link, and asserts the entity ends up correctly qualified.
//
// The chain (every step verified against the code, not assumed):
//
//  1. Hop 1's gate refuses the offloaded lane and queues `pending + content_excluded`
//     with the entity's inline text and NO StorageRef.
//  2. Hop 2 hits any ordinary failure — embedder down, timeout, dedup fault. SaveFailed
//     copies the existing record and does `existing.Reason = reason`, OVERWRITING the
//     qualifier. The record is now `failed + content_error`, still carrying SourceText.
//  3. Restart. `reprocessFailed` re-processes a FAILED record delivered in the snapshot,
//     and hop 2 reaches EMBEDDING_INDEX before hop 1 reaches ENTITY_STATES by
//     construction (initStorageAndWorker runs before waitForDependenciesAndStartWatcher).
//     The qualifier carry-forward is an exact match on the KNOWN qualifier, so a FAILURE
//     reason fails closed to unqualified; getSourceText takes the inline branch (no
//     StorageRef) and SUCCEEDS. Hop 2 writes `generated + ""` — a record that claims a
//     complete vector for an entity whose body is still unreachable.
//  4. Hop 1's re-delivery at the SAME revision then meets the guard. Against the first
//     fix's "an UNQUALIFIED generated vector stands" test, that laundered record is
//     frozen: not enumerable (a content_excluded scan misses it) and not repairable
//     (the re-queue is skipped) until a new ENTITY_STATES revision arrives.
//
// Matching the guard on terminal QUALITY rather than on presence is what closes it.
func TestQualifier_HopTwoFailureDoesNotLaunderAQualifiedSuccess(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	const entityID = "acme.ops.docs.sys.doc.laundered"
	const rev = uint64(5)
	const inlineText = "hydraulic pump service report"

	index := newMemKV()
	s := NewStorage(index, newMemKV())

	// --- 1. hop 1: gate refuses the offloaded lane, queues inline text QUALIFIED ---
	saved, err := s.SavePendingGuarded(ctx, &Record{
		EntityID:       entityID,
		SourceText:     inlineText,
		SourceRevision: rev,
		Reason:         ReasonContentExcluded,
	})
	if err != nil || !saved {
		t.Fatalf("hop-1 qualified queue: saved=%v err=%v", saved, err)
	}

	// --- 2. hop 2 fails for an unrelated reason; SaveFailed overwrites the qualifier ---
	if err := s.SaveFailed(ctx, entityID, "embedder unreachable", failReasonConnectionRefused, rev); err != nil {
		t.Fatalf("SaveFailed: %v", err)
	}
	laundered, err := s.GetEmbedding(ctx, entityID)
	if err != nil || laundered == nil {
		t.Fatalf("GetEmbedding: rec=%v err=%v", laundered, err)
	}
	if laundered.Status != StatusFailed || laundered.Reason != failReasonConnectionRefused {
		t.Fatalf("step 2 precondition: record = %q/%q, want failed/connection_refused", laundered.Status, laundered.Reason)
	}
	if laundered.SourceText != inlineText || laundered.StorageRef != nil {
		t.Fatalf("step 2 precondition: the failed record must still carry the inline text and no StorageRef; got text=%q ref=%v",
			laundered.SourceText, laundered.StorageRef)
	}

	// --- 3. restart: hop 2 re-processes the FAILED record from the snapshot ---
	// duringSnapshot=true is what the production loop passes for a snapshot entry
	// (worker.go: duringSnapshot := !w.snapshotComplete.Load()).
	w := NewWorker(s, okEmbedder(), index, discardLogger()).WithMaxSourceTextLen(4000)
	w.ctx = ctx
	data, err := json.Marshal(laundered)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	entry := &memKVEntry{key: entityID, value: data, rev: 1, op: jetstream.KeyValuePut}

	_, _, terminal, outcome, _ := w.handleKVEntry(entry, true, 0)
	if !terminal || outcome != OutcomeGenerated {
		t.Fatalf("restart reprocess = (terminal=%v, outcome=%v), want a generated terminal", terminal, outcome)
	}
	afterReprocess, err := s.GetEmbedding(ctx, entityID)
	if err != nil || afterReprocess == nil {
		t.Fatalf("GetEmbedding: rec=%v err=%v", afterReprocess, err)
	}
	// This is the laundering itself, asserted rather than glossed: hop 2 cannot recover
	// the qualifier here — SaveFailed destroyed it and the record has no StorageRef to
	// re-observe the condition from. The record now LOOKS complete.
	if afterReprocess.Status != StatusGenerated || afterReprocess.Reason != "" {
		t.Fatalf("step 3 precondition: reprocess = %q/%q, want generated/\"\" (the laundering step)",
			afterReprocess.Status, afterReprocess.Reason)
	}

	// --- 4. hop 1 re-delivers the SAME revision; the guard must NOT skip ---
	// This is the step the fix changes. The entity's body is still unreachable, so hop 1
	// still qualifies its write.
	saved, err = s.SavePendingGuarded(ctx, &Record{
		EntityID:       entityID,
		SourceText:     inlineText,
		SourceRevision: rev,
		Reason:         ReasonContentExcluded,
	})
	if err != nil {
		t.Fatalf("hop-1 re-delivery: %v", err)
	}
	if !saved {
		t.Fatal("hop-1's re-delivery was SKIPPED over a laundered `generated + \"\"` record: " +
			"the entity is frozen looking complete while its body is unreachable — " +
			"not enumerable (a content_excluded scan misses it) and not repairable")
	}

	// Hop 2 then re-terminalizes it, carrying the qualifier off the pending record.
	pending, err := s.GetEmbedding(ctx, entityID)
	if err != nil || pending == nil {
		t.Fatalf("GetEmbedding: rec=%v err=%v", pending, err)
	}
	data, err = json.Marshal(pending)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if _, _, terminal, outcome, _ = w.handleKVEntry(
		&memKVEntry{key: entityID, value: data, rev: 2, op: jetstream.KeyValuePut}, false, 0,
	); !terminal || outcome != OutcomeGenerated {
		t.Fatalf("hop-2 after re-queue = (terminal=%v, outcome=%v), want a generated terminal", terminal, outcome)
	}

	final, err := s.GetEmbedding(ctx, entityID)
	if err != nil || final == nil {
		t.Fatalf("GetEmbedding: rec=%v err=%v", final, err)
	}
	if final.Status != StatusGenerated || final.Reason != ReasonContentExcluded {
		t.Fatalf("final record = %q/%q, want generated/%q: the entity must end ENUMERABLE and repairable",
			final.Status, final.Reason, ReasonContentExcluded)
	}
	if len(final.Vector) == 0 {
		t.Fatal("the qualified success must still store its vector")
	}
}

// TestSavePendingGuarded_SkipsOnTerminalQualityNotOnPresence pins all four quality
// combinations of the guard, because the BLOCKING defect was one row of this table
// (existing `generated + ""` vs an incoming qualified write) being wrong.
func TestSavePendingGuarded_SkipsOnTerminalQualityNotOnPresence(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cases := []struct {
		name          string
		storedReason  string
		incomingWrite string
		wantSaved     bool
		why           string
	}{
		{
			name: "complete stands against a complete re-queue", storedReason: "", incomingWrite: "",
			wantSaved: false,
			why:       "#722 B2: a stale re-drive must not downgrade a complete vector to pending",
		},
		{
			name:         "qualified stands against an identical qualified re-queue",
			storedReason: ReasonContentExcluded, incomingWrite: ReasonContentExcluded,
			wantSaved: false,
			why:       "nothing would change; re-embedding on every delivery is pure cost",
		},
		{
			name:         "qualified YIELDS to a complete re-queue (the body is reachable now)",
			storedReason: ReasonContentExcluded, incomingWrite: "",
			wantSaved: true,
			why:       "this is the self-heal: hop 1's offloaded lane leaves Reason unset",
		},
		{
			name:         "complete-LOOKING yields to a qualified re-queue (the laundered record)",
			storedReason: "", incomingWrite: ReasonContentExcluded,
			wantSaved: true,
			why:       "BLOCKING: a hop-2 failure launders the qualifier away; this row un-freezes it",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			const entityID = "acme.ops.docs.sys.doc.quality"
			s := NewStorage(newMemKV(), newMemKV())
			if err := s.SavePending(ctx, entityID, "", "text", 5); err != nil {
				t.Fatalf("SavePending: %v", err)
			}
			if err := s.SaveGenerated(ctx, entityID, []float32{1}, "m", 1, "h", 5, tc.storedReason); err != nil {
				t.Fatalf("SaveGenerated: %v", err)
			}

			saved, err := s.SavePendingGuarded(ctx, &Record{
				EntityID: entityID, SourceText: "fresh", SourceRevision: 5, Reason: tc.incomingWrite,
			})
			if err != nil {
				t.Fatalf("SavePendingGuarded: %v", err)
			}
			if saved != tc.wantSaved {
				t.Fatalf("saved = %v, want %v — %s", saved, tc.wantSaved, tc.why)
			}
		})
	}
}

// TestSavePendingGuarded_StrictlyNewerVectorStandsWhateverItsQuality closes the hole a
// quality-only comparison would open (review MEDIUM 1): at a STRICTLY NEWER stored
// revision the incoming write is stale, and a quality mismatch must not be allowed to
// overrule that. Downgrading there is worse than the transient dip the equal-revision
// rows accept — the fresh record becomes PENDING at an OLDER revision, and hop 2's own
// ordering guard cannot recover it because it compares against the pending record this
// write just left behind. #722 B2 covers the same-quality case; this covers the rest.
func TestSavePendingGuarded_StrictlyNewerVectorStandsWhateverItsQuality(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cases := []struct {
		name         string
		storedReason string
		incoming     string
	}{
		{"newer complete vs older qualified re-queue", "", ReasonContentExcluded},
		{"newer qualified vs older complete re-queue", ReasonContentExcluded, ""},
		{"newer complete vs older complete re-queue", "", ""},
		{"newer qualified vs older qualified re-queue", ReasonContentExcluded, ReasonContentExcluded},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			const entityID = "acme.ops.docs.sys.doc.ordering"
			s := NewStorage(newMemKV(), newMemKV())
			if err := s.SavePending(ctx, entityID, "", "text", 6); err != nil {
				t.Fatalf("SavePending: %v", err)
			}
			if err := s.SaveGenerated(ctx, entityID, []float32{1}, "m", 1, "h", 6, tc.storedReason); err != nil {
				t.Fatalf("SaveGenerated(rev 6): %v", err)
			}

			// A stale re-drive at revision 5 against a generated vector at 6.
			saved, err := s.SavePendingGuarded(ctx, &Record{
				EntityID: entityID, SourceText: "stale", SourceRevision: 5, Reason: tc.incoming,
			})
			if err != nil {
				t.Fatalf("SavePendingGuarded: %v", err)
			}
			if saved {
				t.Fatal("a STRICTLY NEWER generated vector was downgraded to pending at an older " +
					"revision: durable content regression, and hop 2's ordering guard cannot recover it")
			}
			rec, err := s.GetEmbedding(ctx, entityID)
			if err != nil || rec == nil {
				t.Fatalf("GetEmbedding: rec=%v err=%v", rec, err)
			}
			if rec.Status != StatusGenerated || rec.SourceRevision != 6 {
				t.Fatalf("record = %q at rev %d, want the rev-6 generated vector intact",
					rec.Status, rec.SourceRevision)
			}
		})
	}
}

// TestSaveGenerated_RejectsAnUnboundedQualifier locks the bound the field doc and the
// capability spec both assert. The qualifier is a CONTROL input (SavePendingGuarded's
// skip compares it), so an arbitrary persisted value is not a cosmetic label: it would
// match nothing this build writes. Rejecting at the single write site fails closed.
func TestSaveGenerated_RejectsAnUnboundedQualifier(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const entityID = "acme.ops.docs.sys.doc.bounded"

	s := NewStorage(newMemKV(), newMemKV())
	if err := s.SavePending(ctx, entityID, "", "text", 1); err != nil {
		t.Fatalf("SavePending: %v", err)
	}

	if err := s.SaveGenerated(ctx, entityID, []float32{1}, "m", 1, "h", 1, "body_truncated"); err == nil {
		t.Fatal("SaveGenerated accepted an out-of-enum qualifier; the bounded set must be enforced, not documented")
	}
	rec, err := s.GetEmbedding(ctx, entityID)
	if err != nil || rec == nil {
		t.Fatalf("GetEmbedding: rec=%v err=%v", rec, err)
	}
	if rec.Status != StatusPending {
		t.Fatalf("status = %q, want pending: a rejected write must not have persisted", rec.Status)
	}

	// Both members of the set are accepted.
	for _, q := range []string{"", ReasonContentExcluded} {
		if err := s.SaveGenerated(ctx, entityID, []float32{1}, "m", 1, "h", 1, q); err != nil {
			t.Fatalf("SaveGenerated(qualifier=%q): %v", q, err)
		}
	}
}
