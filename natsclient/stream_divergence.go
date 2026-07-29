// stream_divergence.go REPORTS what a get-or-create seam silently discarded.
//
// `EnsureStream` binding an existing stream returns it and drops the caller's
// configuration on the floor. That is the correct thing to DO — a caller that is
// not the stream's owner silently restamping another owner's limits is worse than
// the drift it would be correcting — but doing it in silence has a cost the
// stream-provisioning capability names directly: a stream two components declare
// has its limits decided permanently by BOOT ORDER, with no diagnostic on either
// side. Whoever started first wins, forever, and nothing anywhere says so.
//
// So the declaration is compared and the difference is reported. Report only:
// nothing here writes to a stream, fails a call, or changes what the caller gets
// back.
//
// # Only DECLARED fields are compared
//
// A zero field is silence, not a declaration of zero. A caller that omits MaxAge
// is not asking for unlimited retention, so comparing its zero against a live
// 24-hour window would report a divergence the caller never expressed — and would
// fire on almost every bind, which is how a real signal gets tuned out.
//
// This is also the limit of what the comparison can see, and it is the same limit
// the creation check has: `jetstream.StreamConfig`'s enum fields have MEANINGFUL
// zero values. `DiscardOld`, `FileStorage` and `LimitsPolicy` are all zero, so a
// caller who chose one deliberately is indistinguishable from one who said
// nothing, and a divergence in that direction cannot be reported. Declaring the
// non-default value IS visible. The asymmetry is worth stating plainly rather than
// leaving a reader to assume the report is exhaustive.

package natsclient

import (
	"fmt"
	"slices"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// StreamFieldDivergence is one field a caller declared that the live stream does
// not match.
//
// The values are rendered strings rather than typed values because the whole
// point is to be read by a person: a duration, a byte count and a subject list
// have nothing in common except that an operator needs to see both sides.
type StreamFieldDivergence struct {
	// Field is the jetstream.StreamConfig field name, so it can be found in code.
	Field string
	// Declared is what the caller asked for.
	Declared string
	// Observed is what the live stream actually carries.
	Observed string
}

// String renders one divergence for a diagnostic.
func (d StreamFieldDivergence) String() string {
	return fmt.Sprintf("%s: declared=%s observed=%s", d.Field, d.Declared, d.Observed)
}

// DiffDeclaredStream reports every field the caller DECLARED whose live value
// differs.
//
// It is exported because a caller may need to do more than have it logged.
// gated-dag, for instance, treats a subject divergence as fatal — a dispatch
// stream that does not capture its subject makes every publish fail — and that is
// a decision only the caller can make. The framework reports; the owner decides.
func DiffDeclaredStream(declared, observed jetstream.StreamConfig) []StreamFieldDivergence {
	var out []StreamFieldDivergence
	add := func(field, want, got string) {
		out = append(out, StreamFieldDivergence{Field: field, Declared: want, Observed: got})
	}

	// Subjects first: a stream that does not capture the caller's subject fails
	// every publish with "no stream matches subject", which is the divergence with
	// the loudest downstream consequence. Compared as a SET, because subject order
	// is not meaningful to JetStream and reporting a reordering as drift would be
	// noise.
	if len(declared.Subjects) > 0 && !sameSubjectSet(declared.Subjects, observed.Subjects) {
		add("Subjects", fmt.Sprint(declared.Subjects), fmt.Sprint(observed.Subjects))
	}

	if declared.MaxAge > 0 && declared.MaxAge != observed.MaxAge {
		add("MaxAge", declared.MaxAge.String(), unlimitedOr(observed.MaxAge))
	}
	if declared.MaxBytes > 0 && declared.MaxBytes != observed.MaxBytes {
		add("MaxBytes", fmt.Sprint(declared.MaxBytes), unlimitedOrBytes(observed.MaxBytes))
	}
	if declared.Duplicates > 0 && declared.Duplicates != observed.Duplicates {
		add("Duplicates", declared.Duplicates.String(), observed.Duplicates.String())
	}
	if declared.MaxMsgs > 0 && declared.MaxMsgs != observed.MaxMsgs {
		add("MaxMsgs", fmt.Sprint(declared.MaxMsgs), unlimitedOrBytes(observed.MaxMsgs))
	}
	if declared.MaxMsgsPerSubject > 0 && declared.MaxMsgsPerSubject != observed.MaxMsgsPerSubject {
		add("MaxMsgsPerSubject", fmt.Sprint(declared.MaxMsgsPerSubject), unlimitedOrBytes(observed.MaxMsgsPerSubject))
	}
	if declared.MaxMsgSize > 0 && declared.MaxMsgSize != observed.MaxMsgSize {
		add("MaxMsgSize", fmt.Sprint(declared.MaxMsgSize), fmt.Sprint(observed.MaxMsgSize))
	}
	if declared.Replicas > 0 && declared.Replicas != observed.Replicas {
		add("Replicas", fmt.Sprint(declared.Replicas), fmt.Sprint(observed.Replicas))
	}

	// The enum fields, reportable in ONE direction only. Declaring the non-zero
	// value is visible; declaring the zero value is indistinguishable from saying
	// nothing, so a live DiscardNew against a caller who wanted DiscardOld cannot
	// be reported here. See the file comment.
	if declared.Storage != jetstream.FileStorage && declared.Storage != observed.Storage {
		add("Storage", storageName(declared.Storage), storageName(observed.Storage))
	}
	if declared.Retention != jetstream.LimitsPolicy && declared.Retention != observed.Retention {
		add("Retention", retentionName(declared.Retention), retentionName(observed.Retention))
	}
	if declared.Discard != jetstream.DiscardOld && declared.Discard != observed.Discard {
		add("Discard", discardName(declared.Discard), discardName(observed.Discard))
	}

	return out
}

// DivergenceLabels renders divergences for a log attribute.
func DivergenceLabels(divergences []StreamFieldDivergence) []string {
	labels := make([]string, 0, len(divergences))
	for _, d := range divergences {
		labels = append(labels, d.String())
	}
	return labels
}

// sameSubjectSet compares subject lists ignoring order and duplicates.
func sameSubjectSet(a, b []string) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	left := slices.Clone(a)
	right := slices.Clone(b)
	slices.Sort(left)
	slices.Sort(right)
	return slices.Equal(slices.Compact(left), slices.Compact(right))
}

// unlimitedOr renders an observed duration, naming the zero for what the server
// means by it. "0s" invites a reader to see a bound of zero rather than none.
func unlimitedOr(d time.Duration) string {
	if d <= 0 {
		return "unlimited"
	}
	return d.String()
}

// unlimitedOrBytes renders an observed count, naming both of JetStream's
// unlimited encodings. -1 is what a server reports for a field nobody set.
func unlimitedOrBytes(v int64) string {
	if v <= 0 {
		return fmt.Sprintf("unlimited (%d)", v)
	}
	return fmt.Sprint(v)
}

func storageName(s jetstream.StorageType) string {
	switch s {
	case jetstream.FileStorage:
		return "file"
	case jetstream.MemoryStorage:
		return "memory"
	default:
		return fmt.Sprintf("unknown(%d)", s)
	}
}

func retentionName(r jetstream.RetentionPolicy) string {
	switch r {
	case jetstream.LimitsPolicy:
		return "limits"
	case jetstream.InterestPolicy:
		return "interest"
	case jetstream.WorkQueuePolicy:
		return "workqueue"
	default:
		return fmt.Sprintf("unknown(%d)", r)
	}
}

func discardName(d jetstream.DiscardPolicy) string {
	switch d {
	case jetstream.DiscardOld:
		return "old"
	case jetstream.DiscardNew:
		return "new"
	default:
		return fmt.Sprintf("unknown(%d)", d)
	}
}
