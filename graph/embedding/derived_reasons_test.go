package embedding

import "testing"

// TestDerivedReasons_NotInPersistedFailureEnum pins the in-memory-only design of
// the derived-write reasons (#625): they are deliberately NOT members of
// normalizeFailureReason's closed persisted-failure enum, because no stored
// record may ever carry them (they classify process-local stranding, and a
// persisted-but-unreachable enum member is a phantom signal). If a future
// change "completes" the enum with these reasons, this test flags the invariant
// so the persistence question is re-argued rather than drifted into: a record
// carrying one could only exist through a write path that violates the seam's
// no-durable-evidence contract, and the scan boundary must keep collapsing such
// a record to the unknown bucket.
func TestDerivedReasons_NotInPersistedFailureEnum(t *testing.T) {
	t.Parallel()
	for _, reason := range []string{ReasonDeleteFailed, ReasonPendingWriteFailed, ReasonEntityReadFailed} {
		if got := normalizeFailureReason(reason); got != reasonUnknown {
			t.Fatalf("normalizeFailureReason(%q) = %q, want %q: derived-write reasons are in-memory only "+
				"and must never join the persisted failure enum (#625)", reason, got, reasonUnknown)
		}
	}
}
