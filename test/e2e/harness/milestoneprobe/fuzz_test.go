package milestoneprobe

import (
	"testing"
	"time"
)

// Attempt.Validate and Effect.Validate are exported surfaces that decide
// whether externally-supplied evidence may be believed, so they carry fuzz
// targets with seed corpora covering each class they accept and each they must
// reject (developer contract, § Test and operational fidelity).
//
// Two invariants, deliberately of different kinds. The first is one-way and
// cannot pass vacuously: evidence carrying a different source identity — or an
// empty expected identity — is NEVER accepted. That is the property the probe's
// whole correlation rests on, since every replacement process re-reads the same
// subject and must not count another terminal's record as its own. The second
// is the accept side, so the validator cannot drift closed and silently reject
// the evidence the tier depends on. Neither reruns the implementation's switch:
// the completeness predicate below is the requirement's field list, written
// once here.

// attemptTime and effectTime keep the zero-time branch reachable from a fuzzed
// integer: time.Unix(0, 0) is 1970, which is NOT the zero Time that Validate
// rejects.
func fuzzTime(unix int64) time.Time {
	if unix == 0 {
		return time.Time{}
	}
	return time.Unix(unix, 0).UTC()
}

func FuzzAttemptValidate(f *testing.F) {
	// Accepting seeds.
	f.Add("msg-1", "msg-1", "loop-1", BehaviorExitBeforeAck, "process-a", 42, int64(100))
	f.Add("id.with.subject.metacharacters.>", "id.with.subject.metacharacters.>", "loop-9",
		BehaviorPanicOnce, "process-b", 1, int64(1))
	// Rejecting seeds, one per clause the requirement names.
	f.Add("msg-1", "other", "loop-1", BehaviorExitBeforeAck, "process-a", 42, int64(100)) // foreign identity
	f.Add("", "msg-1", "loop-1", BehaviorExitBeforeAck, "process-a", 42, int64(100))      // empty expectation
	f.Add("msg-1", "msg-1", "", BehaviorExitBeforeAck, "process-a", 42, int64(100))       // no loop
	f.Add("msg-1", "msg-1", "loop-1", "", "process-a", 42, int64(100))                    // no behavior
	f.Add("msg-1", "msg-1", "loop-1", BehaviorExitBeforeAck, "", 42, int64(100))          // no process instance
	f.Add("msg-1", "msg-1", "loop-1", BehaviorExitBeforeAck, "process-a", 0, int64(100))  // non-positive pid
	f.Add("msg-1", "msg-1", "loop-1", BehaviorExitBeforeAck, "process-a", -7, int64(100)) // negative pid
	f.Add("msg-1", "msg-1", "loop-1", BehaviorExitBeforeAck, "process-a", 42, int64(0))   // zero time

	f.Fuzz(func(t *testing.T, expected, sourceID, loopID, behavior, processInstance string, processID int, observedUnix int64) {
		observedAt := fuzzTime(observedUnix)
		attempt := Attempt{
			SourceMessageID: sourceID,
			LoopID:          loopID,
			Behavior:        behavior,
			ProcessInstance: processInstance,
			ProcessID:       processID,
			ObservedAt:      observedAt,
		}

		err := attempt.Validate(expected)

		if expected == "" || sourceID != expected {
			if err == nil {
				t.Fatalf("Validate(%q) accepted evidence whose source id is %q", expected, sourceID)
			}
			return
		}

		complete := loopID != "" && behavior != "" && processInstance != "" && processID > 0 && !observedAt.IsZero()
		if complete != (err == nil) {
			t.Fatalf("Validate(%q) error = %v, want accepted = %v for %#v", expected, err, complete, attempt)
		}
	})
}

func FuzzEffectValidate(f *testing.F) {
	// Accepting seeds.
	f.Add("msg-1", "msg-1", "loop-1", "process-a", int64(100))
	f.Add("id.with.subject.metacharacters.>", "id.with.subject.metacharacters.>", "loop-9", "process-b", int64(1))
	// Rejecting seeds, one per clause the requirement names.
	f.Add("msg-1", "other", "loop-1", "process-a", int64(100)) // foreign identity
	f.Add("", "msg-1", "loop-1", "process-a", int64(100))      // empty expectation
	f.Add("msg-1", "msg-1", "", "process-a", int64(100))       // no loop
	f.Add("msg-1", "msg-1", "loop-1", "", int64(100))          // no process instance
	f.Add("msg-1", "msg-1", "loop-1", "process-a", int64(0))   // zero time

	f.Fuzz(func(t *testing.T, expected, sourceID, loopID, processInstance string, committedUnix int64) {
		committedAt := fuzzTime(committedUnix)
		effect := Effect{
			SourceMessageID: sourceID,
			LoopID:          loopID,
			ProcessInstance: processInstance,
			CommittedAt:     committedAt,
		}

		err := effect.Validate(expected)

		if expected == "" || sourceID != expected {
			if err == nil {
				t.Fatalf("Validate(%q) accepted evidence whose source id is %q", expected, sourceID)
			}
			return
		}

		complete := loopID != "" && processInstance != "" && !committedAt.IsZero()
		if complete != (err == nil) {
			t.Fatalf("Validate(%q) error = %v, want accepted = %v for %#v", expected, err, complete, effect)
		}
	})
}
