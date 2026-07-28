package embedding

// Derived-write/read failure reasons (#625). They classify entries in
// graph-embedding's PROCESS-LOCAL current-failed map when a hop-1 derived write
// or authoritative read fails — a failed EMBEDDING_INDEX delete, a failed
// pending write, a failed ENTITY_STATES read — so those failures surface as
// `degraded` immediately and are re-driven by the background repair loop.
//
// IN-MEMORY ONLY, by design. These reasons are NEVER persisted into a stored
// Record: they must not be passed to SaveFailed (a derived-write failure means
// the durable state could not be brought to truth — writing a durable "failed"
// record about it is the same class of write and would race the same fault),
// and they are deliberately NOT members of normalizeFailureReason's closed
// enum — a persisted-but-unreachable enum member is a phantom signal. The
// bounded-reasons contract of the readiness envelope still holds: this is a
// closed const set.
const (
	// ReasonDeleteFailed marks an entity whose derived-record delete failed
	// (tombstone or reconcile-observed absence); the record is still present
	// and queryable until repair converges it.
	ReasonDeleteFailed = "delete_failed"
	// ReasonPendingWriteFailed marks an entity whose hop-1 SavePending /
	// SavePendingWithStorageRef write failed; the revision stays uncompleted
	// (#613 F2) and repair re-queues from authoritative state.
	ReasonPendingWriteFailed = "pending_write_failed"
	// ReasonEntityReadFailed marks an entity whose authoritative ENTITY_STATES
	// read failed transiently during a reconcile; repair re-reads until the
	// source answers.
	ReasonEntityReadFailed = "entity_read_failed"
)
