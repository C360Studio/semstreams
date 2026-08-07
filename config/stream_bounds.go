package config

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
)

// Discard-policy declaration values for StreamConfig.Discard. These are the
// operator-facing spellings of jetstream.DiscardOld / jetstream.DiscardNew; the
// mapping happens once, in buildStreamConfig.
const (
	// StreamDiscardOld evicts the oldest messages once the stream reaches a
	// limit. The write always succeeds; the data loss is silent and at the tail.
	StreamDiscardOld = "old"
	// StreamDiscardNew refuses the write once the stream reaches a limit. Nothing
	// is evicted; producers see the rejection instead. See discardPolicyGuidance.
	StreamDiscardNew = "new"
)

// Declaration-source strings. The framework-constant and operator-map paths
// carry no component attribution, so they report the SOURCE rather than
// inventing an owner they do not know.
const (
	frameworkConstantSource = "framework constant (config/streams.go)"
)

// discardPolicyGuidance is the sentence every declaration diagnostic carries
// when discard policy is at stake. This change's own measurement characterized
// the DiscardNew failure mode against NATS 2.12 — at the ceiling a bucket
// refuses a REPLACEMENT of an existing key, not merely a new one — so handing
// operators the knob without the warning would be careless. Kept as one
// constant so the missing-field error and the declared-DiscardNew warning
// cannot drift apart.
const discardPolicyGuidance = `discard must be "old" or "new": "old" evicts the oldest messages once the ` +
	`stream reaches max_bytes or max_age (the write succeeds, the loss is silent); "new" refuses the write ` +
	`instead, which a producer at the ceiling sees as NATS 503 err_code=10077 "maximum bytes exceeded" on ` +
	`every publish — including a replacement of an existing message — until something is deleted`

// Bounds-contract sentinels. Classifiable so a boot path, an operator tool, or a
// test can tell an undeclared bound from an expired bridge from a malformed
// archival declaration, all of which fail readiness for different reasons and
// have different fixes.
var (
	// ErrStreamBoundsUndeclared is returned when an ordinary stream carries no
	// explicitly declared finite MaxAge, finite MaxBytes, or discard policy and
	// is not admitted by an archival declaration or an active migration override.
	//
	// It is natsclient's value rather than a second one of the same name. The same
	// requirement is enforced at two seams — this declarative path and
	// natsclient.Client.EnsureStream — and a caller testing for "bounds are not
	// declared" should not have to know which door refused it.
	ErrStreamBoundsUndeclared = natsclient.ErrStreamBoundsUndeclared

	// ErrStreamMigrationOverrideInvalid is returned when a migration override is
	// not a valid time-limited bridge — no owner, or no expiry. An open-ended
	// override is rejected at validation so a bridge cannot become permanent.
	ErrStreamMigrationOverrideInvalid = errors.New("stream migration override is not a valid time-limited bridge")

	// ErrStreamMigrationOverrideExpired is returned when an override's expiry has
	// passed. Readiness fails rather than degrading quietly: the bridge was
	// declared with an end date and the end date is the whole point.
	ErrStreamMigrationOverrideExpired = errors.New("stream migration override has expired")

	// ErrArchivalStreamInvalid is returned when an archival declaration omits its
	// owner or its reason, so archival cannot become a silent way to opt out of
	// bounds.
	ErrArchivalStreamInvalid = errors.New("archival stream declaration is incomplete")
)

// StreamMigrationOverride admits ONE existing ordinary stream that predates the
// bounds contract, as a named, time-limited exception. It is a migration bridge
// and nothing else: a stream whose contract is permanence is ARCHIVAL and must
// use that classification, so the two never share an instrument.
//
// Owner and Expires are both required. An override with no expiry is rejected at
// validation, because an override's value comes from being rare and alarming and
// an open-ended one is neither.
type StreamMigrationOverride struct {
	// Owner is the team, component, or person accountable for completing the
	// migration. Required — an exception nobody owns never ends.
	Owner string `json:"owner"`

	// Expires is the date the bridge ends, as "2006-01-02" or a full RFC3339
	// timestamp. A date-only value is INCLUSIVE of that day: "2026-09-30" expires
	// at the end of 2026-09-30 UTC, which is how an operator reads it. Required.
	Expires string `json:"expires"`

	// Reason is optional prose for the next reader. Unlike ArchivalStream.Reason
	// it is not required, because an override's justification is its expiry.
	Reason string `json:"reason,omitempty"`
}

// StreamMigrationOverrides maps ordinary stream name to its migration override.
type StreamMigrationOverrides map[string]StreamMigrationOverride

// ArchivalStream declares ONE ordinary stream whose contract is permanence:
// nothing may ever be evicted from it. It is exempt from the finite-bounds
// requirement by declaration, and readiness reports it as a named PERMANENT
// exception structurally distinct from a time-limited migration override.
//
// Both fields are required. An archive expressible only as a renewed override
// would train an operator to renew without reading, which is exactly what makes
// the genuinely time-limited overrides invisible.
type ArchivalStream struct {
	// Owner is the team or component accountable for this stream's growth.
	// Required — an archival stream's only remaining lever is capacity, and
	// capacity questions need an addressee.
	Owner string `json:"owner"`

	// Reason states why permanence is the contract. Required — without it,
	// "archival" is just an unbounded stream with better vocabulary.
	Reason string `json:"reason"`
}

// ArchivalStreams maps ordinary stream name to its archival declaration.
type ArchivalStreams map[string]ArchivalStream

// StreamMigrationOverrideStatus is one active, time-limited exception as
// readiness reports it.
type StreamMigrationOverrideStatus struct {
	Stream    string        // the admitted ordinary stream
	Owner     string        // who is accountable for finishing the migration
	Reason    string        // optional prose, empty when not declared
	Expires   time.Time     // when readiness starts failing
	Remaining time.Duration // time left, at the moment readiness was evaluated
}

// ArchivalStreamStatus is one permanent exception as readiness reports it.
//
// This is a DIFFERENT TYPE from StreamMigrationOverrideStatus rather than the
// same struct with a flag, and the difference is load-bearing: it has no expiry
// field and no remaining-time field, so no surface can render a permanent
// exception as though it were counting down, and no operator can renew it.
type ArchivalStreamStatus struct {
	Stream string // the permanently unbounded ordinary stream
	Owner  string // who is accountable for its growth
	Reason string // why permanence is the contract
}

// StreamExceptionReport is readiness's account of every ordinary stream admitted
// WITHOUT finite bounds, split by the kind of exception that admitted it.
//
// The split is structural, not cosmetic. A single list with a kind flag would
// let an operator surface render "permanent" and "expires in March" through the
// same widget, and the whole reason archival exists as its own classification is
// that blurring those two is what trains people to renew bridges without reading
// them.
type StreamExceptionReport struct {
	// MigrationOverrides are time-limited bridges. Every entry here is a
	// scheduled future readiness failure. Sorted by stream name.
	MigrationOverrides []StreamMigrationOverrideStatus

	// Archival are permanent, declared exceptions. Sorted by stream name.
	Archival []ArchivalStreamStatus
}

// streamExemption records WHY a stream is admitted without finite bounds, or
// that it is not.
type streamExemption uint8

const (
	// exemptNone: the stream must declare finite bounds and a discard policy.
	exemptNone streamExemption = iota
	// exemptMigrationOverride: admitted by an active, time-limited override.
	exemptMigrationOverride
	// exemptArchival: permanent by declaration.
	exemptArchival
)

// streamDeclaration is one ordinary stream as the provisioner resolved it: the
// declaration itself, where it came from, and whether an exception admits it
// without bounds. Resolution is pure and I/O-free so it can run at config
// validation, before any NATS connection exists.
type streamDeclaration struct {
	name string
	cfg  StreamConfig

	// source names where the declaration came from, for the diagnostic. Where
	// the source records an owning component it names that component and its
	// port; the framework-constant and operator-map paths carry no component
	// attribution, so they name the source instead of a guessed owner.
	source string

	exemption streamExemption
}

// portAttribution is one component/port pair that derived a stream name.
type portAttribution struct {
	component string
	port      string
}

// resolveStreamDeclarations returns every ordinary stream this configuration
// would provision — framework constants, operator-map entries, and port-derived
// streams — in deterministic name order, each carrying its declaration source
// and any bounds exemption.
//
// The backing-stream prefix guard runs here in the same order EnsureStreams
// applied it before this function existed (operator map first, sorted, then
// ports) so a config with several offenders always names the same one.
//
// Pure and I/O-free. logger may be nil.
func resolveStreamDeclarations(cfg *Config, logger *slog.Logger) ([]streamDeclaration, error) {
	type resolved struct {
		cfg      StreamConfig
		source   string
		derived  []portAttribution
		explicit bool // declared by a framework constant or the operator map
	}
	decls := make(map[string]*resolved)

	// 1. Framework constants. These are explicit declarations at a named source
	//    in framework code, not silent defaults: the values are written down,
	//    and readiness reports the source when one of them is incomplete.
	for _, fc := range frameworkStreams() {
		decls[fc.name] = &resolved{cfg: fc.cfg, source: frameworkConstantSource, explicit: true}
	}

	// 2. Operator map. Highest priority; can override a framework constant.
	for _, name := range slices.Sorted(maps.Keys(cfg.Streams)) {
		source := fmt.Sprintf("config.streams[%q]", name)
		if err := checkOrdinaryStream(name, source); err != nil {
			return nil, fmt.Errorf("validate stream declaration %q: %w", name, err)
		}
		if existing, ok := decls[name]; ok {
			existing.cfg = cfg.Streams[name]
			existing.source = source
			existing.explicit = true
			continue
		}
		decls[name] = &resolved{cfg: cfg.Streams[name], source: source, explicit: true}
	}

	// 3. Streams derived from component JetStream output ports. Component names
	//    are sorted so the attribution in a diagnostic does not depend on map
	//    iteration order.
	for _, compName := range slices.Sorted(maps.Keys(cfg.Components)) {
		compCfg := cfg.Components[compName]
		if !compCfg.Enabled {
			continue
		}

		ports, err := extractPortsFromConfig(compCfg.Config)
		if err != nil {
			return nil, fmt.Errorf("decode component %q ports: %w", compName, err)
		}

		for _, definition := range ports.Outputs {
			port, err := definition.Resolve(component.DirectionOutput)
			if err != nil {
				return nil, fmt.Errorf("resolve component %q output port %q: %w", compName, definition.Name, err)
			}
			facts, err := port.Facts()
			if err != nil {
				return nil, fmt.Errorf("inspect component %q output port %q: %w", compName, definition.Name, err)
			}
			if facts.Kind() != component.PortKindJetStream {
				continue
			}
			stream, ok := facts.Stream()
			if !ok {
				return nil, fmt.Errorf("component %q output port %q has no JetStream facts", compName, definition.Name)
			}
			streamName, subjects, err := derivePortStream(stream)
			if err != nil {
				return nil, fmt.Errorf("derive component %q output port %q stream: %w", compName, definition.Name, err)
			}
			streamConfig, err := streamConfigFromFacts(stream, subjects)
			if err != nil {
				return nil, fmt.Errorf("project component %q output port %q stream policy: %w", compName, definition.Name, err)
			}

			source := fmt.Sprintf("component %q port %q", compName, definition.Name)
			if err := checkOrdinaryStream(streamName, source); err != nil {
				return nil, fmt.Errorf("validate stream declaration %q: %w", streamName, err)
			}

			attribution := portAttribution{component: compName, port: definition.Name}
			if existing, ok := decls[streamName]; ok {
				existing.derived = append(existing.derived, attribution)
				continue
			}
			decls[streamName] = &resolved{
				cfg:     streamConfig,
				derived: []portAttribution{attribution},
			}
		}
	}

	out := make([]streamDeclaration, 0, len(decls))
	for _, name := range slices.Sorted(maps.Keys(decls)) {
		r := decls[name]
		out = append(out, streamDeclaration{
			name:   name,
			cfg:    r.cfg,
			source: describeSource(r.source, r.explicit, r.derived),
		})
	}
	return out, nil
}

// describeSource builds the declaration-source string for a diagnostic.
//
// A port-derived stream records an owning component, so the diagnostic names it
// (and every other component publishing to the same stream, ordered by
// component — that is what an operator looks up). A stream declared in the
// operator map or by a framework constant records no component, so the
// diagnostic names the SOURCE: reporting an owner we do not know would be a
// guess, and a guessed owner in a boot failure sends someone to the wrong team.
func describeSource(explicitSource string, explicit bool, derived []portAttribution) string {
	slices.SortFunc(derived, func(a, b portAttribution) int {
		if c := cmp.Compare(a.component, b.component); c != 0 {
			return c
		}
		return cmp.Compare(a.port, b.port)
	})

	parts := make([]string, 0, len(derived)+1)
	if explicit {
		parts = append(parts, explicitSource)
	}
	for _, d := range derived {
		parts = append(parts, fmt.Sprintf("component %q port %q", d.component, d.port))
	}
	return strings.Join(parts, "; ")
}

// derivePortStream returns the stream name and subjects a JetStream output port
// contributes.
//
// An explicit stream_name from the port definition wins over subject-derived
// naming. The canonical port type carries stream_name (e.g. agentic-tools'
// tool.result port declares stream_name: "AGENT" so its publishes land on the
// existing AGENT stream rather than spawning a derived TOOL stream that would
// collide with AGENT's "tool.>" capture). This relies on the shadow struct
// having been retired in favour of component.PortDefinition; a previous shadow
// stripped this field on JSON unmarshal and silently swallowed every tool.result
// publish in semspec (project_open_work_2026_05_08.md, bug class 3).
func derivePortStream(stream component.StreamFacts) (string, []string, error) {
	subjects := stream.Subjects()
	if len(subjects) == 0 {
		return "", nil, fmt.Errorf("JetStream output requires at least one subject")
	}
	if stream.Name() != "" {
		return stream.Name(), subjects, nil
	}
	name := DeriveStreamName(subjects[0])
	if name == "" {
		return "", nil, fmt.Errorf("cannot derive stream name from subject %q", subjects[0])
	}
	return name, subjects, nil
}

func streamConfigFromFacts(stream component.StreamFacts, subjects []string) (StreamConfig, error) {
	config := StreamConfig{
		Subjects: append([]string(nil), subjects...),
		Storage:  strings.TrimSpace(stream.Storage()),
		Replicas: stream.Replicas(),
	}
	if config.Storage != "" && config.Storage != "file" && config.Storage != "memory" {
		return StreamConfig{}, fmt.Errorf("unknown storage %q", config.Storage)
	}

	switch retention := strings.TrimSpace(stream.RetentionPolicy()); retention {
	case "":
	case "limits", "interest":
		config.Retention = retention
	case "work_queue":
		config.Retention = "workqueue"
	default:
		return StreamConfig{}, fmt.Errorf("unknown retention policy %q", retention)
	}

	days := stream.RetentionDays()
	if days < 0 || int64(days) > math.MaxInt64/int64(24*time.Hour) {
		return StreamConfig{}, fmt.Errorf("retention_days %d is outside the supported duration range", days)
	}
	if days > 0 {
		config.MaxAge = fmt.Sprintf("%dd", days)
	}

	maxSizeGB := stream.MaxSizeGB()
	const bytesPerGiB int64 = 1024 * 1024 * 1024
	if maxSizeGB < 0 || int64(maxSizeGB) > math.MaxInt64/bytesPerGiB {
		return StreamConfig{}, fmt.Errorf("max_size_gb %d is outside the supported byte range", maxSizeGB)
	}
	if maxSizeGB > 0 {
		config.MaxBytes = int64(maxSizeGB) * bytesPerGiB
	}
	if config.Replicas < 0 {
		return StreamConfig{}, fmt.Errorf("replicas %d must not be negative", config.Replicas)
	}
	return config, nil
}

// logAt logs only when a logger was supplied. Config validation runs with none.
func logAt(logger *slog.Logger, level slog.Level, msg string, args ...any) {
	if logger == nil {
		return
	}
	logger.Log(context.Background(), level, msg, args...)
}

// planStreams resolves every ordinary stream this configuration would provision
// and enforces the bounds contract on it, returning the declarations to create
// and readiness's account of every stream admitted without finite bounds.
//
// now is a parameter so override expiry is testable without wall-clock games.
func planStreams(cfg *Config, now time.Time, logger *slog.Logger) ([]streamDeclaration, StreamExceptionReport, error) {
	decls, err := resolveStreamDeclarations(cfg, logger)
	if err != nil {
		return nil, StreamExceptionReport{}, err
	}

	report, exemptions, err := validateExceptionDeclarations(cfg, now)
	if err != nil {
		return nil, StreamExceptionReport{}, err
	}

	var undeclared []string
	for i := range decls {
		decls[i].exemption = exemptions[decls[i].name]
		if miss := missingBounds(decls[i]); len(miss) > 0 {
			undeclared = append(undeclared, fmt.Sprintf("  - stream %q (declared by %s): missing %s",
				decls[i].name, decls[i].source, strings.Join(miss, ", ")))
		}
	}
	if len(undeclared) > 0 {
		return nil, StreamExceptionReport{}, fmt.Errorf("%w:\n%s\n\n%s",
			ErrStreamBoundsUndeclared, strings.Join(undeclared, "\n"), boundsRemedy)
	}

	return decls, report, nil
}

// boundsRemedy tells the operator exactly what to write and what each choice
// costs. It names both escapes, because an operator who cannot see them will
// reach for the nearest plausible bound instead and pick a wrong one.
var boundsRemedy = strings.Join([]string{
	`declare each missing field in config.streams["<STREAM>"]: ` +
		`max_age (a positive duration, e.g. "24h" or "7d"), ` +
		`max_bytes (a positive byte count — 0 and -1 mean unlimited to NATS and are NOT a declaration), and ` +
		discardPolicyGuidance + ".",
	`a stream whose contract is permanence belongs in archival_streams (owner + reason), not in a bound; ` +
		`a stream that predates this requirement can be bridged by a stream_migration_overrides entry ` +
		`naming its owner and an expiry.`,
}, "\n")

// missingBounds returns the declaration fields this stream still owes, in a
// stable order. An exempt stream owes nothing.
func missingBounds(decl streamDeclaration) []string {
	if decl.exemption != exemptNone {
		return nil
	}
	var missing []string
	if d, err := parseDurationWithDays(decl.cfg.MaxAge); decl.cfg.MaxAge == "" || err != nil || d <= 0 {
		missing = append(missing, "max_age")
	}
	if decl.cfg.MaxBytes <= 0 {
		missing = append(missing, "max_bytes")
	}
	if decl.cfg.Discard != StreamDiscardOld && decl.cfg.Discard != StreamDiscardNew {
		missing = append(missing, "discard")
	}
	return missing
}

// validateExceptionDeclarations validates the two exception blocks and builds
// readiness's report of them.
//
// Shape validation runs for EVERY entry, including one naming a stream this
// configuration does not provision: a stale bridge that outlived its stream is
// still a bridge, and letting it sit unvalidated is how "temporary" becomes
// permanent. Expiry is likewise enforced regardless of whether the stream still
// exists — when an override expires, the fix is to delete it.
func validateExceptionDeclarations(cfg *Config, now time.Time) (StreamExceptionReport, map[string]streamExemption, error) {
	exemptions := make(map[string]streamExemption)
	var report StreamExceptionReport

	for _, name := range slices.Sorted(maps.Keys(cfg.ArchivalStreams)) {
		source := fmt.Sprintf("archival_streams[%q]", name)
		if err := checkOrdinaryStream(name, source); err != nil {
			return StreamExceptionReport{}, nil, fmt.Errorf("validate stream declaration %q: %w", name, err)
		}
		a := cfg.ArchivalStreams[name]
		var missing []string
		if strings.TrimSpace(a.Owner) == "" {
			missing = append(missing, "owner")
		}
		if strings.TrimSpace(a.Reason) == "" {
			missing = append(missing, "reason")
		}
		if len(missing) > 0 {
			return StreamExceptionReport{}, nil, fmt.Errorf(
				"%w: %s is missing %s — an archival declaration states WHO owns the stream and WHY permanence "+
					"is its contract, so archival cannot become a silent way to opt out of bounds",
				ErrArchivalStreamInvalid, source, strings.Join(missing, " and "))
		}
		if _, ok := cfg.StreamMigrationOverrides[name]; ok {
			return StreamExceptionReport{}, nil, fmt.Errorf(
				"%w: stream %q is declared BOTH archival and migration-overridden — permanence and a "+
					"time-limited bridge are different contracts and must never share an instrument; "+
					"keep the archival declaration and delete the override",
				ErrArchivalStreamInvalid, name)
		}
		exemptions[name] = exemptArchival
		report.Archival = append(report.Archival, ArchivalStreamStatus{
			Stream: name, Owner: a.Owner, Reason: a.Reason,
		})
	}

	for _, name := range slices.Sorted(maps.Keys(cfg.StreamMigrationOverrides)) {
		source := fmt.Sprintf("stream_migration_overrides[%q]", name)
		if err := checkOrdinaryStream(name, source); err != nil {
			return StreamExceptionReport{}, nil, fmt.Errorf("validate stream declaration %q: %w", name, err)
		}
		o := cfg.StreamMigrationOverrides[name]
		if strings.TrimSpace(o.Owner) == "" {
			return StreamExceptionReport{}, nil, fmt.Errorf(
				"%w: %s declares no owner — an exception nobody owns never ends",
				ErrStreamMigrationOverrideInvalid, source)
		}
		expires, err := parseOverrideExpiry(o.Expires)
		if err != nil {
			return StreamExceptionReport{}, nil, fmt.Errorf(
				"%w: %s: %w — an override is a migration bridge, and a bridge with no end date is just "+
					"an unbounded stream with paperwork; if this stream's contract is permanence, declare it "+
					"in archival_streams instead",
				ErrStreamMigrationOverrideInvalid, source, err)
		}
		if !now.Before(expires) {
			return StreamExceptionReport{}, nil, fmt.Errorf(
				"%w: %s expired at %s (owner %q, stream %q) — declare finite bounds for the stream, or, if its "+
					"contract turned out to be permanence, move it to archival_streams",
				ErrStreamMigrationOverrideExpired, source, expires.Format(time.RFC3339), o.Owner, name)
		}
		exemptions[name] = exemptMigrationOverride
		report.MigrationOverrides = append(report.MigrationOverrides, StreamMigrationOverrideStatus{
			Stream:    name,
			Owner:     o.Owner,
			Reason:    o.Reason,
			Expires:   expires,
			Remaining: expires.Sub(now),
		})
	}

	return report, exemptions, nil
}

// parseOverrideExpiry accepts a date ("2006-01-02") or a full RFC3339 timestamp.
//
// A date-only value is INCLUSIVE of that day — "2026-09-30" expires at the end
// of 2026-09-30 UTC — because that is how an operator writing a deadline reads
// it, and an off-by-one-day expiry on a bridge is a needless 3am surprise.
func parseOverrideExpiry(s string) (time.Time, error) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return time.Time{}, errors.New(`no expiry declared (use "2006-01-02" or an RFC3339 timestamp)`)
	}
	if day, err := time.ParseInLocation(time.DateOnly, trimmed, time.UTC); err == nil {
		return day.AddDate(0, 0, 1), nil
	}
	ts, err := time.Parse(time.RFC3339, trimmed)
	if err != nil {
		return time.Time{}, fmt.Errorf(
			`expiry %q is not a date ("2006-01-02") or an RFC3339 timestamp`, trimmed)
	}
	return ts, nil
}

// ValidateStreamDeclarations resolves every ordinary stream this configuration
// would provision and enforces the bounds contract on it, returning readiness's
// account of every stream admitted without finite bounds.
//
// It is pure and I/O-free, which is what lets Config.Validate run it: without
// that, `semstreams --validate` would print "✓ Configuration is valid" for a
// configuration that hard-fails at boot.
func ValidateStreamDeclarations(cfg *Config) (StreamExceptionReport, error) {
	_, report, err := planStreams(cfg, time.Now(), nil)
	return report, err
}

// logStreamExceptions reports readiness's exceptions at boot.
//
// The two kinds are logged at different levels ON PURPOSE. An override is a
// scheduled future boot failure and should read as one; an archival stream is a
// declared, approved contract. Logging them identically is the first step toward
// an operator treating a countdown like a permanent fixture.
func logStreamExceptions(logger *slog.Logger, report StreamExceptionReport) {
	if logger == nil {
		return
	}
	for _, o := range report.MigrationOverrides {
		logger.Warn("Ordinary stream admitted WITHOUT finite bounds by a time-limited migration override",
			"stream", o.Stream,
			"owner", o.Owner,
			"expires", o.Expires.Format(time.RFC3339),
			"expires_in", o.Remaining.Round(time.Hour).String(),
			"reason", o.Reason,
			"hint", "readiness FAILS once this expiry passes; declare finite bounds before then")
	}
	for _, a := range report.Archival {
		logger.Info("Ordinary stream is permanently unbounded by archival declaration",
			"stream", a.Stream,
			"owner", a.Owner,
			"reason", a.Reason,
			"hint", "permanent by contract, not a countdown; its only ceiling is the account tier limit")
	}
}

// missingOverriddenStreamRemedy is returned when a migration override names a
// stream that does not exist. It states the distinction the override rests on,
// because an operator who reaches for one on a fresh deployment has almost
// certainly mistaken it for a way to declare "unbounded".
var missingOverriddenStreamRemedy = strings.Join([]string{
	`a stream_migration_overrides entry is a time-limited BRIDGE for a stream that already exists and ` +
		`predates the bounds requirement. There is nothing to bridge here.`,
	`If this stream should be permanently unbounded, declare it in archival_streams (owner + reason) — ` +
		`that is the classification for permanence. If it should be bounded, declare max_age, max_bytes ` +
		`and discard on it. If you expected it to exist already, the override is masking that it is gone.`,
}, "\n")

// ExpiredMigrationOverrides returns every migration override whose expiry has
// passed as of now, so a RUNNING instance can report a bridge that ended while it
// was up.
//
// It exists because expiry is otherwise evaluated only at configuration validation
// and at provisioning — both boot-time. An instance that started before the
// deadline would otherwise run indefinitely past it with nothing saying so, and the
// whole value of a bridge is that it ends.
//
// Enforcement stays at boot. This function REPORTS, and that split is deliberate:
// the stream a lapsed override admits is still working, and taking a healthy fleet
// out of service simultaneously because a calendar date passed would be a
// self-inflicted outage over a hygiene failure. The refusal lands at the next boot,
// which is when an operator can act on it anyway.
//
// A malformed override (no expiry, unparseable date) is NOT returned here: it is
// rejected at validation and never reaches a running instance.
func ExpiredMigrationOverrides(cfg *Config, now time.Time) []StreamMigrationOverrideStatus {
	if cfg == nil || len(cfg.StreamMigrationOverrides) == 0 {
		return nil
	}

	var expired []StreamMigrationOverrideStatus
	for _, name := range slices.Sorted(maps.Keys(cfg.StreamMigrationOverrides)) {
		override := cfg.StreamMigrationOverrides[name]
		expires, err := parseOverrideExpiry(override.Expires)
		if err != nil {
			continue
		}
		if now.Before(expires) {
			continue
		}
		expired = append(expired, StreamMigrationOverrideStatus{
			Stream:  name,
			Owner:   override.Owner,
			Reason:  override.Reason,
			Expires: expires,
			// Negative: the bridge ended this long ago. The field is "time left", and
			// a lapsed bridge has less than none — rendering it as zero would make an
			// override that expired an hour ago indistinguishable from one expiring
			// this instant.
			Remaining: expires.Sub(now),
		})
	}
	return expired
}
