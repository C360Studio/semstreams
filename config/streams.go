// Package config provides configuration management for SemStreams.
package config

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
)

// StreamConfig defines configuration for a JetStream stream.
//
// MaxAge, MaxBytes, and Discard are REQUIRED for an ordinary stream and are
// never supplied by a framework default. A bound the operator never chose is
// indistinguishable in the operator surface from one they did, which is the
// condition the bounds contract exists to end. The two ways out are both
// explicit declarations of their own: `archival_streams` for a stream whose
// contract is permanence, and `stream_migration_overrides` for a time-limited
// bridge. See ValidateStreamDeclarations.
type StreamConfig struct {
	Subjects  []string `json:"subjects"`            // Subjects captured by this stream
	Storage   string   `json:"storage,omitempty"`   // "file" or "memory" (default: file)
	MaxAge    string   `json:"max_age,omitempty"`   // Required: message TTL (e.g., "168h", "7d")
	MaxBytes  int64    `json:"max_bytes,omitempty"` // Required: max storage in bytes; must be > 0
	Retention string   `json:"retention,omitempty"` // "limits", "interest", "workqueue" (default: limits)
	Replicas  int      `json:"replicas,omitempty"`  // Replication factor (default: 1)

	// Discard is the operator's choice of what happens when the stream reaches
	// MaxBytes or MaxAge: StreamDiscardOld evicts the oldest messages,
	// StreamDiscardNew refuses the write (producers see NATS 503
	// err_code=10077). Required for an ordinary stream — it was hardcoded to
	// DiscardOld before, so the policy was never the operator's choice.
	Discard string `json:"discard,omitempty"`

	// Duplicates is the server-side duplicate-detection window for the
	// Nats-Msg-Id header (e.g. "2m", "30m", "1h"). Producers using
	// Client.PublishToStreamWithMsgID rely on this window to collapse
	// redeliveries of the same logical event (ADR-055 §5, "T1"). Empty
	// leaves the window unset, so the NATS server applies its default of
	// 2 minutes — adequate for steady-state redelivery but shorter than a
	// restart/recovery replay horizon. Streams whose producers need
	// dedup across that horizon set this explicitly. Must be <= MaxAge:
	// the NATS server REJECTS (does not clamp) an explicit window larger
	// than MaxAge, so createStream clamps it down to MaxAge with a warning
	// rather than letting EnsureStreams abort boot.
	Duplicates string `json:"duplicates,omitempty"`
}

// StreamConfigs is a map of stream name to configuration.
type StreamConfigs map[string]StreamConfig

// DeriveStreamName extracts stream name from subject convention.
// Convention: subject "component.action.type" → stream "COMPONENT"
// Examples:
//
//	"objectstore.stored.entity" → "OBJECTSTORE"
//	"sensor.processed.entity"   → "SENSOR"
//	"rule.triggered.alert"      → "RULE"
func DeriveStreamName(subject string) string {
	// Handle wildcard subjects by extracting the first segment
	subject = strings.TrimPrefix(subject, "*.")
	subject = strings.TrimSuffix(subject, ".>")
	subject = strings.TrimSuffix(subject, ".*")

	parts := strings.Split(subject, ".")
	if len(parts) == 0 || parts[0] == "" || parts[0] == "*" || parts[0] == ">" {
		return ""
	}
	return strings.ToUpper(parts[0])
}

// DeriveStreamSubjects creates wildcard pattern for stream capture.
// Convention: subject "component.action.type" → ["component.>"]
// Examples:
//
//	"objectstore.stored.entity" → ["objectstore.>"]
//	"sensor.processed.entity"   → ["sensor.>"]
func DeriveStreamSubjects(subject string) []string {
	streamName := DeriveStreamName(subject)
	if streamName == "" {
		return nil
	}
	return []string{strings.ToLower(streamName) + ".>"}
}

// StreamsManager handles JetStream stream creation and management.
type StreamsManager struct {
	natsClient *natsclient.Client
	logger     *slog.Logger
}

// NewStreamsManager creates a new StreamsManager.
func NewStreamsManager(natsClient *natsclient.Client, logger *slog.Logger) *StreamsManager {
	return &StreamsManager{
		natsClient: natsClient,
		logger:     logger,
	}
}

// logsStreamConfig defines the configuration for the LOGS stream.
// This stream captures all application logs with automatic expiration.
// Subject pattern: logs.{level}.{source} (e.g., logs.INFO.graph-processor)
var logsStreamConfig = StreamConfig{
	Subjects: []string{"logs.>"},
	Storage:  "file",
	MaxAge:   "1h",              // TTL: expire after 1 hour
	MaxBytes: 100 * 1024 * 1024, // 100MB max storage
	Discard:  StreamDiscardOld,  // Oldest logs are the expendable ones
	Replicas: 1,
}

// healthStreamConfig defines the configuration for the HEALTH stream.
// This stream captures component and service health updates.
// Subject patterns:
//   - health.component.{name} (e.g., health.component.graph-processor)
//   - health.service.{name} (e.g., health.service.flow-service)
var healthStreamConfig = StreamConfig{
	Subjects: []string{"health.>"},
	Storage:  "memory",         // No persistence needed for health
	MaxAge:   "5m",             // Short TTL - only recent health matters
	MaxBytes: 10 * 1024 * 1024, // 10MB max storage
	Discard:  StreamDiscardOld, // Stale health is worthless; never refuse a fresh report
	Replicas: 1,
}

// metricsStreamConfig defines the configuration for the METRICS stream.
// This stream captures prometheus metrics snapshots.
// Subject pattern: metrics.{component}.{metric} (e.g., metrics.graph-processor.messages_processed)
var metricsStreamConfig = StreamConfig{
	Subjects: []string{"metrics.>"},
	Storage:  "memory",         // No persistence needed for metrics
	MaxAge:   "5m",             // Short TTL - only recent metrics matter
	MaxBytes: 50 * 1024 * 1024, // 50MB max storage
	Discard:  StreamDiscardOld, // Stale metrics are worthless; never refuse a fresh sample
	Replicas: 1,
}

// flowsStreamConfig defines the configuration for the FLOWS stream.
// This stream captures flow status changes.
// Subject pattern: flows.{flowId}.status (e.g., flows.abc123.status)
var flowsStreamConfig = StreamConfig{
	Subjects: []string{"flows.>"},
	Storage:  "memory",         // No persistence needed for status
	MaxAge:   "5m",             // Short TTL - only recent status matters
	MaxBytes: 10 * 1024 * 1024, // 10MB max storage
	Discard:  StreamDiscardOld, // Stale status is worthless; never refuse a fresh one
	Replicas: 1,
}

// governanceVerdictAuditStreamConfig defines the GOVERNANCE_VERDICT_AUDIT stream
// (ADR-055 §3a). It is the append-only home for governance deny/approve verdict
// events, replacing the prior rule-ID audit triple. Each verdict is a new stream
// sequence (append-only by construction — no key to overwrite, so last-write-wins
// audit loss is structurally impossible). Subject pattern:
// governance.verdict.{decision}.{rule_token} (decision ∈ deny|approve; rule_token
// is a subject-safe hash of the rule ID, canonical rule_id in the payload).
// File storage + a long MaxAge sized to the audit/compliance horizon — its own
// knob, the reason for a dedicated stream rather than riding AGENT.
// Discard is StreamDiscardOld, and that is a deliberate choice rather than an
// inherited default. Under StreamDiscardNew a full audit stream would reject
// every subsequent governance verdict with a producer-side 503 err_code=10077,
// which fails the rule engine's publish path rather than the audit trail — a
// storage limit taking a governance decision. The 90-day MaxAge is the intended
// retention horizon; the 500MB cap is a backstop against a verdict storm, and
// evicting the oldest verdicts is the correct behavior when it is reached.
var governanceVerdictAuditStreamConfig = StreamConfig{
	Subjects:  []string{"governance.verdict.>"},
	Storage:   "file",            // Durable: an audit trail must survive restarts
	MaxAge:    "2160h",           // ~90 days audit/compliance horizon
	MaxBytes:  500 * 1024 * 1024, // 500MB cap
	Discard:   StreamDiscardOld,  // See the note above — DiscardNew would break the verdict publish path
	Retention: "limits",          // Append-only event log (NOT interest/workqueue)
	Replicas:  1,
}

// frameworkStream pairs a framework-guaranteed stream with its name.
type frameworkStream struct {
	name string
	cfg  StreamConfig
}

// frameworkStreams returns the streams SemStreams guarantees regardless of
// operator configuration, in a deterministic order.
//
// These are explicit declarations at a named source, not silent defaults: each
// value is written down above with its rationale, readiness names
// "framework constant (config/streams.go)" when one is incomplete, and an
// operator can still override any of them through config.streams.
func frameworkStreams() []frameworkStream {
	return []frameworkStream{
		{"LOGS", logsStreamConfig},
		{"HEALTH", healthStreamConfig},
		{"METRICS", metricsStreamConfig},
		{"FLOWS", flowsStreamConfig},
		// GOVERNANCE_VERDICT_AUDIT is a framework guarantee (ADR-055 §3a): the
		// append-only verdict audit must exist wherever the rule engine can issue
		// deny/approve verdicts, independent of operator stream config.
		{"GOVERNANCE_VERDICT_AUDIT", governanceVerdictAuditStreamConfig},
	}
}

// VerifyJetStreamLimits reads the operator's MaxMemory / MaxFileStore
// hints from cfg.NATS.JetStream and logs a Warn for each value that
// exceeds the server's actual account limit. JetStream account limits
// are server-side configuration (nats.conf or jetstream-domain
// configuration); the nats.go SDK exposes AccountInfo as read-only, so
// the framework cannot push the operator's intent — but it CAN surface
// the gap loudly so an operator who set max_file_store: 10GB in the
// framework config but didn't update nats.conf isn't left wondering
// why stream-create fails with "insufficient storage resources" at
// runtime (#101). Zero or unset values skip the check.
//
// Best-effort: a failure to fetch AccountInfo (server down, JetStream
// disabled, AccountInfo unsupported) logs at Debug and returns nil —
// the check is diagnostic, not gating. EnsureStreams runs anyway and
// any actual capacity miss surfaces as a CreateStream error there.
func (sm *StreamsManager) VerifyJetStreamLimits(ctx context.Context, cfg *Config) error {
	configured := cfg.NATS.JetStream
	// Skip when JetStream itself is disabled in the framework config —
	// no AccountInfo to query meaningfully.
	if !configured.Enabled {
		return nil
	}
	// Skip when neither limit is set — nothing to compare against.
	if configured.MaxMemory <= 0 && configured.MaxFileStore <= 0 {
		return nil
	}

	js, err := sm.natsClient.JetStream()
	if err != nil {
		sm.logger.Debug("VerifyJetStreamLimits: JetStream context unavailable; skipping limit verification",
			"error", err)
		return nil
	}

	infoCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	info, err := js.AccountInfo(infoCtx)
	if err != nil {
		sm.logger.Debug("VerifyJetStreamLimits: AccountInfo() failed; skipping limit verification",
			"error", err,
			"hint", "diagnostic-only — EnsureStreams will surface any real capacity miss")
		return nil
	}

	if jetStreamLimitExceeds(configured.MaxMemory, info.Limits.MaxMemory) {
		sm.logger.Warn("nats.jetstream.max_memory exceeds server limit",
			"configured", configured.MaxMemory,
			"server_limit", info.Limits.MaxMemory,
			"hint", "JetStream account limits are server-side config (nats.conf 'jetstream { max_memory_store: N }'). The framework config block is a verification hint, not a control surface — update nats.conf and restart the server, or lower the framework config to match.")
	}
	if jetStreamLimitExceeds(configured.MaxFileStore, info.Limits.MaxStore) {
		sm.logger.Warn("nats.jetstream.max_file_store exceeds server limit",
			"configured", configured.MaxFileStore,
			"server_limit", info.Limits.MaxStore,
			"hint", "JetStream account limits are server-side config (nats.conf 'jetstream { max_file_store: N }'). The framework config block is a verification hint, not a control surface — update nats.conf and restart the server, or lower the framework config to match.")
	}
	return nil
}

// jetStreamLimitExceeds is the pure predicate behind
// VerifyJetStreamLimits's per-field gap check. Returns true when an
// operator-configured limit is set (>0) AND the server reports a
// finite limit (!= -1, the AccountLimits sentinel for "unlimited") AND
// the configured value exceeds the server's. Extracted so the predicate
// is unit-testable without standing up a NATS server (the integration
// test against the testcontainers nats-server only exercises the
// unlimited-server early-return branch, since testcontainers reports
// -1 / -1 by default).
func jetStreamLimitExceeds(configured, serverLimit int64) bool {
	if configured <= 0 {
		return false
	}
	if serverLimit == -1 {
		return false
	}
	return configured > serverLimit
}

// ErrBackingStreamNotProvisionable is the refusal sentinel, re-exported from
// natsclient so config callers and tests need not reach across for it. It is the
// SAME error value, so errors.Is matches a refusal raised at any provisioning
// seam — this provisioner, Client.EnsureStream, or Client.CreateStream.
var ErrBackingStreamNotProvisionable = natsclient.ErrBackingStreamNotProvisionable

// checkOrdinaryStream is the config provisioner's view of the shared fail-closed
// name guard. One definition lives in natsclient.CheckOrdinaryStreamName, which
// every provisioning seam calls, so a bypass cannot open by a guard being added
// in one place and forgotten in another. This wrapper exists only to keep the
// call sites below short and to pin the provisioner's own attribution string.
//
// The hazard this closes at THIS seam specifically: cfg.Streams is an
// operator-authored map with arbitrary keys, and createStream stamps a declared
// MaxAge/MaxBytes/Discard onto whatever it is handed — so one typo reconciles
// reachability-blind age eviction onto authoritative graph state, or onto
// out-of-line content the graph still references. The bounds contract makes that
// worse rather than better: every ordinary stream now MUST carry a finite MaxAge
// and MaxBytes, so a mistyped backing-stream name that got past this guard would
// be required to receive exactly the eviction policy that destroys it.
func checkOrdinaryStream(name, source string) error {
	return natsclient.CheckOrdinaryStreamName(name, source)
}

// EnsureStreams creates all required JetStream streams based on:
// 1. System streams (LOGS for out-of-band logging)
// 2. Explicit streams defined in config.Streams (highest priority)
// 3. Streams derived from component JetStream output ports
//
// Every resolved ordinary stream must carry an explicitly declared finite
// MaxAge, a finite MaxBytes, and a discard policy, unless an archival
// declaration or an active migration override admits it. That check runs BEFORE
// any stream is created, so a configuration missing a bound fails closed with a
// complete diagnostic rather than provisioning half its streams and then
// stopping — a partially-provisioned account is harder to reason about than one
// that never started.
func (sm *StreamsManager) EnsureStreams(ctx context.Context, cfg *Config) error {
	decls, exceptions, err := planStreams(cfg, time.Now(), sm.logger)
	if err != nil {
		return errs.WrapFatal(err, "StreamsManager", "EnsureStreams", "resolve stream declarations")
	}

	logStreamExceptions(sm.logger, exceptions)

	for _, decl := range decls {
		if err := sm.createStream(ctx, decl); err != nil {
			return fmt.Errorf("create stream %s: %w", decl.name, err)
		}
	}

	sm.logger.Info("Ensured JetStream streams",
		"count", len(decls),
		"migration_overrides", len(exceptions.MigrationOverrides),
		"archival", len(exceptions.Archival))
	return nil
}

// PortsConfig and PortDefinition are aliases for the canonical
// component-package types. They previously existed as a parallel shadow
// inside this file because config could not import component (transitive
// cycle through agentic/message). The cycle was broken in 2026-05-08 by
// promoting PlatformConfig to its own pkg/platform leaf package; the
// shadow types are now unnecessary and were a recurring source of
// silent field-strip bugs (StreamName 2026-05-08, and any future field
// added to component.PortDefinition before this fix landed).
//
// Keeping the alias names (PortsConfig, PortDefinition) preserves the
// existing call sites inside this package without churn while making
// the canonical type the single source of truth.
type (
	// PortsConfig is the canonical component port configuration.
	PortsConfig = component.PortConfig
	// PortDefinition is the canonical component port definition.
	PortDefinition = component.PortDefinition
)

// extractPortsFromConfig parses port definitions from raw component config.
// A free function rather than a method because stream declaration resolution is
// pure and I/O-free — it runs at config validation, where no StreamsManager (and
// no NATS connection) exists.
func extractPortsFromConfig(rawConfig json.RawMessage) (*PortsConfig, error) {
	var cfg struct {
		Ports PortsConfig `json:"ports"`
	}
	if err := json.Unmarshal(rawConfig, &cfg); err != nil {
		return nil, err
	}
	return &cfg.Ports, nil
}

// buildStreamConfig turns a resolved declaration into the JetStream config to
// stamp. It is where the bounds contract becomes bytes, and it fails closed
// rather than substituting anything: an ordinary stream reaching here without a
// finite MaxAge, a finite MaxBytes, or a discard policy is a bug in the
// resolution path, and inventing a value would recreate exactly the silent
// default the contract removes.
//
// Pure except for warnings, so the stamped configuration is testable without a
// NATS server. logger may be nil.
func buildStreamConfig(decl streamDeclaration, logger *slog.Logger) (jetstream.StreamConfig, error) {
	cfg := decl.cfg

	if miss := missingBounds(decl); len(miss) > 0 {
		return jetstream.StreamConfig{}, fmt.Errorf(
			"%w: stream %q (declared by %s): missing %s\n\n%s",
			ErrStreamBoundsUndeclared, decl.name, decl.source, strings.Join(miss, ", "), boundsRemedy)
	}

	// Storage and retention resolve through the same helpers the drift check
	// uses, so "what the declaration governs" cannot mean one thing at creation
	// and another at reconciliation. An absent or unrecognized spelling resolves
	// to the historical default here and governs nothing there.
	storage, _ := declaredStorage(cfg)
	retention, _ := declaredRetention(cfg)

	maxAge, maxBytes, discard, err := resolveBounds(decl, logger)
	if err != nil {
		return jetstream.StreamConfig{}, err
	}

	// Parse duplicate-detection window. Empty leaves Duplicates at zero so
	// the NATS server applies its default (2 minutes); a parse failure falls
	// back to that default rather than failing stream creation. See ADR-055
	// §5 "T1".
	var duplicates time.Duration
	if cfg.Duplicates != "" {
		var dupErr error
		duplicates, dupErr = parseDurationWithDays(cfg.Duplicates)
		if dupErr != nil {
			logAt(logger, slog.LevelWarn, "Invalid duplicates window, using server default",
				"stream", decl.name, "duplicates", cfg.Duplicates, "error", dupErr)
			duplicates = 0
		}
	}
	// The NATS server rejects an explicit duplicate window larger than MaxAge
	// (it does not clamp it). Clamp down to MaxAge with a warning so a
	// misconfigured window degrades gracefully instead of aborting boot in
	// EnsureStreams. Only when MaxAge is finite: an exempt stream may carry
	// MaxAge 0, which means unlimited and constrains no window at all.
	duplicates = clampDuplicates(decl.name, duplicates, maxAge, logger)

	// Replicas default
	replicas := cfg.Replicas
	if replicas <= 0 {
		replicas = 1
	}

	return jetstream.StreamConfig{
		Name:       decl.name,
		Subjects:   cfg.Subjects,
		Storage:    storage,
		Retention:  retention,
		MaxAge:     maxAge,
		MaxBytes:   maxBytes,
		Discard:    discard,
		Replicas:   replicas,
		Duplicates: duplicates,
	}, nil
}

// resolveBounds returns the MaxAge, MaxBytes, and discard policy to stamp, per
// the declaration's exemption class.
func resolveBounds(decl streamDeclaration, logger *slog.Logger) (time.Duration, int64, jetstream.DiscardPolicy, error) {
	cfg := decl.cfg

	var maxAge time.Duration
	if cfg.MaxAge != "" {
		parsed, err := parseDurationWithDays(cfg.MaxAge)
		if err != nil {
			return 0, 0, jetstream.DiscardOld, fmt.Errorf(
				"stream %q (declared by %s): max_age %q is not a duration: %w",
				decl.name, decl.source, cfg.MaxAge, err)
		}
		maxAge = parsed
	}

	// MaxBytes 0 is what NATS reads as unlimited. For an ordinary stream that
	// state is unreachable (missingBounds rejected it); for an exempt one it is
	// the intended meaning.
	maxBytes := cfg.MaxBytes
	if maxBytes < 0 {
		maxBytes = 0
	}

	switch decl.exemption {
	case exemptArchival:
		// Permanent by declaration: nothing may ever be evicted, so no limit is
		// stamped. DiscardNew rather than the NATS zero value because it is the
		// honest encoding of the contract — `nats stream info` on an archive
		// should not read "discard: old". It is inert while no limit exists.
		return 0, 0, jetstream.DiscardNew, nil
	case exemptMigrationOverride:
		// A bridge keeps the stream behaving as it did before this contract:
		// whatever the operator declared is honored, the rest stays unlimited,
		// and the discard policy falls back to the pre-contract DiscardOld.
		// Nothing here is silent — readiness names the override, its owner, and
		// its expiry every boot.
		return maxAge, maxBytes, discardPolicy(cfg.Discard, jetstream.DiscardOld), nil
	case exemptNone:
	}

	discard := discardPolicy(cfg.Discard, jetstream.DiscardOld)
	if discard == jetstream.DiscardNew {
		logAt(logger, slog.LevelWarn,
			"Stream declares discard=new: at its ceiling this stream REFUSES writes rather than evicting",
			"stream", decl.name,
			"source", decl.source,
			"max_bytes", maxBytes,
			"max_age", maxAge,
			"hint", discardPolicyGuidance)
	}
	return maxAge, maxBytes, discard, nil
}

// discardPolicy maps the declared spelling to the JetStream policy.
func discardPolicy(declared string, fallback jetstream.DiscardPolicy) jetstream.DiscardPolicy {
	switch declared {
	case StreamDiscardNew:
		return jetstream.DiscardNew
	case StreamDiscardOld:
		return jetstream.DiscardOld
	}
	return fallback
}

// createStream creates or updates a JetStream stream.
func (sm *StreamsManager) createStream(ctx context.Context, decl streamDeclaration) error {
	name := decl.name
	cfg := decl.cfg

	// Fail closed FIRST, before any JetStream access: EnsureStreams validates
	// its declaration set, but createStream is the seam that actually issues
	// CreateStream/UpdateStream, so guarding it too means no future caller path
	// — a new derivation rule, a direct call, a test helper — can reach NATS
	// with a KV or ObjectStore backing-stream name.
	//
	// This statement's POSITION is load-bearing, not incidental. Every
	// reconciliation path below is downstream of it in control flow, which is the
	// only reason widening the reconciler to write MaxAge/MaxBytes/Discard onto
	// an EXISTING stream is safe: an unfiltered reconciler that learned to write
	// those fields is exactly how one operator typo (`KV_ENTITY_STATES` in
	// cfg.Streams) would stamp reachability-blind age eviction onto authoritative
	// graph state. Do not move reconciliation anywhere this guard does not
	// dominate, and do not demote the guard to a check that merely runs "nearby".
	// TestCreateStream_BackingStreamIsRefusedBeforeReconciliation pins it.
	if err := checkOrdinaryStream(name, "stream provisioner"); err != nil {
		return errs.WrapFatal(err, "StreamsManager", "createStream",
			fmt.Sprintf("provision stream %q", name))
	}

	// Same reasoning for the bounds contract: this is the seam that stamps a
	// configuration onto NATS, so an unbounded ordinary stream must not be able
	// to reach it by a route that skipped declaration validation.
	streamCfg, err := buildStreamConfig(decl, sm.logger)
	if err != nil {
		return errs.WrapFatal(err, "StreamsManager", "createStream",
			fmt.Sprintf("provision stream %q", name))
	}

	js, err := sm.natsClient.JetStream()
	if err != nil {
		return fmt.Errorf("get JetStream context: %w", err)
	}

	// The stream already exists: inspect its LIVE configuration rather than
	// treating create-or-open success as proof it matches the declaration.
	existingStream, err := js.Stream(ctx, name)
	if err == nil {
		observed := existingStream.CachedInfo().Config

		// Fail closed before repairing anything. A stream whose storage tier or
		// retention policy diverges cannot be brought into agreement in place, so
		// repairing its capacity fields would leave it half-declared while
		// reporting success — worse than the silent acceptance this replaces.
		if drifts := nonEditableDrift(decl, observed); len(drifts) > 0 {
			return errs.WrapFatal(fmt.Errorf("%w: stream %q (declared by %s): %s\n\n%s",
				ErrStreamNonEditableDrift, name, decl.source,
				strings.Join(driftLabels(drifts), "; "), nonEditableDriftRemedy),
				"StreamsManager", "createStream", fmt.Sprintf("reconcile stream %q", name))
		}

		updateCfg, drifts := reconcileStreamConfig(decl, observed, streamCfg, sm.logger)
		if len(drifts) == 0 {
			sm.logger.Debug("Stream already exists with correct config", "stream", name)
			return nil
		}

		sm.logger.Info("Reconciling stream configuration drift to the declaration",
			"stream", name,
			"source", decl.source,
			"drift", driftLabels(drifts))
		if _, err := js.UpdateStream(ctx, updateCfg); err != nil {
			return fmt.Errorf("update stream: %w", err)
		}
		return nil
	}

	// Stream doesn't exist - create it
	_, err = js.CreateStream(ctx, streamCfg)
	if err != nil {
		return fmt.Errorf("create stream: %w", err)
	}

	sm.logger.Info("Created JetStream stream",
		"stream", name,
		"subjects", cfg.Subjects,
		"storage", cfg.Storage,
		"source", decl.source,
		"max_age", streamCfg.MaxAge,
		"max_bytes", streamCfg.MaxBytes,
		"discard", streamCfg.Discard.String(),
		"duplicates", streamCfg.Duplicates)

	return nil
}

// subjectsEqual checks if two subject lists are equal (order-independent).
func subjectsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	aSet := make(map[string]bool)
	for _, s := range a {
		aSet[s] = true
	}
	for _, s := range b {
		if !aSet[s] {
			return false
		}
	}
	return true
}
