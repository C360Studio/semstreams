//go:build integration

// Integration test that proves the full violations propagation chain end-to-end:
//
//	governance.WithContext(ctx) → natsClient.Publish (X-Gov-* headers injected)
//	→ wire → natsclient.Subscribe (headers extracted → handler ctx)
//	→ evaluateRulesForMessage (governance.FromContext → *MessageContext)
//	→ stateful evaluator ($message.violations.* condition re-evaluated)
//	→ deny action ($message.max_severity substitution in reason) → audit triple written
//
// This validates the correctness of all five Tag 3 chunks working together,
// and the Fix A bugfix that wired the $message.* re-evaluation gate.
//
// Rule under test (combined AND: $message.violations.has_pii + $caller.role):
//
//	{
//	    "id":    "pii-non-admin-deny",
//	    "type":  "expression",
//	    "conditions": [
//	        {"field": "$message.violations.has_pii", "operator": "eq",  "value": true},
//	        {"field": "$caller.role",                "operator": "ne",  "value": "admin"}
//	    ],
//	    "logic": "and",
//	    "on_enter": [{"type": "deny", "reason": "non-admin caller cannot access content with PII (caller=$caller.id, severity=$message.max_severity)"}]
//	}
//
// Run with: go test -race -tags=integration -run 'TestIntegration_ViolationsPropagation' ./processor/rule/...
package rule_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/auth"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/governance"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/c360studio/semstreams/processor/rule"
	"github.com/c360studio/semstreams/processor/rule/expression"
)

// vpInputSubject is the NATS subject the rule processor listens on for
// violations propagation tests.
const vpInputSubject = "violations.propagation.test.>"

// TestIntegration_ViolationsPropagation_PiiNonAdminDeny proves the full chain
// for combined $message.* + $caller.* AND conditions. Three sub-cases:
//
//   - pii_message_viewer_caller_denies: HasPii=true + viewer role → both conditions
//     TRUE → deny fires → audit triple with substituted reason written.
//
//   - pii_message_admin_caller_no_deny: HasPii=true + admin role → $caller.role ne
//     "admin" is FALSE → AND short-circuits → no deny.
//
//   - clean_message_viewer_caller_no_deny: HasPii=false + viewer role → $message.
//     violations.has_pii eq true is FALSE → AND short-circuits → no deny.
func TestIntegration_ViolationsPropagation_PiiNonAdminDeny(t *testing.T) {
	nc := getTestNATSClient(t)
	ctx := context.Background()

	// Spin up the graph responder so the deny action's audit triple write has
	// somewhere to land. Reuses the helper from deny_integration_test.go.
	addedCh := startGraphResponder(t, nc)

	// Build and start the rule processor wired to the PII non-admin deny rule.
	processor := buildVPRuleProcessor(t, nc)

	testCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	require.NoError(t, processor.Start(testCtx))
	defer processor.Stop(5 * time.Second)

	// Allow the processor's natsclient.Subscribe call to propagate before
	// publishing. NATS server-side subscription registration is asynchronous;
	// without a brief pause the first Publish may arrive before the handler is
	// active. 150ms is well under the 1s negative-assertion window below and
	// the 5s positive-assertion deadline, so this does not mask real failures.
	time.Sleep(150 * time.Millisecond)

	// ---- Sub-case A: PII message + viewer caller → DENY fires -----------------

	t.Run("pii_message_viewer_caller_denies", func(t *testing.T) {
		// Drain any stale triples accumulated during setup.
		drainTriples(addedCh, 100, 10*time.Millisecond)

		// Both conditions must be TRUE for the deny to fire:
		//   $message.violations.has_pii eq true  → satisfied by HasPii=true
		//   $caller.role ne "admin"               → satisfied by role="viewer"
		viewerCtx := auth.WithIdentity(ctx, &auth.Identity{
			ID:   "viewer-bob",
			Role: "viewer",
			Org:  "acme",
		})
		govCtx := governance.WithContext(viewerCtx, &governance.Context{
			HasPii:         true,
			ViolationCount: 1,
			MaxSeverity:    governance.SeverityHigh,
			Allowed:        true,
		})

		// Publish a triggering message. natsclient.Publish injects both
		// X-Caller-* headers (from auth.WithIdentity) and X-Gov-* headers
		// (from governance.WithContext) onto the wire. The processor's Subscribe
		// extracts both, evaluateRulesForMessage converts them to *CallerContext
		// and *MessageContext, the stateful evaluator sees both pseudo-field sets,
		// fires TransitionEntered → deny action → executeDeny substitutes the
		// reason and writes the audit triple.
		payload := mustMarshalVPMessage(t, "acme.ops.test.svc.entity.vp001")
		require.NoError(t, nc.Publish(govCtx, "violations.propagation.test.pii-viewer", payload))

		// Wait for exactly one rule.deny audit triple.
		auditTriples := drainTriples(addedCh, 1, 5*time.Second)

		require.Len(t, auditTriples, 1,
			"PII message + viewer caller: both conditions TRUE → deny → exactly one audit triple")

		at := auditTriples[0]

		assert.Equal(t, rule.PredicateRuleDeny, at.Predicate,
			"audit triple predicate must be %q", rule.PredicateRuleDeny)

		// Full-string equality: both $caller.id and $message.max_severity must be
		// substituted to prove the complete header→ctx→pseudo-field→substitution chain.
		wantReason := "non-admin caller cannot access content with PII (caller=viewer-bob, severity=high)"
		assert.Equal(t, wantReason, at.Object,
			"audit triple object must carry both $caller.* and $message.* substituted reason")

		assert.Equal(t, "pii-non-admin-deny", at.Subject,
			"audit triple subject must be the originating rule ID")
	})

	// ---- Sub-case B: PII message + admin caller → no deny ---------------------

	t.Run("pii_message_admin_caller_no_deny", func(t *testing.T) {
		// Drain residual triples so Len assertion below is precise.
		drainTriples(addedCh, 100, 10*time.Millisecond)

		// $caller.role ne "admin" is FALSE for an admin caller →
		// AND short-circuits → no deny → no audit triple.
		adminCtx := auth.WithIdentity(ctx, &auth.Identity{
			ID:   "admin-alice",
			Role: "admin",
			Org:  "acme",
		})
		govCtx := governance.WithContext(adminCtx, &governance.Context{
			HasPii:         true,
			ViolationCount: 1,
			MaxSeverity:    governance.SeverityHigh,
			Allowed:        true,
		})

		payload := mustMarshalVPMessage(t, "acme.ops.test.svc.entity.vp002")
		require.NoError(t, nc.Publish(govCtx, "violations.propagation.test.pii-admin", payload))

		// A bounded drain is the correct technique for a negative assertion: there
		// is no positive signal to wait for, so we accept the cost of a 1s window.
		written := drainTriples(addedCh, 1, 1*time.Second)
		assert.Empty(t, written,
			"admin caller: $caller.role ne 'admin' is false → AND short-circuits → no deny → no audit triple")
	})

	// ---- Sub-case C: clean message + viewer caller → no deny ------------------

	t.Run("clean_message_viewer_caller_no_deny", func(t *testing.T) {
		drainTriples(addedCh, 100, 10*time.Millisecond)

		// $message.violations.has_pii eq true is FALSE for HasPii=false →
		// AND short-circuits → no deny → no audit triple.
		viewerCtx := auth.WithIdentity(ctx, &auth.Identity{
			ID:   "viewer-charlie",
			Role: "viewer",
			Org:  "acme",
		})
		govCtx := governance.WithContext(viewerCtx, &governance.Context{
			HasPii:         false,
			ViolationCount: 0,
			MaxSeverity:    governance.SeverityNone,
			Allowed:        true,
		})

		payload := mustMarshalVPMessage(t, "acme.ops.test.svc.entity.vp003")
		require.NoError(t, nc.Publish(govCtx, "violations.propagation.test.clean-viewer", payload))

		written := drainTriples(addedCh, 1, 1*time.Second)
		assert.Empty(t, written,
			"clean message: $message.violations.has_pii eq true is false → AND short-circuits → no deny → no audit triple")
	})
}

// buildVPRuleProcessor creates a rule processor configured with the PII
// non-admin deny rule. The rule uses a combined AND condition across the
// $message.* and $caller.* pseudo-field namespaces, which is the exact
// scenario that Fix A (reEvaluateWithCallerFields populating $message.* too)
// and the new reEvaluateWithMessageFields pass were designed to handle.
//
// EnableGraphIntegration is set to true so executeDeny's AddTriple call routes
// through the NATS triple-mutator (SubjectTripleAdd) and reaches addedCh via
// the startGraphResponder handler.
func buildVPRuleProcessor(t *testing.T, nc *natsclient.Client) *rule.Processor {
	t.Helper()

	piiNonAdminDenyRule := rule.Definition{
		ID:   "pii-non-admin-deny",
		Type: "expression",
		Name: "PII Non-Admin Deny Rule",
		Conditions: []expression.ConditionExpression{
			{
				Field:    "$message.violations.has_pii",
				Operator: expression.OpEqual,
				Value:    true,
			},
			{
				Field:    "$caller.role",
				Operator: expression.OpNotEqual,
				Value:    "admin",
			},
		},
		Logic:   expression.LogicAnd,
		Enabled: rule.ModeEnabled,
		OnEnter: []rule.Action{
			{
				Type:   rule.ActionTypeDeny,
				Reason: "non-admin caller cannot access content with PII (caller=$caller.id, severity=$message.max_severity)",
			},
		},
	}

	cfg := rule.DefaultConfig()
	cfg.Ports = &component.PortConfig{
		Inputs: []component.PortDefinition{
			{
				Name:      "semantic_input",
				Type:      "nats",
				Subject:   vpInputSubject,
				Interface: "core.semantic.v1",
				Required:  true,
			},
		},
		Outputs: []component.PortDefinition{
			{
				Name:      "rule_events",
				Type:      "nats",
				Subject:   "events.rule.vp-test",
				Interface: "core.rule.v1",
				Required:  true,
			},
		},
	}
	cfg.InlineRules = []rule.Definition{piiNonAdminDenyRule}
	// EnableGraphIntegration must be true so the triple mutator (SubjectTripleAdd)
	// is reachable for the deny action's audit write.
	cfg.EnableGraphIntegration = true

	metricsRegistry := metric.NewMetricsRegistry()
	processor, err := rule.NewProcessorWithMetrics(nc, &cfg, metricsRegistry)
	require.NoError(t, err)
	processor.SetDecoder(message.NewDecoder(payloadbuiltins.NewTestRegistry(t)))

	require.NoError(t, processor.Initialize())

	return processor
}

// mustMarshalVPMessage encodes a minimal semantic BaseMessage suitable for
// triggering expression rules on the NATS subject for violations propagation
// tests. Mirrors mustMarshalIPMessage from identity_propagation_integration_test.go.
func mustMarshalVPMessage(t *testing.T, entityID string) []byte {
	t.Helper()

	payload := message.NewGenericJSON(map[string]any{
		"entity_id": entityID,
		"action":    "request",
	})

	msg := message.NewBaseMessage(
		message.Type{Domain: "core", Category: "json", Version: "v1"},
		payload,
		"violations-propagation-integration-test",
	)

	data, err := json.Marshal(msg)
	require.NoError(t, err, "marshal test message")
	return data
}
