package gateddagexec

import (
	"fmt"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	semtypes "github.com/c360studio/semstreams/pkg/types"
	"github.com/c360studio/semstreams/vocabulary"
)

// Default predicate vocabulary. Consumers may override any of these; the
// framework defaults give a working out-of-the-box configuration.
const (
	defaultCompletedPredicate = "gateddag.unit.completed"
	defaultFailedPredicate    = "gateddag.unit.failed"
	defaultDirtiedPredicate   = "gateddag.unit.dirtied"
	defaultDependsOnPredicate = "gateddag.unit.depends-on"
	defaultClaimPredicate     = "gateddag.unit.claim"

	defaultWorkers              = 4
	defaultQueueSize            = 256
	defaultBackstopInterval     = "30s"
	defaultQueryTimeout         = "30s"
	defaultMaxUnits             = 1000
	defaultDispatchStream       = "GATEDDAG_DISPATCH"
	defaultDispatchStreamMaxAge = "24h"
	defaultDispatchDedupeWindow = "2m" // >= default backstop (30s); bounds ack-timeout dedup + reset delay
	defaultStrandedAfter        = "0"  // disabled by default (opt-in, back-compat)

	// defaultDispatchStreamMaxBytes bounds a queue of small dispatch messages
	// (a unit ID plus routing), so 256 MiB is a deep backlog and still a real
	// ceiling rather than "whatever the account has left".
	defaultDispatchStreamMaxBytes int64 = 256 << 20
	// defaultDispatchStreamDiscard refuses the newest dispatch at the ceiling
	// rather than evicting the oldest. See DispatchStreamDiscard: a dropped
	// dispatch strands a claimed unit, which is what this stream exists to prevent.
	defaultDispatchStreamDiscard = "new"
	// defaultDispatchStreamRetention deletes each dispatch once it is acked, so the
	// size ceiling is reached by genuine backlog rather than by successfully
	// processed history. See DispatchStreamRetention.
	defaultDispatchStreamRetention = "workqueue"

	// FailurePolicyContinueOthers keeps independent branches flowing when a unit
	// fails (its dependents stay Blocked; everything else proceeds). Default.
	FailurePolicyContinueOthers = "continue_others"
	// FailurePolicyStopOnFirstFailure halts all new dispatch once any unit has
	// failed (in-flight units still finish). A blunt circuit-breaker.
	FailurePolicyStopOnFirstFailure = "stop_on_first_failure"
)

func init() {
	for _, predicate := range []string{
		defaultCompletedPredicate,
		defaultFailedPredicate,
		defaultDirtiedPredicate,
		defaultDependsOnPredicate,
		defaultClaimPredicate,
		PredicateFanOutPhase,
	} {
		vocabulary.Register(predicate)
	}
}

// Config parameterizes the gated-DAG executor. The zero value is NOT valid —
// NewComponent applies DefaultConfig for unset fields then calls Validate.
type Config struct {
	// FanOutWorkflow is the lifecycle.Workflow.Name this executor watches for
	// re-eval triggers. Defaults to the framework FanOut workflow; a consumer
	// may point it at its own registered workflow. The executor self-registers
	// the framework default when this is left at the default (see component.go).
	FanOutWorkflow string `json:"fan_out_workflow,omitempty"`

	// UnitEntityPrefix is the canonical one-to-six-token literal
	// graph.query.prefix scope read authoritatively each evaluation — the blast
	// radius of one fan-out. REQUIRED (no default: it is the set of entities this
	// executor will act on).
	UnitEntityPrefix string `json:"unit_entity_prefix"`

	// DispatchSubject is published (with the unit entity ID as a reference, never
	// content) when a unit becomes dispatchable. The consumer wires its own
	// handler here. REQUIRED.
	DispatchSubject string `json:"dispatch_subject"`

	// Marker / edge predicate vocabulary. Must be pairwise distinct after
	// defaulting — a collision would make e.g. a completed marker read as a
	// claim, silently corrupting dispatch.
	CompletedPredicate string `json:"completed_predicate,omitempty"`
	FailedPredicate    string `json:"failed_predicate,omitempty"`
	DirtiedPredicate   string `json:"dirtied_predicate,omitempty"`
	DependsOnPredicate string `json:"depends_on_predicate,omitempty"`
	ClaimPredicate     string `json:"claim_predicate,omitempty"`

	// DispatchStream is the JetStream stream the executor ensures at Start and
	// publishes dispatches into (ADR-070). It captures DispatchSubject, so a
	// dispatch is durably queued and delivered whenever a consumer (re)subscribes.
	DispatchStream string `json:"dispatch_stream,omitempty"`
	// DispatchStreamMaxAge bounds the dispatch stream's retention (a work stream,
	// not the graph — ADR-068's no-TTL rule is about ENTITY_STATES). An unconsumed
	// dispatch older than this is dropped. Duration string; must be > 0.
	DispatchStreamMaxAge string `json:"dispatch_stream_max_age,omitempty"`
	// DispatchStreamMaxBytes bounds the dispatch stream's SIZE. Required to be
	// positive for the same reason MaxAge is: 0 and -1 both read as unlimited to
	// JetStream, so leaving it unset lets a stalled consumer grow the stream until
	// the account's storage tier is exhausted — which takes down every other
	// stream in the tier, not just this one. Byte count; must be > 0.
	//
	// An explicit 0 is treated as OMITTED and takes the default. The field is a
	// plain int64, so after JSON decoding an omitted key and a written 0 are the
	// same value and no check can separate them — an operator who means unlimited
	// cannot express it here, and should not: this requirement exists to end
	// unbounded work streams. A negative value IS distinguishable and is rejected.
	DispatchStreamMaxBytes int64 `json:"dispatch_stream_max_bytes,omitempty"`
	// DispatchStreamDiscard is what happens at the size ceiling: "old" evicts the
	// oldest dispatches, "new" refuses the newest.
	//
	// The default is "new", which is the opposite of the framework default and is
	// deliberate. A dispatch is a REQUEST to do work, and the two failures are not
	// symmetric. A REFUSED publish returns an error the executor sees: it rolls the
	// durable claim back and the unit re-selects on the next eval (ADR-070 B1). An
	// EVICTED dispatch was already acked, so the claim stands while the message is
	// silently deleted — the unit is stranded until the stranded-unit detector
	// alerts or someone resets it. Loud and recoverable beats silent and wedged.
	//
	// This default is only safe because DispatchStreamRetention deletes on ack; see
	// there. Validate refuses the combination that is not.
	DispatchStreamDiscard string `json:"dispatch_stream_discard,omitempty"`
	// DispatchStreamRetention is the stream's retention policy: "workqueue"
	// (default) deletes each dispatch once it is acked, "limits" retains it until
	// MaxAge or MaxBytes.
	//
	// "workqueue" is the default because a dispatch is a REQUEST, and it is what
	// makes the size ceiling mean what the discard policy assumes it means. Under
	// "limits" the stream retains SUCCESSFULLY PROCESSED dispatches for the full
	// MaxAge, so the ceiling is reached by acked history rather than by backlog —
	// and paired with discard "new" that refuses all new work on a perfectly
	// healthy system, drained by nothing but time. Under "workqueue" the bytes only
	// accumulate when the consumer is genuinely behind, which is the condition the
	// ceiling is supposed to describe.
	//
	// Choose "limits" only if the consumer topology needs it. A workqueue stream
	// delivers each message once, so its consumers must not have overlapping filter
	// subjects — one durable consumer per dispatch subject, which is the documented
	// pattern. Several independent consumers of the same subject need "limits", and
	// then the discard policy must be "old".
	//
	// Retention cannot be changed on an existing stream: JetStream refuses the
	// update, so a deployment that already has a dispatch stream keeps the policy it
	// was created with. The bind-path divergence report names the difference.
	DispatchStreamRetention string `json:"dispatch_stream_retention,omitempty"`
	// DispatchDedupeWindow is the stream's server-side duplicate-detection window
	// (Nats-Msg-Id = unitID). It makes the B1 claim-rollback safe against an
	// ack-timeout-after-persist: a re-dispatch of the same unit within the window
	// is deduped to one stored message. MUST be >= BackstopInterval (the worst-case
	// re-dispatch latency) or an ack-timeout retry could fall outside the window
	// and double-deliver. A reset-driven re-dispatch within the window is delayed
	// by <= the window. Duration string; must be > 0.
	DispatchDedupeWindow string `json:"dispatch_dedupe_window,omitempty"`
	// StrandedAfter is the age past which a claimed, non-terminal, non-dirtied unit
	// stops being treated as healthy in-flight and instead surfaces as a stall
	// alert (ADR-070; the stranded-unit detector). Duration string; "0" disables
	// the check (back-compat). Set it ABOVE the max legitimate unit runtime — a
	// healthy long-running unit held by the consumer heartbeat is KV-
	// indistinguishable from a stranded one, so this is a soft, alert-only knob.
	StrandedAfter string `json:"stranded_after,omitempty"`

	// Workers / QueueSize bound the dispatch concurrency leg (pkg/dispatch).
	Workers   int `json:"workers,omitempty"`
	QueueSize int `json:"queue_size,omitempty"`

	// BackstopInterval is the period of the unconditional re-eval tick that
	// closes the missed-watch-event hole and surfaces stalls (invariant #4).
	BackstopInterval string `json:"backstop_interval,omitempty"`

	// QueryTimeout bounds each authoritative graph.query.prefix read.
	QueryTimeout string `json:"query_timeout,omitempty"`

	// MaxUnits caps the authoritative whole-set read (QueryPrefixAll bound),
	// guarding reply size / memory. A fan-out larger than this logs a truncation
	// warning rather than silently dropping units.
	MaxUnits int `json:"max_units,omitempty"`

	// FailurePolicy selects how a failed unit affects new dispatch. One of
	// FailurePolicyContinueOthers (default) or FailurePolicyStopOnFirstFailure.
	// (retry_with_backoff is deferred — ADR-046 Phase 2.1 §4.)
	FailurePolicy string `json:"failure_policy,omitempty"`

	// FanOutInstanceID is the 6-part entity ID of the FanOut lifecycle instance
	// this executor owns (GH #364). OPTIONAL: when set, the executor creates the
	// instance in `dispatching` on Start and auto-transitions it to `completed`
	// when every unit is Done. When empty, no instance lifecycle is owned
	// (beta.117 behavior). FanOut *failure* stays consumer-driven either way.
	FanOutInstanceID string `json:"fan_out_instance_id,omitempty"`

	// StallSubject, when set, receives an edge-triggered StallEvent (on the
	// 0→non-zero stall transition) for active wedge-detection (GH #365.3).
	// OPTIONAL: the gated_dag_stalled_units gauge + WARN log are always emitted.
	StallSubject string `json:"stall_subject,omitempty"`
}

// DefaultConfig returns the framework defaults. Required fields
// (UnitEntityPrefix, DispatchSubject) are intentionally left empty — Validate
// rejects a config that does not set them.
func DefaultConfig() Config {
	return Config{
		FanOutWorkflow:       FanOutWorkflow,
		CompletedPredicate:   defaultCompletedPredicate,
		FailedPredicate:      defaultFailedPredicate,
		DirtiedPredicate:     defaultDirtiedPredicate,
		DependsOnPredicate:   defaultDependsOnPredicate,
		ClaimPredicate:       defaultClaimPredicate,
		Workers:              defaultWorkers,
		QueueSize:            defaultQueueSize,
		BackstopInterval:     defaultBackstopInterval,
		QueryTimeout:         defaultQueryTimeout,
		MaxUnits:             defaultMaxUnits,
		FailurePolicy:        FailurePolicyContinueOthers,
		DispatchStream:       defaultDispatchStream,
		DispatchStreamMaxAge: defaultDispatchStreamMaxAge,
		DispatchDedupeWindow: defaultDispatchDedupeWindow,
		StrandedAfter:        defaultStrandedAfter,

		DispatchStreamMaxBytes:  defaultDispatchStreamMaxBytes,
		DispatchStreamDiscard:   defaultDispatchStreamDiscard,
		DispatchStreamRetention: defaultDispatchStreamRetention,
	}
}

// withDefaults returns a copy of c with unset fields filled from DefaultConfig.
func (c Config) withDefaults() Config {
	d := DefaultConfig()
	if c.FanOutWorkflow == "" {
		c.FanOutWorkflow = d.FanOutWorkflow
	}
	if c.CompletedPredicate == "" {
		c.CompletedPredicate = d.CompletedPredicate
	}
	if c.FailedPredicate == "" {
		c.FailedPredicate = d.FailedPredicate
	}
	if c.DirtiedPredicate == "" {
		c.DirtiedPredicate = d.DirtiedPredicate
	}
	if c.DependsOnPredicate == "" {
		c.DependsOnPredicate = d.DependsOnPredicate
	}
	if c.ClaimPredicate == "" {
		c.ClaimPredicate = d.ClaimPredicate
	}
	if c.Workers == 0 {
		c.Workers = d.Workers
	}
	if c.QueueSize == 0 {
		c.QueueSize = d.QueueSize
	}
	if c.BackstopInterval == "" {
		c.BackstopInterval = d.BackstopInterval
	}
	if c.QueryTimeout == "" {
		c.QueryTimeout = d.QueryTimeout
	}
	if c.MaxUnits == 0 {
		c.MaxUnits = d.MaxUnits
	}
	if c.FailurePolicy == "" {
		c.FailurePolicy = d.FailurePolicy
	}
	if c.DispatchStream == "" {
		c.DispatchStream = d.DispatchStream
	}
	if c.DispatchStreamMaxAge == "" {
		c.DispatchStreamMaxAge = d.DispatchStreamMaxAge
	}
	// A ZERO MaxBytes takes the default rather than being passed through. Zero
	// means unlimited to JetStream, and an operator who omitted the field did not
	// ask for that; an operator who wants a different ceiling states one, and a
	// negative value is rejected by Validate rather than silently defaulted.
	if c.DispatchStreamMaxBytes == 0 {
		c.DispatchStreamMaxBytes = d.DispatchStreamMaxBytes
	}
	if c.DispatchStreamDiscard == "" {
		c.DispatchStreamDiscard = d.DispatchStreamDiscard
	}
	if c.DispatchStreamRetention == "" {
		c.DispatchStreamRetention = d.DispatchStreamRetention
	}
	if c.DispatchDedupeWindow == "" {
		c.DispatchDedupeWindow = d.DispatchDedupeWindow
	}
	if c.StrandedAfter == "" {
		c.StrandedAfter = d.StrandedAfter
	}
	return c
}

// Validate checks a defaulted config. Call withDefaults first (NewComponent
// does). Returns the first violation found.
func (c Config) Validate() error {
	if c.UnitEntityPrefix == "" {
		return fmt.Errorf("unit_entity_prefix is required (it scopes the unit set this executor reads)")
	}
	if err := semtypes.ValidateEntityIDPrefix(c.UnitEntityPrefix); err != nil {
		return fmt.Errorf("unit_entity_prefix must be a canonical one-to-six-token literal entity prefix: %w", err)
	}
	if c.DispatchSubject == "" {
		return fmt.Errorf("dispatch_subject is required (it is where a dispatchable unit's reference is published)")
	}
	if c.FanOutWorkflow == "" {
		return fmt.Errorf("fan_out_workflow must not be empty")
	}
	if c.FanOutInstanceID != "" {
		if err := semtypes.ValidateEntityID(c.FanOutInstanceID); err != nil {
			return fmt.Errorf("fan_out_instance_id must be a canonical six-part literal entity ID: %w", err)
		}
	}
	// The framework-owned FanOut instance lifecycle uses the framework FanOut
	// Participant type, whose Workflow() is the default name. Owning an instance
	// under a custom workflow would silently no-op (create under the default,
	// complete under the custom), so reject the combination loudly.
	if c.FanOutInstanceID != "" && c.FanOutWorkflow != FanOutWorkflow {
		return fmt.Errorf("fan_out_instance_id requires the default fan_out_workflow %q (the framework owns only its own FanOut type); got fan_out_workflow=%q — register and drive your own instance lifecycle instead",
			FanOutWorkflow, c.FanOutWorkflow)
	}
	if c.Workers <= 0 {
		return fmt.Errorf("workers must be > 0 (got %d)", c.Workers)
	}
	if c.QueueSize <= 0 {
		return fmt.Errorf("queue_size must be > 0 (got %d)", c.QueueSize)
	}
	if c.MaxUnits <= 0 {
		return fmt.Errorf("max_units must be > 0 (got %d)", c.MaxUnits)
	}
	if _, err := c.backstopInterval(); err != nil {
		return fmt.Errorf("backstop_interval: %w", err)
	}
	if _, err := c.queryTimeout(); err != nil {
		return fmt.Errorf("query_timeout: %w", err)
	}
	if c.DispatchStream == "" {
		return fmt.Errorf("dispatch_stream is required (the durable stream dispatches are published into — ADR-070)")
	}
	if _, err := c.dispatchStreamMaxAge(); err != nil {
		return fmt.Errorf("dispatch_stream_max_age: %w", err)
	}
	if c.DispatchStreamMaxBytes <= 0 {
		return fmt.Errorf(
			"dispatch_stream_max_bytes must be > 0 (got %d); 0 and -1 both mean unlimited to JetStream, "+
				"and an unbounded work stream exhausts the account's storage tier rather than only itself",
			c.DispatchStreamMaxBytes)
	}
	discard, err := c.dispatchStreamDiscard()
	if err != nil {
		return fmt.Errorf("dispatch_stream_discard: %w", err)
	}
	retention, err := c.dispatchStreamRetention()
	if err != nil {
		return fmt.Errorf("dispatch_stream_retention: %w", err)
	}
	// The one combination that stops a HEALTHY system. Under "limits" the stream
	// retains acked dispatches for the full MaxAge, so the finite MaxBytes is
	// reached by successfully processed history; "new" then refuses every further
	// publish, and nothing drains it but time. Refused at validation rather than
	// documented, because the symptom (all dispatch stops while the consumer is
	// idle and healthy) points nowhere near the cause.
	if retention == jetstream.LimitsPolicy && discard == jetstream.DiscardNew {
		return fmt.Errorf(
			`dispatch_stream_retention "limits" cannot be combined with dispatch_stream_discard "new": ` +
				`"limits" retains dispatches after they are acked, so dispatch_stream_max_bytes is reached by ` +
				`processed history rather than by backlog, and "new" then refuses all new dispatch on a ` +
				`healthy system until max_age expires. Use the default "workqueue" retention (deletes on ` +
				`ack), or set dispatch_stream_discard to "old" if the consumer topology requires "limits"`)
	}
	dedupe, err := c.dispatchDedupeWindow()
	if err != nil {
		return fmt.Errorf("dispatch_dedupe_window: %w", err)
	}
	// The dedupe window must cover the worst-case re-dispatch latency (a
	// backstop-driven re-eval) or an ack-timeout retry falls outside it and
	// double-delivers (ADR-070 B1). backstop is already validated above.
	if backstop, berr := c.backstopInterval(); berr == nil && dedupe < backstop {
		return fmt.Errorf("dispatch_dedupe_window (%s) must be >= backstop_interval (%s): a shorter window lets an ack-timeout re-dispatch escape server dedup and double-deliver", dedupe, backstop)
	}
	if _, err := c.strandedAfter(); err != nil {
		return fmt.Errorf("stranded_after: %w", err)
	}
	switch c.FailurePolicy {
	case FailurePolicyContinueOthers, FailurePolicyStopOnFirstFailure:
	default:
		return fmt.Errorf("failure_policy must be %q or %q (got %q)",
			FailurePolicyContinueOthers, FailurePolicyStopOnFirstFailure, c.FailurePolicy)
	}
	// Predicates must be pairwise distinct: a collision silently mis-reads one
	// marker class as another.
	preds := map[string]string{
		"completed_predicate":  c.CompletedPredicate,
		"failed_predicate":     c.FailedPredicate,
		"dirtied_predicate":    c.DirtiedPredicate,
		"depends_on_predicate": c.DependsOnPredicate,
		"claim_predicate":      c.ClaimPredicate,
	}
	seen := make(map[string]string, len(preds))
	for name, val := range preds {
		if val == "" {
			return fmt.Errorf("%s must not be empty", name)
		}
		if err := vocabulary.RequireDeclaredPredicate(val); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		if other, dup := seen[val]; dup {
			return fmt.Errorf("predicate %q is used by both %s and %s; they must be distinct", val, other, name)
		}
		seen[val] = name
	}
	return nil
}

// backstopInterval parses BackstopInterval; must be > 0.
func (c Config) backstopInterval() (time.Duration, error) {
	d, err := time.ParseDuration(c.BackstopInterval)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q: %w", c.BackstopInterval, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("must be > 0 (got %s)", d)
	}
	return d, nil
}

// queryTimeout parses QueryTimeout; must be > 0.
func (c Config) queryTimeout() (time.Duration, error) {
	d, err := time.ParseDuration(c.QueryTimeout)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q: %w", c.QueryTimeout, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("must be > 0 (got %s)", d)
	}
	return d, nil
}

// dispatchStreamDiscard maps DispatchStreamDiscard to its JetStream policy. An
// unrecognized spelling is an ERROR rather than a fallback: silently applying
// delete-oldest to a work stream because a value was misspelled is precisely the
// dropped-dispatch failure the explicit field exists to prevent.
func (c Config) dispatchStreamDiscard() (jetstream.DiscardPolicy, error) {
	switch c.DispatchStreamDiscard {
	case "new":
		return jetstream.DiscardNew, nil
	case "old":
		return jetstream.DiscardOld, nil
	default:
		return 0, fmt.Errorf(
			`must be "new" (refuse the newest dispatch at the ceiling; default, and loud) or `+
				`"old" (evict the oldest, which silently strands whichever unit's dispatch is dropped), got %q`,
			c.DispatchStreamDiscard)
	}
}

// dispatchStreamRetention maps DispatchStreamRetention to its JetStream policy.
// An unrecognized spelling is an ERROR rather than a fallback, on the same
// principle as the discard policy: silently applying one retention model because a
// value was misspelled is how the ceiling comes to mean something the discard
// policy does not expect.
//
// "interest" is deliberately NOT offered. It deletes a message once every
// interested consumer has acked, which for a stream whose consumers are wired by
// the adopter makes durability depend on whether anyone happened to be subscribed
// when the dispatch was published — the opposite of what ADR-070 made this stream
// durable for.
func (c Config) dispatchStreamRetention() (jetstream.RetentionPolicy, error) {
	switch c.DispatchStreamRetention {
	case "workqueue":
		return jetstream.WorkQueuePolicy, nil
	case "limits":
		return jetstream.LimitsPolicy, nil
	default:
		return 0, fmt.Errorf(
			`must be "workqueue" (delete each dispatch on ack; default, and what makes the size ceiling `+
				`mean backlog) or "limits" (retain until max_age/max_bytes; needed only when several `+
				`independent consumers read the same dispatch subject), got %q`,
			c.DispatchStreamRetention)
	}
}

// dispatchStreamMaxAge parses DispatchStreamMaxAge; must be > 0.
func (c Config) dispatchStreamMaxAge() (time.Duration, error) {
	d, err := time.ParseDuration(c.DispatchStreamMaxAge)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q: %w", c.DispatchStreamMaxAge, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("must be > 0 (got %s)", d)
	}
	return d, nil
}

// dispatchDedupeWindow parses DispatchDedupeWindow; must be > 0.
func (c Config) dispatchDedupeWindow() (time.Duration, error) {
	d, err := time.ParseDuration(c.DispatchDedupeWindow)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q: %w", c.DispatchDedupeWindow, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("must be > 0 (got %s)", d)
	}
	return d, nil
}

// strandedAfter parses StrandedAfter; must be >= 0 (0 disables the detector).
func (c Config) strandedAfter() (time.Duration, error) {
	d, err := time.ParseDuration(c.StrandedAfter)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q: %w", c.StrandedAfter, err)
	}
	if d < 0 {
		return 0, fmt.Errorf("must be >= 0 (got %s)", d)
	}
	return d, nil
}
