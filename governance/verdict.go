// Package governance holds the framework's governance-audit payload types.
//
// A governance verdict (deny/approve, ADR-039) is an *event*: N verdicts occur
// per rule over time. ADR-055 §3a retires the previous mechanism — a best-effort
// audit triple written onto a phantom rule-ID "entity" via the graph-ingest
// auto-vivify path that ADR-055 deletes — and replaces it with this registered
// verdict-event payload published to the append-only GOVERNANCE_VERDICT_AUDIT
// JetStream stream. The verdict thus carries its own producer contract (a
// semantic envelope, §2) and lands on an honest, queryable event log instead of
// a mutable, last-write-wins rule-ID record.
package governance

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"time"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadregistry"
)

// Payload type coordinates (the semantic envelope, ADR-055 §2).
const (
	// Domain is the payload-registry domain for governance events.
	Domain = "governance"
	// CategoryVerdict is the payload-registry category for verdict events.
	CategoryVerdict = "verdict"
	// SchemaVersion is the current governance payload schema version.
	SchemaVersion = "v1"
)

// Verdict decision values. These are the subject-safe tokens used in the
// verdict subject (`governance.verdict.{decision}.{rule_token}`).
const (
	// DecisionDeny records an explicit tool-call/structural denial.
	DecisionDeny = "deny"
	// DecisionApprove records an explicit approval.
	DecisionApprove = "approve"
)

// SubjectPrefix is the root of every verdict subject. The GOVERNANCE_VERDICT_AUDIT
// stream captures `governance.verdict.>`.
const SubjectPrefix = "governance.verdict"

// VerdictEvent is the registered audit payload for a governance verdict.
//
// Always-present fields (RuleID, Reason, EntityID, Decision, Timestamp) come from
// the rule action's execution context. LoopID/CallID are OPTIONAL — they are
// echoed from the proposed-call message when the verdict fires on a tool-call
// (ADR-055 §3a) and are empty for entity-state-driven or cron-fired rules, which
// have no inbound call identity.
type VerdictEvent struct {
	// Decision is DecisionDeny or DecisionApprove.
	Decision string `json:"decision"`
	// RuleID is the canonical (un-encoded) rule identifier. Free-form config
	// string; carried here verbatim because the subject token is a lossy hash.
	RuleID string `json:"rule_id"`
	// Reason is the operator-facing verdict reason (substituted).
	Reason string `json:"reason"`
	// EntityID is the entity the verdict concerns (may be empty for some rule
	// contexts).
	EntityID string `json:"entity_id,omitempty"`
	// Timestamp is when the verdict was issued.
	Timestamp time.Time `json:"timestamp"`
	// LoopID is the agentic-loop id, when the verdict fired on a tool-call.
	LoopID string `json:"loop_id,omitempty"`
	// CallID is the tool-call id, when the verdict fired on a tool-call.
	CallID string `json:"call_id,omitempty"`
}

// Validate implements message.Payload. It is deliberately lenient: the only hard
// requirements are a recognized decision and a non-empty rule ID. The audit emit
// is best-effort and must never block a structural verdict, so a permissive
// Validate keeps a sparse-but-real verdict event readable rather than rejecting
// it at decode.
func (e *VerdictEvent) Validate() error {
	switch e.Decision {
	case DecisionDeny, DecisionApprove:
	default:
		return fmt.Errorf("invalid verdict decision %q (must be %q or %q)", e.Decision, DecisionDeny, DecisionApprove)
	}
	if e.RuleID == "" {
		return fmt.Errorf("rule_id required")
	}
	return nil
}

// Schema implements message.Payload.
func (e *VerdictEvent) Schema() message.Type {
	return message.Type{Domain: Domain, Category: CategoryVerdict, Version: SchemaVersion}
}

// MarshalJSON implements json.Marshaler. The Alias indirection avoids infinite
// recursion; the BaseMessage envelope is added by the publisher
// (message.NewBaseMessage), not here.
func (e *VerdictEvent) MarshalJSON() ([]byte, error) {
	type Alias VerdictEvent
	return json.Marshal((*Alias)(e))
}

// UnmarshalJSON implements json.Unmarshaler.
func (e *VerdictEvent) UnmarshalJSON(data []byte) error {
	type Alias VerdictEvent
	return json.Unmarshal(data, (*Alias)(e))
}

// RuleToken returns a deterministic, subject-safe encoding of a rule ID for use
// as a NATS subject token. Rule IDs are free-form config strings that can contain
// `.` (the token separator), spaces, or `*`/`>` (wildcards) — any of which would
// break subject publishing AND filtering. The token is a 64-bit FNV-1a hash in
// hex (16 chars, always subject-safe). The canonical rule_id travels in the
// payload for display/query, so the hash being lossy is acceptable (ADR-055 §3a).
func RuleToken(ruleID string) string {
	h := fnv.New64a()
	// fnv.Write never returns an error.
	_, _ = h.Write([]byte(ruleID))
	return fmt.Sprintf("%016x", h.Sum64())
}

// VerdictSubject builds the publish subject for a verdict event:
// `governance.verdict.{decision}.{rule_token}`. decision should be DecisionDeny
// or DecisionApprove (already subject-safe).
func VerdictSubject(decision, ruleID string) string {
	return fmt.Sprintf("%s.%s.%s", SubjectPrefix, decision, RuleToken(ruleID))
}

// RegisterPayloads registers the governance verdict-event payload with the
// supplied registry. Called from payloadbuiltins.Register during bootstrap so
// any registry-based decoder (audit/ops dashboards replaying the stream) can read
// verdict events off the wire.
func RegisterPayloads(reg *payloadregistry.Registry) error {
	return reg.Register(&payloadregistry.Registration{
		Domain:      Domain,
		Category:    CategoryVerdict,
		Version:     SchemaVersion,
		Description: "Governance deny/approve verdict audit event (ADR-039 / ADR-055 §3a)",
		Factory:     func() any { return &VerdictEvent{} },
	})
}
