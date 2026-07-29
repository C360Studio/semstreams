// stream_bounds.go carries the ORDINARY-STREAM BOUNDS REQUIREMENT to the
// programmatic stream-creation seam (stream-provisioning).
//
// The requirement itself is declared by the configuration path: an ordinary
// stream states a finite MaxAge, a finite MaxBytes, and a discard policy, or it
// is admitted by an archival declaration or an expiring migration override. This
// file exists because a requirement enforced at only one of two doors is not a
// requirement — a direct `Client.EnsureStream` caller would be the supported
// route around it, and one of them was in this repository (COMPONENT_CAPABILITIES
// was created here with MaxAge one hour, no MaxBytes and no discard policy; the
// live server reported `max_bytes=-1 discard=old`, neither of which anyone chose).
//
// It follows the prefix refusal that already came to this seam, and for the same
// reason: the seam is reachable from operator configuration — processor/gated-dag
// exposes its dispatch stream as config JSON — so a guard that lived only in
// `config` would leave the operator-reachable path open.
//
// # What this seam can and cannot require
//
// It requires the two fields whose absence is OBSERVABLE: a positive MaxAge and
// a positive MaxBytes. JetStream reads 0 and -1 alike as unlimited, so neither is
// a declaration.
//
// It does NOT require the discard policy, and that gap is structural rather than
// an oversight. `jetstream.StreamConfig.Discard` is an int whose zero value IS
// `DiscardOld`, so a caller who chose delete-oldest deliberately and a caller who
// never thought about it produce byte-identical configuration. There is nothing
// here to test. The declarative path CAN require it because its discard field is
// a string, where empty is distinguishable from "old" — which is one of the
// reasons that path is the one an operator should be given. A programmatic caller
// is asked for it by review and by the diagnostic below, not by a check that
// cannot exist.
//
// # Why this gates CREATION only
//
// `EnsureStream` is get-or-create. Gating the whole call would refuse to BIND a
// pre-existing unbounded stream, which is neither this caller's stream nor its
// decision to make — a non-owner silently refusing to read someone else's stream
// is worse than the drift.
//
// So an under-declared caller fails on a fresh deployment and BINDS on an existing
// one. That asymmetry is deliberate, and what makes the second case visible is
// narrower than it first appears: the declared-versus-observed report is silent for
// this caller BY CONSTRUCTION, because it compares only the fields the caller
// declared and an under-declared caller declared none of them. Reporting its
// silence as drift is exactly what that rule exists to prevent.
//
// What covers the bind path is a separate observation about the LIVE stream: it is
// itself unbounded (see reportBindDivergence). That fires on a property of the
// stream rather than on the caller's silence, so it has none of the false-positive
// problem, and it is the migration signal for a stream that predates this
// requirement.

package natsclient

import (
	"errors"
	"fmt"
	"strings"

	"github.com/nats-io/nats.go/jetstream"
)

// ErrStreamBoundsUndeclared is the ONE identity for the bounds requirement,
// whichever seam refuses. `config` shares this value rather than defining its
// own, so a caller — including a sister repo — can test for the requirement
// without knowing whether the declarative path or the programmatic one caught it.
var ErrStreamBoundsUndeclared = errors.New("ordinary stream bounds are not declared")

// CheckStreamBounds refuses to CREATE an ordinary stream that declares no finite
// byte and age bounds.
//
// It is pure and I/O-free, so a caller can run it at configuration-validation
// time rather than discovering the refusal at boot. Backing streams are not its
// business: a KV or ObjectStore backing stream is refused outright by
// CheckOrdinaryStreamName, and its retention contract belongs to graph-retention
// (ADR-068/073), so this returns nil for one rather than demanding bounds that
// would be the wrong requirement entirely.
//
// source names the caller so an operator can find what to edit.
func CheckStreamBounds(cfg jetstream.StreamConfig, source string) error {
	if kind, _ := ClassifyBackingStream(cfg.Name); kind != ResourceOrdinaryStream {
		return nil
	}

	var missing []string
	if cfg.MaxAge <= 0 {
		missing = append(missing, "MaxAge")
	}
	if cfg.MaxBytes <= 0 {
		missing = append(missing, "MaxBytes")
	}
	if len(missing) == 0 {
		return nil
	}

	return fmt.Errorf("%w: stream %q (created by %s) declares no %s\n\n%s",
		ErrStreamBoundsUndeclared, cfg.Name, source, strings.Join(missing, " and "), boundsAtSeamRemedy)
}

// boundsAtSeamRemedy tells a programmatic caller what to set, what the unlimited
// encodings actually mean, and where the escapes live — because the escapes are
// operator DECLARATIONS and are not available at this seam, which an author who
// needs one has to be told rather than left to discover.
var boundsAtSeamRemedy = strings.Join([]string{
	"set both on the jetstream.StreamConfig passed to EnsureStream: MaxAge (a positive duration) and " +
		"MaxBytes (a positive byte count — 0 and -1 both mean unlimited to JetStream and are NOT a " +
		"declaration). Set Discard explicitly too: it cannot be required here, because DiscardOld is the " +
		"zero value of the field, but at a finite MaxBytes it is the difference between evicting the " +
		"oldest messages and refusing the newest (producer-side 503 err_code=10077).",
	"a stream whose contract is PERMANENCE, or one that predates this requirement, cannot be excepted " +
		"at this seam: archival_streams and stream_migration_overrides are operator declarations read by " +
		"the configuration path. Declare the stream in configuration and let the stream provisioner " +
		"create it, rather than reaching for an unbounded EnsureStream call.",
}, "\n")
