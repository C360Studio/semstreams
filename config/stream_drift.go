package config

import (
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// ErrStreamNonEditableDrift is returned when an existing ordinary stream's live
// configuration diverges from its declaration in a field the provisioner will
// not change on a live stream. Readiness fails rather than accepting the
// divergence in silence, because a stream that reports one storage tier or
// retention policy while its declaration states another is a configuration two
// people can read opposite answers from.
var ErrStreamNonEditableDrift = errors.New("existing stream diverges in a field that cannot be reconciled in place")

// nonEditableDriftRemedy tells the operator exactly what the server will and
// will not do, because the two halves have genuinely different fixes and a
// diagnostic that blurs them sends someone to delete a stream they did not have
// to delete.
//
// Verified against the pinned server, nats-server v2.12.4
// `server/stream.go:2103 (*jsAccount).configUpdateCheck`:
//
//   - storage type: refused outright ("stream configuration update can not
//     change storage type").
//   - retention policy: refused only when the change is TO or FROM workqueue;
//     limits <-> interest is permitted by the server. The framework still refuses
//     to apply it — see nonEditableDrift.
const nonEditableDriftRemedy = `the server refuses a storage-type change on a live stream outright, and ` +
	`refuses a retention change to or from "workqueue"; a "limits" <-> "interest" change it would accept, but ` +
	`the framework will not make it for you, because switching to "interest" starts deleting acknowledged ` +
	`messages and that is an eviction decision an operator takes deliberately, not one a boot-time reconciler ` +
	`takes on their behalf. Fix by restoring the declaration to the observed value, by applying the retention ` +
	`change yourself (` + "`nats stream edit`" + `), or by draining and recreating the stream — recreating ` +
	`DESTROYS its messages, which is why the provisioner will not do it.`

// streamFieldDrift is one field whose live value diverges from the declaration.
// Observed and declared are rendered as the operator-facing DECLARATION
// spellings ("memory", "interest", "old") rather than the nats.go String()
// forms ("Memory", "Interest", "DiscardOld"), because the remedy for every one
// of these is to write a value into a declaration.
type streamFieldDrift struct {
	field    string
	observed string
	declared string

	// narrowed marks a repair that TIGHTENS a bound, which NATS applies by
	// evicting immediately: messages outside the new bound are deleted when the
	// update lands. Recorded at the point of comparison, where the values are still
	// numbers, so the caller can raise the log level rather than announcing a
	// data-deleting repair at the same level as a harmless widening.
	narrowed bool
}

func (d streamFieldDrift) String() string {
	return fmt.Sprintf("%s: observed=%s declared=%s", d.field, d.observed, d.declared)
}

// narrowingDrifts returns the subset of repairs that tighten a bound, and so
// delete data when applied.
func narrowingDrifts(drifts []streamFieldDrift) []streamFieldDrift {
	var out []streamFieldDrift
	for _, d := range drifts {
		if d.narrowed {
			out = append(out, d)
		}
	}
	return out
}

// driftLabels renders a drift set for a log attribute or an error message.
func driftLabels(drifts []streamFieldDrift) []string {
	out := make([]string, 0, len(drifts))
	for _, d := range drifts {
		out = append(out, d.String())
	}
	return out
}

// governedBounds records which of the three retention fields a declaration
// actually GOVERNS. Reconciling a field the declaration does not govern would
// stamp a framework fallback over an operator's or the server's choice, which is
// the same class of harm as ignoring drift — just pointed the other way.
type governedBounds struct {
	maxAge   bool
	maxBytes bool
	discard  bool
}

// boundsGovernedBy resolves governance from the declaration's exemption class.
//
//   - exemptNone: all three are REQUIRED, so all three are governed.
//   - exemptArchival: permanence IS the declaration. An archival stream must
//     carry no age limit, no byte limit, and the honest DiscardNew encoding, so
//     an existing stream that still evicts is exactly the drift to repair — and
//     the repair only ever REMOVES a limit, which destroys nothing.
//   - exemptMigrationOverride: a bridge governs only what the operator actually
//     wrote. resolveBounds falls back to DiscardOld for an undeclared policy, and
//     that fallback is a framework default rather than a declaration; stamping it
//     onto the legacy stream would have the bridge change the very behavior it
//     exists to preserve.
func boundsGovernedBy(decl streamDeclaration) governedBounds {
	if decl.exemption == exemptMigrationOverride {
		return governedBounds{
			maxAge:   strings.TrimSpace(decl.cfg.MaxAge) != "",
			maxBytes: decl.cfg.MaxBytes > 0,
			discard:  declaresDiscard(decl.cfg),
		}
	}
	return governedBounds{maxAge: true, maxBytes: true, discard: true}
}

// declaresDiscard reports whether the declaration names a discard policy the
// framework recognizes. An unrecognized spelling is NOT a declaration: it falls
// back at creation, and reconciling a fallback is restamping.
func declaresDiscard(cfg StreamConfig) bool {
	return cfg.Discard == StreamDiscardOld || cfg.Discard == StreamDiscardNew
}

// declaredStorage returns the storage tier the declaration names, and whether it
// named one at all. An absent or unrecognized value is not a declaration: it
// resolves to FileStorage at creation the way it always has, but it governs
// nothing on an existing stream, so an omitted `storage` cannot fail readiness
// for a stream someone deliberately created in memory.
func declaredStorage(cfg StreamConfig) (jetstream.StorageType, bool) {
	switch strings.TrimSpace(cfg.Storage) {
	case "memory":
		return jetstream.MemoryStorage, true
	case "file":
		return jetstream.FileStorage, true
	}
	return jetstream.FileStorage, false
}

// declaredRetention returns the retention policy the declaration names, and
// whether it named one. Same reasoning as declaredStorage: an omitted
// `retention` resolves to LimitsPolicy at creation and governs nothing after.
func declaredRetention(cfg StreamConfig) (jetstream.RetentionPolicy, bool) {
	switch strings.TrimSpace(cfg.Retention) {
	case "limits":
		return jetstream.LimitsPolicy, true
	case "interest":
		return jetstream.InterestPolicy, true
	case "workqueue":
		return jetstream.WorkQueuePolicy, true
	}
	return jetstream.LimitsPolicy, false
}

// nonEditableDrift returns every GOVERNED field whose live value diverges from
// the declaration and that the provisioner will not change on a live stream.
//
// Storage is here because the server refuses it. Retention is here for a
// different reason and the difference is worth stating: the server would accept
// limits <-> interest (it refuses only to/from workqueue, nats-server v2.12.4
// `configUpdateCheck`). The framework declines anyway, because retention is not
// a capacity bound — it is the stream's delivery contract, and moving a live
// stream to InterestPolicy begins deleting messages every bound consumer has
// acked. A reconciler that silently applies an eviction semantics change at boot
// is the same failure this whole capability exists to prevent, one layer up from
// stamping MaxAge on a KV backing stream. Failing readiness puts the human who
// owns the stream in the loop; it costs a boot, and it destroys nothing.
func nonEditableDrift(decl streamDeclaration, observed jetstream.StreamConfig) []streamFieldDrift {
	var drifts []streamFieldDrift
	if want, governed := declaredStorage(decl.cfg); governed && observed.Storage != want {
		drifts = append(drifts, streamFieldDrift{
			field: "storage", observed: storageLabel(observed.Storage), declared: storageLabel(want),
		})
	}
	if want, governed := declaredRetention(decl.cfg); governed && observed.Retention != want {
		drifts = append(drifts, streamFieldDrift{
			field: "retention", observed: retentionLabel(observed.Retention), declared: retentionLabel(want),
		})
	}
	return drifts
}

// reconcileStreamConfig returns the configuration to send to UpdateStream and
// the drift that justifies sending it. An empty drift set means the live stream
// already matches its declaration and no update is issued.
//
// The update config is built by OVERLAYING the governed fields onto the OBSERVED
// configuration rather than by sending the declaration. UpdateStream replaces a
// stream's whole configuration, so sending a freshly built declaration would
// reset every field the declaration does not carry — MaxMsgsPerSubject,
// MaxMsgSize, Description, AllowDirect, operator metadata, the duplicate window,
// replicas — as a side effect of repairing MaxAge. Overlaying inverts the
// default: a field is preserved unless the declaration governs it.
//
// declared is the configuration buildStreamConfig produced for this declaration.
// logger may be nil.
func reconcileStreamConfig(
	decl streamDeclaration,
	observed, declared jetstream.StreamConfig,
	logger *slog.Logger,
) (jetstream.StreamConfig, []streamFieldDrift) {
	update := observed
	var drifts []streamFieldDrift

	// Subjects have always been governed by the declaration: a stream's capture
	// set is what the framework routes to it.
	if !subjectsEqual(observed.Subjects, declared.Subjects) {
		drifts = append(drifts, streamFieldDrift{
			field:    "subjects",
			observed: strings.Join(observed.Subjects, ","),
			declared: strings.Join(declared.Subjects, ","),
		})
		update.Subjects = declared.Subjects
	}

	gov := boundsGovernedBy(decl)
	if gov.maxAge && observed.MaxAge != declared.MaxAge {
		drifts = append(drifts, streamFieldDrift{
			field: "max_age", observed: maxAgeLabel(observed.MaxAge), declared: maxAgeLabel(declared.MaxAge),
			// A finite declaration below a wider observed window (or below an
			// unlimited one, which observes as 0) evicts on apply.
			narrowed: declared.MaxAge > 0 && (observed.MaxAge <= 0 || declared.MaxAge < observed.MaxAge),
		})
		update.MaxAge = declared.MaxAge
	}
	// NATS reports an absent byte limit as -1 while a declaration spells
	// unlimited as 0. Comparing them raw would report drift on every boot of
	// every archival and bridged stream, and re-stamp it forever.
	if gov.maxBytes && unlimitedAsNegativeOne(observed.MaxBytes) != unlimitedAsNegativeOne(declared.MaxBytes) {
		drifts = append(drifts, streamFieldDrift{
			field:    "max_bytes",
			observed: maxBytesLabel(observed.MaxBytes),
			declared: maxBytesLabel(declared.MaxBytes),
			narrowed: declared.MaxBytes > 0 &&
				(unlimitedAsNegativeOne(observed.MaxBytes) < 0 || declared.MaxBytes < observed.MaxBytes),
		})
		update.MaxBytes = declared.MaxBytes
	}
	if gov.discard && observed.Discard != declared.Discard {
		drifts = append(drifts, streamFieldDrift{
			field: "discard", observed: discardLabel(observed.Discard), declared: discardLabel(declared.Discard),
		})
		update.Discard = declared.Discard
	}

	// The duplicate window is governed only when the declaration carries one, so
	// an unset window keeps whatever the server or the operator assigned rather
	// than being reset to the server default.
	if strings.TrimSpace(decl.cfg.Duplicates) != "" && observed.Duplicates != declared.Duplicates {
		drifts = append(drifts, streamFieldDrift{
			field:    "duplicates",
			observed: observed.Duplicates.String(),
			declared: declared.Duplicates.String(),
		})
		update.Duplicates = declared.Duplicates
	}

	if len(drifts) == 0 {
		return update, nil
	}

	// A repaired MaxAge can land BELOW a preserved duplicate window, and the
	// server rejects (does not clamp) a window larger than MaxAge — so the repair
	// would abort boot. Clamping touches a field the declaration does not govern,
	// which is normally forbidden; it is the lesser harm here, and it is loud.
	update.Duplicates = clampDuplicates(decl.name, update.Duplicates, update.MaxAge, logger)

	return update, drifts
}

// clampDuplicates holds down an explicit duplicate-detection window that exceeds
// MaxAge. The NATS server REJECTS such a window rather than clamping it, so
// clamping client-side degrades a misconfiguration into a warning instead of a
// failed boot. A zero MaxAge means unlimited and constrains no window.
func clampDuplicates(stream string, duplicates, maxAge time.Duration, logger *slog.Logger) time.Duration {
	if maxAge > 0 && duplicates > maxAge {
		logAt(logger, slog.LevelWarn, "duplicates window exceeds max_age; clamping to max_age",
			"stream", stream, "duplicates", duplicates, "max_age", maxAge)
		return maxAge
	}
	return duplicates
}

// unlimitedAsNegativeOne normalizes the two spellings of "no byte limit" — a
// declaration's 0 and the server's -1 — onto one value.
func unlimitedAsNegativeOne(maxBytes int64) int64 {
	if maxBytes <= 0 {
		return -1
	}
	return maxBytes
}

func storageLabel(s jetstream.StorageType) string {
	if s == jetstream.MemoryStorage {
		return "memory"
	}
	return "file"
}

func retentionLabel(r jetstream.RetentionPolicy) string {
	switch r {
	case jetstream.InterestPolicy:
		return "interest"
	case jetstream.WorkQueuePolicy:
		return "workqueue"
	case jetstream.LimitsPolicy:
		return "limits"
	}
	return "limits"
}

func discardLabel(d jetstream.DiscardPolicy) string {
	if d == jetstream.DiscardNew {
		return StreamDiscardNew
	}
	return StreamDiscardOld
}

func maxAgeLabel(d time.Duration) string {
	if d <= 0 {
		return "unlimited"
	}
	return d.String()
}

func maxBytesLabel(b int64) string {
	if b <= 0 {
		return "unlimited"
	}
	return strconv.FormatInt(b, 10)
}
