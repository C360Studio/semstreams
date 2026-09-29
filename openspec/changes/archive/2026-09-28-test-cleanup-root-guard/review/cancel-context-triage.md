# Proposed cancel and finite-context review evidence

Status: source evidence for independent review only. No baseline approval, owner waiver, or new analyzer scope.
The twelve candidates below comprise ten cancel callback variants and two finite-context sites. They are a bounded
subset of the supplied census, not a new source census.

## Checkpoint

- Repository: `/Users/coby/.codex/worktrees/gh1397-runner-tests/semstreams`.
- Base: `f60d78906086f0c9090a99f24c389f32d40cc167`.
- Census input: `/private/tmp/gh1064-current-census.json`.
- Census SHA256 at retention: `025738fb0eb0a37f2c9bbe687c501c4d3ca80b2896d3c8d838f02fc2cebb1857`.
- Candidate identity/fingerprint records appear below. Reconcile them to the final scanner snapshot before baseline use.
- Source hashes at retention appear at the end. All semantic dispositions remain proposed.

## 1. Captured MaxDeliver cancellation (two ownership variants)

Sites: `internal/maxdelivery/observer.go:290`, once with cleanup ownership and once with defer ownership.

Source proof: `start` is declared at :237. It creates `runCtx, cancel := context.WithCancel(ctx)` at :251.
The returned context-taking stop closure begins at :262 and invokes that captured cancel at :290. The error path
at :256 invokes the same cancel; no assignment replaces it before return. This candidate is the captured cancellation
call, distinct from the returned closure’s lifecycle shutdown behavior and from `handle.Stop()` at :288.

Proposed classification: non-lifecycle-stop for this cancellation invocation in each exact ownership variant.

Required finite dependencies:

- `internal/maxdelivery/observer.go :: start` in full, including the typed constructor, returned closure, and captures.
- Typed `context.WithCancel` binding and its second result; do not infer this from `context.CancelFunc` spelling.
- Each caller/returned-callback ownership binding already contributing to the exact candidate variant. Preserve both
  variants. The semantic cancellation proof itself needs no inference about the caller context’s deadline.

Observed analyzer limitation: returned-closure binding retained the closure AST and defining function identity but
did not carry the defining function’s verified cancel provenance into that closure. A narrow constructor-capture fix
is possible; exact source evidence is also sufficient for manual classification. No generic captured-state engine.

## 2. Message handler context helper (two sites)

Sites: `natsclient/message_timeout_test.go:12` and :30.

Both call `messageHandlerContext` and defer result index 1. The complete helper is at `natsclient/stream.go:758–763`:
the disabled branch returns `parent, func() {}` at :760, and the other branch returns
`context.WithTimeout(parent, timeout)` at :762. Thus every returned cancellation callable is either an empty closure
or a typed standard context constructor result. This does not prove the disabled parent context finite.

Proposed classification: non-lifecycle-stop for both deferred cancellation invocations.

Required finite dependencies:

- `natsclient/message_timeout_test.go :: TestMessageHandlerContext_DisabledUsesLifecycleContext`.
- `natsclient/message_timeout_test.go :: TestMessageHandlerContext_PositiveRetainsWorkDeadline`.
- `natsclient/stream.go :: messageHandlerContext` in full, including both return paths and return slots.
- Typed `context.WithTimeout` binding and its second result.

A source review can cover both branches without extending the analyzer into general condition evaluation.

## 3. Dispatcher table-produced cancel (one site)

Site: `pkg/dispatch/dispatcher_test.go:339`, in
`TestDispatcher_StopReportsTerminalPoolTimeoutAndContextCause`.

The local table is at :288–309. Its first `stopCtx` factory creates and invokes a typed WithCancel result
at :295–297, then returns `ctx, func() {}` at :298. Its second factory returns
`context.WithDeadline(context.Background(), time.Now().Add(-time.Second))` at :304–305. The range and subtest bind
`stopCtx, cancelStop := tt.stopCtx()` at :338, then defer the second result at :339. Both factory outputs are known
non-lifecycle cancellation callables; this is not a blanket classification of arbitrary table callbacks.

Proposed classification: non-lifecycle-stop.

Required finite dependencies:

- `pkg/dispatch/dispatcher_test.go :: TestDispatcher_StopReportsTerminalPoolTimeoutAndContextCause` in full,
  including the table field type, both factories, range/subtest binding, and deferred invocation.
- Typed `context.WithCancel` and `context.WithDeadline` constructor bindings.

Prefer this exact finite table evidence to new generic table-dispatch analysis. A added/changed factory must
invalidate the declaration dependency rather than inherit the resolution.

## 4. Dispatch newLane cancel return (three sites)

Sites: `processor/agentic-dispatch/delivery_owner_test.go:408`, :452, and :485, all in
`TestEffectFreeCommandWithFailedResponseRetries`.

The local `newLane` function is at :341–371. It creates `ctx, cancel := context.WithCancel(t.Context())` at :368,
then returns `c, callbacks["user.message"], handles, ctx, cancel` at :370. Each subtest binds the fifth result
and defers it at one of the three sites. The cancel result is unchanged. The other callback result is distinct
and is not evidence about cancellation.

Proposed classification: non-lifecycle-stop for all three exact cancellation variants.

Required finite dependencies:

- `processor/agentic-dispatch/delivery_owner_test.go :: TestEffectFreeCommandWithFailedResponseRetries` in full,
  including local newLane, all three five-result bindings, and deferred invocations.
- Typed `context.WithCancel` binding and result index 1 inside newLane, connected to return index 4.

The fact that t.Context has no deadline does not change the identity of the genuine cancellation callback.
No completion guarantee or lifecycle Stop exemption follows from this fact.

## 5. Rule runtime helper cancel return (two sites)

Sites: `processor/rule/lifecycle_runtime_test.go:185` and :217.

The helper `startRuleRuntimeForTest` at :153–176 creates
`startCtx, cancelStart := context.WithCancel(context.Background())` at :160. Error branches invoke cancelStart
at :163 and :172; they do not replace it. The helper returns `processor, cancelStart` at :175. Both named tests
bind its second result and defer it.

Proposed classification: non-lifecycle-stop for these two cancellation invocations.

Required finite dependencies:

- `processor/rule/lifecycle_runtime_test.go :: startRuleRuntimeForTest` in full, including result slot and constructor.
- `processor/rule/lifecycle_runtime_test.go :: TestRuleStopDeadlineArmCancelsAndJoinsCoordinator`.
- `processor/rule/lifecycle_runtime_test.go :: TestRuleMessageCacheOneGuard`.
- Typed `context.WithCancel` binding and its second result.

A narrow verified cancellation return-slot proof is possible; the exact source review does not require analyzing
the runtime goroutine or production lifecycle implementation as a new cleanup boundary.

## 6. Fresh loop-local finite stop contexts (two sites)

Sites: `component/lifecycle_test_suite.go:185` and :233.

In `testParallelFreshInstances` (:153–198), :184 immediately derives
`stopCtx, cancelStop := context.WithTimeout(context.Background(), 5*time.Second)` before the :185 Stop call.
This declaration is fresh inside the inner loop, itself inside the goroutine. In `testNoResourceLeaks` (:201 onward),
:232 derives the same local five-second context immediately before :233. Neither site relies on a loop-carried
context value or a deadline inferred from a function name.

Proposed classification: bounded-cleanup, meaning only a finite supplied context under this census vocabulary.
This makes no claim that Stop must return within five seconds or that the suite contains generic hang containment.

Required finite dependencies:

- `component/lifecycle_test_suite.go :: testParallelFreshInstances` in full for :185.
- `component/lifecycle_test_suite.go :: testNoResourceLeaks` in full for :233.
- Typed `context.WithTimeout` binding, its local context result, the five-second duration expression, and
  `component.LifecycleComponent.Stop` interface binding.
- `component/lifecycle.go :: LifecycleComponent` declaration.

Observed analyzer cause: in the inspected snapshot, `walkStmt` has no ForStmt handling. Its default branch at
`test/testinfra/cleanup_analyzer_test.go:989–991` calls invalidateAssigned then inspectCalls. Thus loop assignments
are invalidated and calls inspected without sequentially applying the immediately preceding WithTimeout assignment.
This is a bounded unsupported-form limitation, not ambiguous source semantics. Exact manual facts avoid adding a
general loop/CFG engine. If a small loop-local statement-walk correction is chosen, loop-carried or escaping mutation
must remain unknown; do not merely stop invalidating arbitrary outer variables.

## Safety of the proposed distinctions

Never classify by the callback name or by `context.CancelFunc` type alone. A converted arbitrary closure can contain
a real Stop. Preserve the regressions `TestCleanupRootGuardConvertedCancelFuncCanConcealStop` and
`TestCleanupRootGuardDeferredConvertedCancelFuncFailsClosed`; a helper returning a converted arbitrary closure must
remain unknown or expose its Stop as well. Constructor identity/result slot, unchanged binding, and exact return or
capture provenance are the relevant proof. A source dependency change must stale the prior manual record.

No entries are approved here. Missing/ambiguous dependencies, changed ownership variants, or failed type loading
cannot be bypassed by this packet. Independent review must assess the facts and the final exact candidate identities.

## Exact retained candidate records

```json
[
  {
    "applicability": "unresolved",
    "classification": "uncertain-owner-provenance",
    "context": "unknown",
    "fingerprint": "0744290e233da31c1400bfae211f5fa1bd44ad4e73c093e33a26847be1c0e2c0",
    "function": "testParallelFreshInstances",
    "identity": "component/lifecycle_test_suite.go|testParallelFreshInstances|ordinary|github.com/c360studio/semstreams/component.Stop|github.com/c360studio/semstreams/component.LifecycleComponent|unknown|1",
    "line": 185,
    "origin": "ordinary",
    "path": "component/lifecycle_test_suite.go",
    "reason": "context provenance unresolved",
    "receiver": "github.com/c360studio/semstreams/component.LifecycleComponent",
    "selections": [
      "default",
      "integration",
      "live_llm"
    ],
    "target": "github.com/c360studio/semstreams/component.Stop"
  },
  {
    "applicability": "unresolved",
    "classification": "uncertain-owner-provenance",
    "context": "unknown",
    "fingerprint": "0744290e233da31c1400bfae211f5fa1bd44ad4e73c093e33a26847be1c0e2c0",
    "function": "testNoResourceLeaks",
    "identity": "component/lifecycle_test_suite.go|testNoResourceLeaks|ordinary|github.com/c360studio/semstreams/component.Stop|github.com/c360studio/semstreams/component.LifecycleComponent|unknown|1",
    "line": 233,
    "origin": "ordinary",
    "path": "component/lifecycle_test_suite.go",
    "reason": "context provenance unresolved",
    "receiver": "github.com/c360studio/semstreams/component.LifecycleComponent",
    "selections": [
      "default",
      "integration",
      "live_llm"
    ],
    "target": "github.com/c360studio/semstreams/component.Stop"
  },
  {
    "applicability": "unresolved",
    "classification": "uncertain-owner-provenance",
    "context": "unknown",
    "fingerprint": "cc8914e6c17a42338d7d62ca0724732d2e2ffb84b27e57c183a5082bc629c4d1",
    "function": "start",
    "identity": "internal/maxdelivery/observer.go|start|cleanup|cancel|unresolved callback|unknown|1",
    "line": 290,
    "origin": "cleanup",
    "path": "internal/maxdelivery/observer.go",
    "reason": "deferred or cleanup callback variable target unresolved",
    "receiver": "unresolved callback",
    "selections": [
      "integration"
    ],
    "target": "cancel"
  },
  {
    "applicability": "unresolved",
    "classification": "uncertain-owner-provenance",
    "context": "unknown",
    "fingerprint": "745e003f74654a2c768b420f7fd33cf416430ea9f5010add694b642b25da7ea6",
    "function": "start",
    "identity": "internal/maxdelivery/observer.go|start|defer|cancel|unresolved callback|unknown|1",
    "line": 290,
    "origin": "defer",
    "path": "internal/maxdelivery/observer.go",
    "reason": "deferred or cleanup callback variable target unresolved",
    "receiver": "unresolved callback",
    "selections": [
      "integration"
    ],
    "target": "cancel"
  },
  {
    "applicability": "unresolved",
    "classification": "uncertain-owner-provenance",
    "context": "unknown",
    "fingerprint": "745e003f74654a2c768b420f7fd33cf416430ea9f5010add694b642b25da7ea6",
    "function": "TestMessageHandlerContext_DisabledUsesLifecycleContext",
    "identity": "natsclient/message_timeout_test.go|TestMessageHandlerContext_DisabledUsesLifecycleContext|defer|cancel|unresolved callback|unknown|1",
    "line": 12,
    "origin": "defer",
    "path": "natsclient/message_timeout_test.go",
    "reason": "deferred or cleanup callback variable target unresolved",
    "receiver": "unresolved callback",
    "selections": [
      "default",
      "integration",
      "live_llm"
    ],
    "target": "cancel"
  },
  {
    "applicability": "unresolved",
    "classification": "uncertain-owner-provenance",
    "context": "unknown",
    "fingerprint": "745e003f74654a2c768b420f7fd33cf416430ea9f5010add694b642b25da7ea6",
    "function": "TestMessageHandlerContext_PositiveRetainsWorkDeadline",
    "identity": "natsclient/message_timeout_test.go|TestMessageHandlerContext_PositiveRetainsWorkDeadline|defer|cancel|unresolved callback|unknown|1",
    "line": 30,
    "origin": "defer",
    "path": "natsclient/message_timeout_test.go",
    "reason": "deferred or cleanup callback variable target unresolved",
    "receiver": "unresolved callback",
    "selections": [
      "default",
      "integration",
      "live_llm"
    ],
    "target": "cancel"
  },
  {
    "applicability": "unresolved",
    "classification": "uncertain-owner-provenance",
    "context": "unknown",
    "fingerprint": "3b4045e817795c5a651cfbdd6be6aeda945d270ad43b2db02ce197e89ee6592e",
    "function": "TestDispatcher_StopReportsTerminalPoolTimeoutAndContextCause",
    "identity": "pkg/dispatch/dispatcher_test.go|TestDispatcher_StopReportsTerminalPoolTimeoutAndContextCause|defer|cancelStop|unresolved callback|unknown|1",
    "line": 339,
    "origin": "defer",
    "path": "pkg/dispatch/dispatcher_test.go",
    "reason": "deferred or cleanup callback variable target unresolved",
    "receiver": "unresolved callback",
    "selections": [
      "default",
      "integration",
      "live_llm"
    ],
    "target": "cancelStop"
  },
  {
    "applicability": "unresolved",
    "classification": "uncertain-owner-provenance",
    "context": "unknown",
    "fingerprint": "745e003f74654a2c768b420f7fd33cf416430ea9f5010add694b642b25da7ea6",
    "function": "TestEffectFreeCommandWithFailedResponseRetries",
    "identity": "processor/agentic-dispatch/delivery_owner_test.go|TestEffectFreeCommandWithFailedResponseRetries|defer|cancel|unresolved callback|unknown|1",
    "line": 408,
    "origin": "defer",
    "path": "processor/agentic-dispatch/delivery_owner_test.go",
    "reason": "deferred or cleanup callback variable target unresolved",
    "receiver": "unresolved callback",
    "selections": [
      "default",
      "integration",
      "live_llm"
    ],
    "target": "cancel"
  },
  {
    "applicability": "unresolved",
    "classification": "uncertain-owner-provenance",
    "context": "unknown",
    "fingerprint": "745e003f74654a2c768b420f7fd33cf416430ea9f5010add694b642b25da7ea6",
    "function": "TestEffectFreeCommandWithFailedResponseRetries",
    "identity": "processor/agentic-dispatch/delivery_owner_test.go|TestEffectFreeCommandWithFailedResponseRetries|defer|cancel|unresolved callback|unknown|2",
    "line": 452,
    "origin": "defer",
    "path": "processor/agentic-dispatch/delivery_owner_test.go",
    "reason": "deferred or cleanup callback variable target unresolved",
    "receiver": "unresolved callback",
    "selections": [
      "default",
      "integration",
      "live_llm"
    ],
    "target": "cancel"
  },
  {
    "applicability": "unresolved",
    "classification": "uncertain-owner-provenance",
    "context": "unknown",
    "fingerprint": "745e003f74654a2c768b420f7fd33cf416430ea9f5010add694b642b25da7ea6",
    "function": "TestEffectFreeCommandWithFailedResponseRetries",
    "identity": "processor/agentic-dispatch/delivery_owner_test.go|TestEffectFreeCommandWithFailedResponseRetries|defer|cancel|unresolved callback|unknown|3",
    "line": 485,
    "origin": "defer",
    "path": "processor/agentic-dispatch/delivery_owner_test.go",
    "reason": "deferred or cleanup callback variable target unresolved",
    "receiver": "unresolved callback",
    "selections": [
      "default",
      "integration",
      "live_llm"
    ],
    "target": "cancel"
  },
  {
    "applicability": "unresolved",
    "classification": "uncertain-owner-provenance",
    "context": "unknown",
    "fingerprint": "cd280e40cf5f43238633ec3b15c8be535076f7b8b385c32049981bb3ccd347fb",
    "function": "TestRuleStopDeadlineArmCancelsAndJoinsCoordinator",
    "identity": "processor/rule/lifecycle_runtime_test.go|TestRuleStopDeadlineArmCancelsAndJoinsCoordinator|defer|cancelStart|unresolved callback|unknown|1",
    "line": 185,
    "origin": "defer",
    "path": "processor/rule/lifecycle_runtime_test.go",
    "reason": "deferred or cleanup callback variable target unresolved",
    "receiver": "unresolved callback",
    "selections": [
      "default",
      "integration",
      "live_llm"
    ],
    "target": "cancelStart"
  },
  {
    "applicability": "unresolved",
    "classification": "uncertain-owner-provenance",
    "context": "unknown",
    "fingerprint": "cd280e40cf5f43238633ec3b15c8be535076f7b8b385c32049981bb3ccd347fb",
    "function": "TestRuleMessageCacheOneGuard",
    "identity": "processor/rule/lifecycle_runtime_test.go|TestRuleMessageCacheOneGuard|defer|cancelStart|unresolved callback|unknown|1",
    "line": 217,
    "origin": "defer",
    "path": "processor/rule/lifecycle_runtime_test.go",
    "reason": "deferred or cleanup callback variable target unresolved",
    "receiver": "unresolved callback",
    "selections": [
      "default",
      "integration",
      "live_llm"
    ],
    "target": "cancelStart"
  }
]
```

## Source snapshots

| Source | Worktree SHA256 | Matches base HEAD |
|---|---|---|
| `component/lifecycle.go` | `42bba6807fd871ea5a964a5de405bd66ded52a02078d49ee148a297178935f5f` | true |
| `component/lifecycle_test_suite.go` | `da21a4b65040b55a206c5289d986ed612c9cda1d6ec3d0e56079388691547d89` | true |
| `internal/maxdelivery/observer.go` | `319f55b0a8d64d60cfc5bb051b953aca801ee6b1e2b6863f7d7bbf3a8c401669` | true |
| `natsclient/message_timeout_test.go` | `e812c291e58fc2318715ae68371c0ece10ad7fa6b38568c0de6bc285765c890b` | true |
| `natsclient/stream.go` | `44092bfbcab1c3a7c242c17fdb4988a1ac37664a4d6cf5703a8b56083d5ce204` | true |
| `pkg/dispatch/dispatcher_test.go` | `d6ae8d6ab284b56a42445aa04a6ba978c12049a5f55fc849c0ff06758265efd4` | true |
| `processor/agentic-dispatch/delivery_owner_test.go` | `761801ab27799a56604467587bb3bd83c0f8055effa9692dd85615cb655e298f` | true |
| `processor/rule/lifecycle_runtime_test.go` | `ff371e1a1eba059ff4a281063178740bb7195ed9c8c0c23fa2cfc34556b28df1` | true |

Tooling note: the targeted gopls definition query for messageHandlerContext failed with Go build-cache permissions
and no definition location. Its declaration was then located in tracked source and read directly; no caller-population
or absence claim relies on that failed query. All assertions here are positive source facts at the listed declarations.
