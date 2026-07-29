package gateddagexec

import (
	"fmt"

	"github.com/c360studio/semstreams/component"
	semtypes "github.com/c360studio/semstreams/pkg/types"
)

// Schema returns the component configuration schema (static metadata used by
// the registry + operator UI). Mirrors the field set in Config.
func (c Config) Schema() component.ConfigSchema {
	minimumLength := 1
	maximumLength := semtypes.MaxEntityIDBytes
	return component.ConfigSchema{
		Properties: map[string]component.PropertySchema{
			"fan_out_workflow": {
				Type:        "string",
				Description: "lifecycle.Workflow.Name watched for re-eval triggers. Defaults to the framework FanOut workflow (self-registered).",
				Default:     FanOutWorkflow,
				Category:    "basic",
			},
			"unit_entity_prefix": {
				Type:        "string",
				Description: fmt.Sprintf("Required canonical literal entity-ID query prefix with one to six dot-separated tokens and at most %d bytes; wildcards are not accepted. This is the graph.query.prefix scope read authoritatively each evaluation — the blast radius of one fan-out.", semtypes.MaxEntityIDBytes),
				MinLength:   &minimumLength,
				MaxLength:   &maximumLength,
				Pattern:     semtypes.EntityIDLiteralPrefixPattern,
				Category:    "basic",
			},
			"dispatch_subject": {
				Type:        "string",
				Description: "Subject published with the unit entity ID reference when a unit is dispatchable. The consumer wires its handler here. Required.",
				Category:    "basic",
			},
			"completed_predicate": {
				Type:        "string",
				Description: "Triple predicate marking a unit complete.",
				Default:     defaultCompletedPredicate,
				Category:    "advanced",
			},
			"failed_predicate": {
				Type:        "string",
				Description: "Triple predicate marking a unit failed.",
				Default:     defaultFailedPredicate,
				Category:    "advanced",
			},
			"dirtied_predicate": {
				Type:        "string",
				Description: "Triple predicate marking a unit reset/dirtied (re-derives Ready over any stale terminal marker).",
				Default:     defaultDirtiedPredicate,
				Category:    "advanced",
			},
			"depends_on_predicate": {
				Type:        "string",
				Description: "Triple predicate carrying a unit's prerequisite unit IDs (multi-valued; one triple per edge).",
				Default:     defaultDependsOnPredicate,
				Category:    "advanced",
			},
			"claim_predicate": {
				Type:        "string",
				Description: "Triple predicate carrying the durable in-flight claim (the dedup record committed before dispatch).",
				Default:     defaultClaimPredicate,
				Category:    "advanced",
			},
			"workers": {
				Type:        "int",
				Description: "Bounded dispatch concurrency.",
				Default:     defaultWorkers,
				Category:    "advanced",
			},
			"queue_size": {
				Type:        "int",
				Description: "Dispatch submit-queue bound.",
				Default:     defaultQueueSize,
				Category:    "advanced",
			},
			"backstop_interval": {
				Type:        "string",
				Description: "Period of the unconditional re-eval tick that closes the missed-watch-event hole and surfaces stalls.",
				Default:     defaultBackstopInterval,
				Category:    "advanced",
			},
			"query_timeout": {
				Type:        "string",
				Description: "Timeout bounding each authoritative graph.query.prefix read.",
				Default:     defaultQueryTimeout,
				Category:    "advanced",
			},
			"max_units": {
				Type:        "int",
				Description: "Cap on the authoritative whole-set read; a larger fan-out logs a truncation warning.",
				Default:     defaultMaxUnits,
				Category:    "advanced",
			},
			"failure_policy": {
				Type:        "enum",
				Description: "How a failed unit affects new dispatch.",
				Enum:        []string{FailurePolicyContinueOthers, FailurePolicyStopOnFirstFailure},
				Default:     FailurePolicyContinueOthers,
				Category:    "advanced",
			},
			"fan_out_instance_id": {
				Type:        "string",
				Description: fmt.Sprintf("Optional canonical six-part literal entity ID, at most %d bytes, of the FanOut lifecycle instance to own: created in 'dispatching' on Start, auto-transitioned to 'completed' when every unit is Done. Empty or omitted means no instance lifecycle is owned.", semtypes.MaxEntityIDBytes),
				MaxLength:   &maximumLength,
				Pattern:     semtypes.OptionalEntityIDLiteralPattern,
				Category:    "advanced",
			},
			"stall_subject": {
				Type:        "string",
				Description: "Optional subject for an edge-triggered StallEvent on the 0→non-zero stall transition (the gated_dag_stalled_units gauge + WARN log are always emitted).",
				Category:    "advanced",
			},
			"dispatch_stream": {
				Type:        "string",
				Description: "JetStream stream the executor ensures at Start and publishes dispatches into (ADR-070). Use a distinct name per distinct dispatch_subject.",
				Default:     defaultDispatchStream,
				Category:    "advanced",
			},
			"dispatch_stream_max_age": {
				Type:        "string",
				Description: "Retention window for the dispatch stream; an unconsumed dispatch older than this is dropped.",
				Default:     defaultDispatchStreamMaxAge,
				Category:    "advanced",
			},
			"dispatch_stream_max_bytes": {
				// "int", not "integer": the generator maps Go-style spellings and
				// falls back to "string" for anything it does not recognize.
				Type: "int",
				Description: "Size ceiling for the dispatch stream, in bytes. Must be > 0: 0 and -1 both mean " +
					"unlimited to JetStream, and an unbounded work stream exhausts the account's whole storage " +
					"tier rather than only itself.",
				Default:  defaultDispatchStreamMaxBytes,
				Category: "advanced",
			},
			"dispatch_stream_discard": {
				Type: "string",
				Description: "Behavior at the size ceiling: 'new' refuses the newest dispatch (default; the " +
					"executor rolls the claim back and the unit re-selects), 'old' evicts the oldest (which " +
					"silently strands whichever unit's dispatch is dropped). Only safe as 'new' when " +
					"dispatch_stream_retention deletes on ack; 'limits' + 'new' is rejected.",
				Default:  defaultDispatchStreamDiscard,
				Enum:     []string{"new", "old"},
				Category: "advanced",
			},
			"dispatch_stream_retention": {
				Type: "string",
				Description: "Retention policy for the dispatch stream: 'workqueue' (default) deletes each " +
					"dispatch once it is acked, so dispatch_stream_max_bytes is reached by genuine backlog; " +
					"'limits' retains processed dispatches until max_age, so the ceiling fills with acked " +
					"history. Choose 'limits' only when several independent consumers read the same dispatch " +
					"subject, and pair it with discard 'old'. Cannot be changed on an existing stream.",
				Default:  defaultDispatchStreamRetention,
				Enum:     []string{"workqueue", "limits"},
				Category: "advanced",
			},
			"dispatch_dedupe_window": {
				Type:        "string",
				Description: "Server-side duplicate-detection window (Nats-Msg-Id=unitID). Must be >= backstop_interval; makes the claim-rollback safe against an ack-timeout-after-persist (ADR-070 B1).",
				Default:     defaultDispatchDedupeWindow,
				Category:    "advanced",
			},
			"stranded_after": {
				Type:        "string",
				Description: "Age past which a claimed non-terminal unit surfaces as a stall alert instead of counting as in-flight (ADR-070). Set above max unit runtime; '0' disables. Alert-only.",
				Default:     defaultStrandedAfter,
				Category:    "advanced",
			},
		},
		Required: []string{"unit_entity_prefix", "dispatch_subject"},
	}
}
