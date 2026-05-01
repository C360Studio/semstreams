//go:build integration

// Integration test that proves the full identity propagation chain end-to-end:
//
//	auth.WithIdentity(ctx) → natsClient.Publish (X-Caller-* headers injected)
//	→ wire → natsclient.Subscribe (headers extracted → handler ctx)
//	→ evaluateRulesForMessage (IdentityFromContext → *CallerContext)
//	→ stateful evaluator ($caller.role condition re-evaluated with caller fields)
//	→ deny action ($caller.* substitution in reason) → audit triple written
//
// This validates the correctness of all five Tag 2 chunks working together.
// No HTTP layer is added — ctx-direct publish is the correct boundary (the
// HTTP→ctx path is independently proven by natsclient/identity_integration_test.go).
//
// Run with: go test -race -tags=integration -run 'TestIntegration_IdentityPropagation' ./processor/rule/...
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
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/c360studio/semstreams/processor/rule"
	"github.com/c360studio/semstreams/processor/rule/expression"
)

// ipInputSubject is the NATS subject the rule processor listens on for
// identity propagation tests. The trailing wildcard lets us publish to
// distinct sub-subjects per sub-case without extra configuration.
const ipInputSubject = "identity.propagation.test.>"

// TestIntegration_IdentityPropagation_RoleBasedDeny proves the full chain:
// an admin caller is passed through without triggering the deny rule, while
// a viewer caller satisfies the $caller.role != "admin" condition, fires the
// deny action, and produces an audit triple with the substituted reason.
//
// Rule under test (proper $caller.role condition shape, NOT a sentinel field):
//
//	{
//	    "id":    "role-based-deny-rule",
//	    "type":  "expression",
//	    "conditions": [{"field": "$caller.role", "operator": "ne", "value": "admin"}],
//	    "logic": "and",
//	    "on_enter": [{"type": "deny", "reason": "user $caller.id (role=$caller.role) blocked from acme operations"}]
//	}
func TestIntegration_IdentityPropagation_RoleBasedDeny(t *testing.T) {
	nc := getTestNATSClient(t)
	ctx := context.Background()

	// Spin up the graph responder so the deny action's audit triple write has
	// somewhere to land. Reuses the helper from deny_integration_test.go.
	addedCh := startGraphResponder(t, nc)

	// Build and start the rule processor wired to the role-based deny rule.
	processor := buildIPRuleProcessor(t, nc)

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

	// ---- Sub-case A: admin caller → condition FALSE → no deny ---------------

	t.Run("admin_caller_no_deny", func(t *testing.T) {
		// Drain any stale triples accumulated during setup.
		drainTriples(addedCh, 100, 10*time.Millisecond)

		adminCtx := auth.WithIdentity(ctx, &auth.Identity{
			ID:   "admin-1",
			Role: "admin",
			Org:  "acme",
		})

		// Publish a triggering message. natsclient.Publish injects the
		// X-Caller-* headers from adminCtx onto the wire.
		payload := mustMarshalIPMessage(t, "acme.ops.test.svc.entity.admin001")
		require.NoError(t, nc.Publish(adminCtx, "identity.propagation.test.admin", payload))

		// The condition "$caller.role ne admin" is FALSE for an admin caller →
		// the stateful evaluator returns TransitionNone → no deny action →
		// no audit triple. Wait a bounded window and assert the channel is empty.
		//
		// A bounded drain (not an arbitrary sleep) is the correct technique for
		// a negative assertion: there is no positive signal to wait for, so we
		// accept the cost of a short fixed window.
		written := drainTriples(addedCh, 1, 1*time.Second)
		assert.Empty(t, written,
			"admin caller: condition ($caller.role ne 'admin') is false → no deny → no audit triple")
	})

	// ---- Sub-case B: viewer caller → condition TRUE → deny fires -------------

	t.Run("viewer_caller_deny_with_substituted_reason", func(t *testing.T) {
		// Drain residual triples so Len assertion below is precise.
		drainTriples(addedCh, 100, 10*time.Millisecond)

		viewerCtx := auth.WithIdentity(ctx, &auth.Identity{
			ID:   "viewer-bob",
			Role: "viewer",
			Org:  "acme",
		})

		// Publish a triggering message. natsclient.Publish injects the viewer's
		// X-Caller-* headers. The processor's Subscribe extracts them into the
		// handler ctx, evaluateRulesForMessage converts to *CallerContext, the
		// stateful evaluator sees "$caller.role ne admin" as TRUE (viewer != admin),
		// fires TransitionEntered → deny action → executeDeny substitutes the reason
		// and writes the audit triple.
		payload := mustMarshalIPMessage(t, "acme.ops.test.svc.entity.viewer001")
		require.NoError(t, nc.Publish(viewerCtx, "identity.propagation.test.viewer", payload))

		// Wait for exactly one rule.deny audit triple.
		auditTriples := drainTriples(addedCh, 1, 5*time.Second)

		require.Len(t, auditTriples, 1,
			"viewer caller: condition ($caller.role ne 'admin') is true → deny → exactly one audit triple")

		at := auditTriples[0]

		assert.Equal(t, rule.PredicateRuleDeny, at.Predicate,
			"audit triple predicate must be %q", rule.PredicateRuleDeny)

		// This is the canonical correctness assertion for the full Tag 2 chain:
		// $caller.id and $caller.role in the reason template must be resolved to
		// the values that travelled via X-Caller-* headers through NATS.
		wantReason := "user viewer-bob (role=viewer) blocked from acme operations"
		assert.Equal(t, wantReason, at.Object,
			"audit triple object must carry the $caller.* substituted reason proving header→ctx→CallerContext round-trip")

		assert.Equal(t, "role-based-deny-rule", at.Subject,
			"audit triple subject must be the originating rule ID")
	})
}

// buildIPRuleProcessor creates a rule processor configured with the role-based
// deny rule and the graph responder's NATS triple-mutator wired in.
//
// The rule definition deliberately uses the proper $caller.role condition
// (operator "ne", value "admin") — NOT a sentinel workaround. This is valid
// because chunk 5 wired $caller.* field population into the expression evaluator's
// stateFields map.
//
// EnableGraphIntegration is set to true so executeDeny's AddTriple call routes
// through the NATS triple-mutator (SubjectTripleAdd) and reaches addedCh via
// the startGraphResponder handler.
func buildIPRuleProcessor(t *testing.T, nc *natsclient.Client) *rule.Processor {
	t.Helper()

	roleDenyRule := rule.Definition{
		ID:   "role-based-deny-rule",
		Type: "expression",
		Name: "Role-Based Deny Rule",
		Conditions: []expression.ConditionExpression{
			{
				Field:    "$caller.role",
				Operator: expression.OpNotEqual,
				Value:    "admin",
			},
		},
		Logic:   expression.LogicAnd,
		Enabled: true,
		// No Entity.Pattern set → Subscribe() returns [">"] → matches any
		// subject the processor receives.
		OnEnter: []rule.Action{
			{
				Type:   rule.ActionTypeDeny,
				Reason: "user $caller.id (role=$caller.role) blocked from acme operations",
			},
		},
	}

	cfg := rule.DefaultConfig()
	cfg.Ports = &component.PortConfig{
		Inputs: []component.PortDefinition{
			{
				Name:      "semantic_input",
				Type:      "nats",
				Subject:   ipInputSubject,
				Interface: "core.semantic.v1",
				Required:  true,
			},
		},
		Outputs: []component.PortDefinition{
			{
				Name:      "rule_events",
				Type:      "nats",
				Subject:   "events.rule.ip-test",
				Interface: "core.rule.v1",
				Required:  true,
			},
		},
	}
	cfg.InlineRules = []rule.Definition{roleDenyRule}
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

// mustMarshalIPMessage encodes a minimal semantic BaseMessage suitable for
// triggering expression rules on the NATS subject. The entity_id field lets
// the rule processor extract a stable entity ID for state tracking.
func mustMarshalIPMessage(t *testing.T, entityID string) []byte {
	t.Helper()

	payload := message.NewGenericJSON(map[string]any{
		"entity_id": entityID,
		"action":    "request",
	})

	msg := message.NewBaseMessage(
		message.Type{Domain: "core", Category: "json", Version: "v1"},
		payload,
		"identity-propagation-integration-test",
	)

	data, err := json.Marshal(msg)
	require.NoError(t, err, "marshal test message")
	return data
}
