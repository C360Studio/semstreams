package graphview

import (
	"fmt"
	"time"
)

// DeltaOp is the kind of a Delta: upsert, delete (tombstone), or poison.
type DeltaOp uint8

// Delta operation kinds. Tombstones and poison signals ride the same ordered,
// coalesced lane as upserts — last-writer-wins by revision (G4).
const (
	// DeltaUpsert carries the newest decoded value for a key.
	DeltaUpsert DeltaOp = iota
	// DeltaDelete is a tombstone: the key converged to absence.
	DeltaDelete
	// DeltaPoison is a typed per-key poison signal; Delta.Err holds the
	// *PoisonError.
	DeltaPoison
)

// String implements fmt.Stringer for readable test failures and logs.
func (op DeltaOp) String() string {
	switch op {
	case DeltaUpsert:
		return "upsert"
	case DeltaDelete:
		return "delete"
	case DeltaPoison:
		return "poison"
	default:
		return fmt.Sprintf("DeltaOp(%d)", uint8(op))
	}
}

// Delta is one coalesced per-key operation delivered to subscribers: at most
// one per changed key per tick window, carrying the greatest-revision
// operation observed in that window. Values are shared across subscribers
// (for pointer-shaped T, do not mutate delivered values).
type Delta[T any] struct {
	// Op is the operation kind.
	Op DeltaOp
	// Key is the KV key.
	Key string
	// Value is the decoded value; set only for DeltaUpsert.
	Value T
	// Revision is the KV revision of the operation.
	Revision uint64
	// Created is the KV server timestamp of the operation (entry.Created()),
	// carried for every op including tombstones — deletes have no decoded T,
	// so the delta lane is their only channel for the write time.
	Created time.Time
	// Err is the *PoisonError; set only for DeltaPoison.
	Err error
}

// Entry is a projection value in a Snapshot.
type Entry[T any] struct {
	// Value is the decoded value (shared, not copied per subscriber).
	Value T
	// Revision is the KV revision that produced the value.
	Revision uint64
}

// KeyedEntry is a projection value returned by List.
type KeyedEntry[T any] struct {
	// Key is the KV key.
	Key string
	// Value is the decoded value.
	Value T
	// Revision is the KV revision that produced the value.
	Revision uint64
}

// Snapshot is the consistent-at-S projection copy returned by
// SnapshotAndSubscribe: every key applied at sequence <= Sequence is present
// (or absent if deleted), and the paired subscription delivers exactly the
// changes after Sequence — no gap, no duplicate, no inversion (G1).
type Snapshot[T any] struct {
	// Entries maps key to its current decoded value.
	Entries map[string]Entry[T]
	// Poisoned maps key to its sticky poison signal (G6) so consumers that
	// attach after a poison event can still drive their latches.
	Poisoned map[string]*PoisonError
	// Sequence is the view apply-sequence S the snapshot was captured at.
	Sequence uint64
	// Revision is the applied-revision watermark at capture.
	Revision uint64
}
