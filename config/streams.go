// Package config provides configuration management for SemStreams.
package config

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
)

// StreamConfig defines configuration for a JetStream stream.
type StreamConfig struct {
	Subjects  []string `json:"subjects"`            // Subjects captured by this stream
	Storage   string   `json:"storage,omitempty"`   // "file" or "memory" (default: file)
	MaxAge    string   `json:"max_age,omitempty"`   // TTL for messages (e.g., "168h", "7d")
	MaxBytes  int64    `json:"max_bytes,omitempty"` // Max storage size in bytes (0 = unlimited)
	Retention string   `json:"retention,omitempty"` // "limits", "interest", "workqueue" (default: limits)
	Replicas  int      `json:"replicas,omitempty"`  // Replication factor (default: 1)

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
var governanceVerdictAuditStreamConfig = StreamConfig{
	Subjects:  []string{"governance.verdict.>"},
	Storage:   "file",            // Durable: an audit trail must survive restarts
	MaxAge:    "2160h",           // ~90 days audit/compliance horizon
	MaxBytes:  500 * 1024 * 1024, // 500MB cap
	Retention: "limits",          // Append-only event log (NOT interest/workqueue)
	Replicas:  1,
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
// The hazard this closes at THIS seam specifically: createStream defaults MaxAge
// to 7 days and hardcodes Discard: DiscardOld, and cfg.Streams is an
// operator-authored map with arbitrary keys — so one typo reconciles
// reachability-blind age eviction onto authoritative graph state, or onto
// out-of-line content the graph still references.
func checkOrdinaryStream(name, source string) error {
	return natsclient.CheckOrdinaryStreamName(name, source)
}

// EnsureStreams creates all required JetStream streams based on:
// 1. System streams (LOGS for out-of-band logging)
// 2. Explicit streams defined in config.Streams (highest priority)
// 3. Streams derived from component JetStream output ports
func (sm *StreamsManager) EnsureStreams(ctx context.Context, cfg *Config) error {
	streams := make(map[string]StreamConfig)

	// 1. Always create system streams for observability
	streams["LOGS"] = logsStreamConfig
	streams["HEALTH"] = healthStreamConfig
	streams["METRICS"] = metricsStreamConfig
	streams["FLOWS"] = flowsStreamConfig
	// GOVERNANCE_VERDICT_AUDIT is a framework guarantee (ADR-055 §3a): the
	// append-only verdict audit must exist wherever the rule engine can issue
	// deny/approve verdicts, independent of operator stream config.
	streams["GOVERNANCE_VERDICT_AUDIT"] = governanceVerdictAuditStreamConfig
	sm.logger.Debug("Adding system streams",
		"streams", []string{"LOGS", "HEALTH", "METRICS", "FLOWS", "GOVERNANCE_VERDICT_AUDIT"})

	// 2. Explicit streams from config (can override system streams).
	// cfg.Streams is an operator-authored map with arbitrary keys, so this is
	// the first place a KV/ObjectStore backing-stream name can enter the
	// provisioner. Validate the declaration before it joins the create set, so
	// no create, update, or reconcile call is issued against such a stream.
	// Sorted so a config with several offenders always names the same one.
	for _, name := range slices.Sorted(maps.Keys(cfg.Streams)) {
		sc := cfg.Streams[name]
		if err := checkOrdinaryStream(name, fmt.Sprintf("config.streams[%q]", name)); err != nil {
			return errs.WrapFatal(err, "StreamsManager", "EnsureStreams",
				fmt.Sprintf("validate stream declaration %q", name))
		}
		streams[name] = sc
		sm.logger.Debug("Found explicit stream config", "stream", name, "subjects", sc.Subjects)
	}

	// 3. Derive streams from component JetStream output ports
	for compName, compCfg := range cfg.Components {
		if !compCfg.Enabled {
			continue
		}

		// Parse component config to extract port definitions
		ports, err := sm.extractPortsFromConfig(compCfg.Config)
		if err != nil {
			sm.logger.Debug("Could not parse ports from component config",
				"component", compName, "error", err)
			continue
		}

		for _, port := range ports.Outputs {
			if port.Type != "jetstream" {
				continue
			}

			// Honor explicit stream_name from the port definition before
			// falling back to subject-derived naming. The canonical port
			// type carries stream_name (e.g. agentic-tools' tool.result
			// port declares stream_name: "AGENT" so its publishes land on
			// the existing AGENT stream rather than spawning a derived
			// TOOL stream that would collide with AGENT's "tool.>"
			// capture). This relies on the shadow struct having been
			// retired in favour of component.PortDefinition; a previous
			// shadow stripped this field on JSON unmarshal and silently
			// swallowed every tool.result publish in semspec
			// (project_open_work_2026_05_08.md, bug class 3).
			streamName := port.StreamName
			subjects := []string{strings.ToLower(streamName) + ".>"}
			if streamName == "" {
				streamName = DeriveStreamName(port.Subject)
				subjects = DeriveStreamSubjects(port.Subject)
			}
			if streamName == "" {
				sm.logger.Warn("Could not derive stream name from subject",
					"component", compName, "subject", port.Subject)
				continue
			}

			// Second entry point for a backing-stream name: an operator-authored
			// port. Both shapes reach here — an explicit stream_name, and the
			// subject fallback, since DeriveStreamName uppercases the first
			// subject token ("kv_entity_states.stored" -> "KV_ENTITY_STATES").
			if err := checkOrdinaryStream(streamName,
				fmt.Sprintf("component %q port %q", compName, port.Name)); err != nil {
				return errs.WrapFatal(err, "StreamsManager", "EnsureStreams",
					fmt.Sprintf("validate stream declaration %q", streamName))
			}

			// Only add if not already explicitly configured
			if _, exists := streams[streamName]; !exists {
				streams[streamName] = StreamConfig{
					Subjects: subjects,
					// Defaults will be applied in createStream
				}
				sm.logger.Debug("Derived stream from component port",
					"stream", streamName,
					"component", compName,
					"subject", port.Subject,
					"stream_name_explicit", port.StreamName != "")
			}
		}
	}

	// 4. Create all streams
	for name, streamCfg := range streams {
		if err := sm.createStream(ctx, name, streamCfg); err != nil {
			return fmt.Errorf("create stream %s: %w", name, err)
		}
	}

	sm.logger.Info("Ensured JetStream streams", "count", len(streams))
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
func (sm *StreamsManager) extractPortsFromConfig(rawConfig json.RawMessage) (*PortsConfig, error) {
	var cfg struct {
		Ports PortsConfig `json:"ports"`
	}
	if err := json.Unmarshal(rawConfig, &cfg); err != nil {
		return nil, err
	}
	return &cfg.Ports, nil
}

// createStream creates or updates a JetStream stream.
func (sm *StreamsManager) createStream(ctx context.Context, name string, cfg StreamConfig) error {
	// Fail closed FIRST, before any JetStream access: EnsureStreams validates
	// its declaration set, but createStream is the seam that actually issues
	// CreateStream/UpdateStream, so guarding it too means no future caller path
	// — a new derivation rule, a direct call, a test helper — can reach NATS
	// with a KV or ObjectStore backing-stream name.
	if err := checkOrdinaryStream(name, "stream provisioner"); err != nil {
		return errs.WrapFatal(err, "StreamsManager", "createStream",
			fmt.Sprintf("provision stream %q", name))
	}

	js, err := sm.natsClient.JetStream()
	if err != nil {
		return fmt.Errorf("get JetStream context: %w", err)
	}

	// Parse storage type
	storage := jetstream.FileStorage
	if cfg.Storage == "memory" {
		storage = jetstream.MemoryStorage
	}

	// Parse retention policy
	retention := jetstream.LimitsPolicy
	switch cfg.Retention {
	case "interest":
		retention = jetstream.InterestPolicy
	case "workqueue":
		retention = jetstream.WorkQueuePolicy
	}

	// Parse max age
	var maxAge time.Duration
	if cfg.MaxAge != "" {
		var err error
		maxAge, err = parseDurationWithDays(cfg.MaxAge)
		if err != nil {
			sm.logger.Warn("Invalid max_age, using default",
				"stream", name, "max_age", cfg.MaxAge, "error", err)
			maxAge = 7 * 24 * time.Hour // Default: 7 days
		}
	} else {
		maxAge = 7 * 24 * time.Hour // Default: 7 days
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
			sm.logger.Warn("Invalid duplicates window, using server default",
				"stream", name, "duplicates", cfg.Duplicates, "error", dupErr)
			duplicates = 0
		}
	}
	// The NATS server rejects an explicit duplicate window larger than MaxAge
	// (it does not clamp it). Clamp down to MaxAge with a warning so a
	// misconfigured window degrades gracefully instead of aborting boot in
	// EnsureStreams. maxAge is always > 0 here (defaulted to 7d above).
	if duplicates > maxAge {
		sm.logger.Warn("duplicates window exceeds max_age; clamping to max_age",
			"stream", name, "duplicates", duplicates, "max_age", maxAge)
		duplicates = maxAge
	}

	// Replicas default
	replicas := cfg.Replicas
	if replicas <= 0 {
		replicas = 1
	}

	streamCfg := jetstream.StreamConfig{
		Name:       name,
		Subjects:   cfg.Subjects,
		Storage:    storage,
		Retention:  retention,
		MaxAge:     maxAge,
		MaxBytes:   cfg.MaxBytes, // 0 means unlimited
		Discard:    jetstream.DiscardOld,
		Replicas:   replicas,
		Duplicates: duplicates,
	}

	// Try to get existing stream
	existingStream, err := js.Stream(ctx, name)
	if err == nil {
		// Stream exists - update on drift in subjects or the duplicate window.
		existingCfg := existingStream.CachedInfo().Config

		// When the framework config doesn't specify a window, preserve the
		// server-assigned/operator-set one so a subjects-only update doesn't
		// silently reset it to the server default — and so we never trigger an
		// update purely because the server reports its default (2m) for our
		// unset zero.
		if cfg.Duplicates == "" {
			streamCfg.Duplicates = existingCfg.Duplicates
		}

		subjectsDrift := !subjectsEqual(existingCfg.Subjects, cfg.Subjects)
		duplicatesDrift := cfg.Duplicates != "" && existingCfg.Duplicates != streamCfg.Duplicates
		if subjectsDrift || duplicatesDrift {
			sm.logger.Info("Updating stream config",
				"stream", name,
				"subjects_drift", subjectsDrift,
				"duplicates_drift", duplicatesDrift,
				"old_subjects", existingCfg.Subjects,
				"new_subjects", cfg.Subjects,
				"old_duplicates", existingCfg.Duplicates,
				"new_duplicates", streamCfg.Duplicates)
			_, err = js.UpdateStream(ctx, streamCfg)
			if err != nil {
				return fmt.Errorf("update stream: %w", err)
			}
		} else {
			sm.logger.Debug("Stream already exists with correct config", "stream", name)
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
		"max_age", maxAge,
		"duplicates", duplicates)

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
