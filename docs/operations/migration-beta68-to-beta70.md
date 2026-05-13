# Migration Guide: beta.68 → beta.70

## Summary

beta.70 ships the full ADR-039 tool-call governance migration in a
single tag — subject-mode primitives (what would have been beta.69)
AND retirement of the in-process `ToolCallFilter` wiring (Phase 3) —
plus a Gemini-wire metadata-drop bug fix that had been latent since
beta.60.

**No intermediate beta.69 release was tagged.** Earlier drafts of
this migration described a two-tag staged path (beta.69 retains the
in-process filter as an escape hatch, beta.70 retires it). The work
landed in main between releases, so beta.68 consumers upgrade
directly to beta.70 with both changes applied.

This tag is **BREAKING** because the in-process wiring is removed in
the same step that adds subject-mode. Consumers wired against
`agentic.ToolCallFilter` at beta.68 will not compile against beta.70
until they migrate to subject-mode rules.

## What ships

### Subject-mode tool-call governance (ADR-039 Phase 2)

- Rule engine gains `$message.*` substitution namespace (dotted-path
  access into inbound message data; unresolved tokens trip the
  existing silent-pass warning).
- New `approve` action — symmetric to `deny` on audit-triple write,
  asymmetric on short-circuit (approve doesn't terminate the rule's
  remaining actions).
- agentic-loop gains `tool_call_governance.{mode,timeout}` config
  with three modes: `disabled` (default, no governance gate),
  `audit` (publish-but-don't-block), `enforce` (publish-and-wait,
  fail-closed on timeout).
- New subjects: `agent.toolcall.proposed.*` (out), wildcard
  `agent.toolcall.approved.>` / `.rejected.>` (in, demuxed per-call).
- Per-call payload carries `parent_loop_id` from day one so rules
  can match across loop hierarchies for sub-agent governance.
- Subscribe-before-publish race fix via in-process buffered waiter
  channels pre-registered before publish.
- Three Prometheus metrics: verdict duration histogram, verdict
  counter, subscribe-before-publish-failures counter.

### Retirement of in-process filter (ADR-039 Phase 3)

The `agentic.ToolCallFilter` interface and all cross-component
wiring are removed. The list of deleted public symbols is below.

### Gemini-wire metadata-drop bug fix

Cached `TaskMessage.Metadata` was silently dropped on every
Gemini-routed tool call because `processor/agentic-model/client_wire.go:303-313`
pre-populates `ToolCall.Metadata` with `MetadataKeyGoogleThoughtSignature`,
tripping a `len(approved[i].Metadata) == 0` propagation guard in
`processor/agentic-loop/handlers.go`. Now merges per-key with
call-wins-on-conflict.

Latent since beta.60 (ADR-037 wire backend). Affected anyone routing
through Gemini with non-empty TaskMessage metadata
(`action_allowlist`, `related_loops`, custom audit tags, etc.).

## Removed public symbols

| Removed | Replacement |
|---|---|
| `agentic.ToolCallFilter` interface | Subject-mode rules subscribed to `agent.toolcall.proposed.>` |
| `agentic.ToolCallFilterResult` | Rule actions emit verdicts to `agent.toolcall.approved.*` / `.rejected.*` |
| `agentic.ToolCallRejection` | Same |
| `agenticloop.Component.SetToolCallFilter(filter)` | `tool_call_governance.mode = "audit"` or `"enforce"` |
| `agenticloop.MessageHandler.SetToolCallFilter(filter)` | Same |
| `agenticgovernance.Component.ToolCallFilter()` accessor | Filter still operates inside the FilterChain via `ProcessMessage`; no cross-component handoff |
| `agenticgovernance.ToolCallFilter.FilterToolCalls(...)` method | Chain processing is the surviving surface |

## For semspec consumers

### What changes in your wiring code

semspec's beta.68 wiring:

```go
gov := governanceComponent.(*agenticgovernance.Component)
loop := loopComponent.(*agenticloop.Component)
if f := gov.ToolCallFilter(); f != nil {
    loop.SetToolCallFilter(f)
}
```

does not compile at beta.70. After beta.70 this becomes:

```go
// No cross-component glue code needed. The agentic-loop and
// agentic-governance components are constructed independently; each
// reads its own config section. Governance is activated via the
// agentic-loop's tool_call_governance.mode config flag.
```

### Recommended cutover path

Even though both stages ship in a single tag, the operational
sequence is identical to the originally-planned two-stage migration —
you can rehearse it without committing:

#### Stage 1: deploy beta.70 with `mode=audit`

```json
{
  "agentic-loop": {
    "tool_call_governance": {
      "mode": "audit",
      "timeout": "1s"
    }
  }
}
```

In `audit` mode, every proposed call is published to
`agent.toolcall.proposed.*` AND dispatched immediately. Verdicts
that arrive are logged for observability but do NOT gate dispatch.
Rules can be developed and tested without affecting production
tool-call flow.

Verify in metrics:

- `tool_call_governance_verdict_total{decision,mode="audit"}` shows
  approve/reject verdicts firing as expected.
- `tool_call_governance_verdict_duration_seconds{mode="audit"}` —
  inspect p99 to size the enforce-mode timeout.

#### Stage 2: switch to `mode=enforce`

```json
{
  "agentic-loop": {
    "tool_call_governance": {
      "mode": "enforce",
      "timeout": "1s"
    }
  }
}
```

In `enforce` mode the loop **waits** for a per-call verdict and
**fails-closed on timeout** (treats absent verdict as a reject).

There is no in-process filter to coexist with — both fallback paths
that the original staged migration described (in-process at beta.69
escape hatch, governance + in-process running in parallel) are gone.
`mode=enforce` IS the only governance gate.

### Translating beta.68 filter config to subject-mode rules

semspec's filter config (beta.68):

```json
{
  "filter_chain": {
    "filters": [
      {
        "name": "tool_call_governance",
        "tool_call_config": {
          "blocked_command_patterns": ["cd /workspace "]
        }
      }
    ]
  }
}
```

Equivalent subject-mode rule (load into the rule engine):

```json
{
  "name": "block-bash-workspace-leak",
  "subscribe": ["agent.toolcall.proposed.>"],
  "conditions": [
    {"field": "tool_name", "operator": "eq", "value": "bash"},
    {"field": "command", "operator": "contains", "value": "cd /workspace"}
  ],
  "logic": "all",
  "on_enter": [
    {
      "type": "publish",
      "subject": "agent.toolcall.rejected.$message.loop_id.$message.call_id",
      "properties": {
        "decision": "rejected",
        "call_id": "$message.call_id",
        "reason": "writes outside worktree blocked"
      }
    },
    {"type": "deny", "reason": "writes outside worktree blocked"}
  ]
}
```

Each `blocked_command_patterns` entry becomes one rule. The
`publish` + `deny` pair is the canonical ADR-039 rejection pattern:
`publish` emits the verdict to the loop's subscribed subject;
`deny` short-circuits subsequent actions and writes the audit
triple.

See `docs/operations/17-tool-call-governance.md` for the full
operator guide, including `approve` action usage, `$caller.*`
integration, sub-agent governance inheritance via
`parent_loop_id`, and troubleshooting.

## Internal API change: ApprovalFilter

The human-in-the-loop `agentictools.ApprovalFilter` no longer
implements `agentic.ToolCallFilter` (because the interface no longer
exists). Its `FilterToolCalls` method returns an internal
`ApprovalFilterResult` directly, not the old generic shape.

External callers of `ApprovalFilter` are not expected — it has
always been internal to agentic-tools. If you reach for it from
outside the package, change the call site from:

```go
result, err := filter.FilterToolCalls(loopID, calls) // beta.68
```

to:

```go
result := filter.FilterToolCalls(loopID, calls) // beta.70
```

Rejected entries are now of type `ApprovalRejection` rather than
`agentic.ToolCallRejection`. Field shape (`Call`, `Reason`) is
unchanged.

## Other behavior changes

- **New default ports on agentic-loop.** `DefaultConfig` now
  includes `agent.toolcall.proposed`, `agent.toolcall.approved`, and
  `agent.toolcall.rejected` in the port set. Tests pinning the port
  count must update from 6→8 inputs and 7→8 outputs.
- **`ExecutionContext.MessageData`** is a new field on the rule
  engine's execution context. Populated automatically on the
  message-path (NATS-subscribed) rules from
  `GenericJSONPayload.Data`. Nil for entity-state-driven and
  cron-fired rules; `$message.*` substitution silent-passes + warns
  when nil.
- **`Evaluation.MessageData`** is a new field on the stateful
  evaluator's `Evaluation` struct. Populated by the rule processor's
  message-path. Callers constructing `Evaluation` manually (rare —
  almost always the rule processor's job) should populate this for
  message-path evaluations.

## New public API

| Symbol | Purpose |
|---|---|
| `agenticloop.ToolCallGovernanceConfig` | Config struct (Mode + Timeout) |
| `agenticloop.ToolCallGovernanceMode{Disabled,Audit,Enforce}` | Mode constants |
| `agenticloop.GovernanceDispatcher` | Interface; constructed automatically in NewComponent |
| `agenticloop.NewGovernanceDispatcher(cfg, publisher, logger, metrics)` | Manual construction for tests |
| `agenticloop.ProposedToolCallPayload` | Wire shape published to `agent.toolcall.proposed.*` |
| `agenticloop.VerdictPayload` | Wire shape accepted on `agent.toolcall.approved/rejected.>` |
| `rule.ActionTypeApprove` (constant `"approve"`) | New action type |
| `rule.PredicateRuleApprove` (constant `"rule.approve"`) | Audit-triple predicate |

## What stays

- `agentic-governance.ToolCallFilter` struct as a chain-internal
  `Filter` — operates on inbound subjects via the FilterChain's
  `Process` surface; the cross-component accessor that exposed it
  is gone.
- The rule engine's existing surfaces: conditions, expression
  evaluator, `add_triple` / `remove_triple` / `update_triple` /
  `publish` / `publish_agent` / `trigger_workflow` /
  `publish_boid_signal` / `update_kv` / `deny` actions; `$entity.*`,
  `$related.*`, `$state.*`, `$schedule.*`, `$caller.*`
  substitutions.

## Tag-readiness gates

Per the CLAUDE.md breaking-change rule, the relevant e2e tier must
run green before tag:

```bash
task e2e:agentic   # exercises the rule-driven governance path
```

The e2e config at `configs/agentic.json` and scenario at
`test/e2e/scenarios/agentic/scenario.go` include a `verify-tool-call-governance`
stage that hard-fails if the governance verdict counter is zero.
beta.70 shipped with this stage green
(`governance_verdicts_approved_audit:3`).
