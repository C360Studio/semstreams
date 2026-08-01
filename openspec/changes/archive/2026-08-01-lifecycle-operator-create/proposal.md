## Why

PR #816 added the lifecycle create lane (`POST /workflows/{type}` → `Manager.Create`)
because the operator surface projected every lifecycle operation *except* the one
that makes the first instance — blocking a framework-only cutover that has
correctly disabled every product writer (gh#814, semdragon).

The route was right. **Projecting `Create` onto an externally-reachable surface
made three of its pre-existing gaps newly load-bearing**, and the review round
(Codex + `semstreams-reviewer`, independently) measured all three against real
NATS. This change closes them, plus the guard defects the same round proved.

The unifying lesson, and the reason this is one change rather than a patch: the
create lane inherited `Create`'s preconditions, its result contract, and its
audit attribution without inspecting any of them. Before, every caller was
in-process app code that composed the entity ID from the workflow's own pattern
and had a compile-checked `Participant`. Now the caller is an unauthenticated
HTTP request.

## What Changes

### The three inherited gaps

- **Entity-ID pattern is enforced on create.** `Create` never checked
  `Workflow.EntityIDPattern` while `List`/`Watch` filter by it and `Despawn`
  validates it. Measured: a create outside the pattern commits, returns 201, is
  readable by `Get`, is **invisible to `List`/`Watch`, and can never be
  reclaimed** — `Despawn` refuses it. Strict owner-lease does not save this: an
  out-of-pattern write is *unclaimed*, not *stale*, so `checkOwnerLease` lets it
  through. Reuses the existing `ErrEntityIDPatternMismatch`.
- **A committed birth is never reported as a failure.** `Create` discards the
  mutation response, and `CreateFromOperator` then does a *separate* `Get`. The
  mutation contract defines `Degraded=true` as "committed durably, read-back
  failed, callers MUST NOT retry" — so a failed read-back after a durable commit
  currently returns 500 for a birth that happened, and the operator's retry then
  gets a 409. The causal response entity is already in hand and is strictly more
  true than a post-hoc read.
- **Operator births carry operator attribution.** `buildInitialTriples`
  hardcodes `TransitionSourceFramework`, so `History` records an HTTP operator
  create as framework-authored — on the highest-privilege operation this surface
  has. The sibling transition lane already passes `TransitionSourceOperator`.

### Two guards that were absent rather than weak

- **`Manager.Register` rejects a `Schema` whose pointer does not implement
  `Participant`.** It validates struct-ness and the `id`/`phase` tags but never
  this, so the unchecked `.(Participant)` assertion **panics on the first
  request** to the new lane. In `Get`/`List`/`Watch` the same line is
  unreachable on a fresh volume; here it needs only "workflow registered +
  non-empty body". This change also *removes the compile-time backstop* that
  previously made the misconfiguration unshippable — an app whose only creator
  is the operator route never writes a compile-checked `mgr.Create(ctx, &T{})`.
- **The must-exist lanes are pinned at the production seam.** Mutation-proved:
  both guards in the real `Manager.UpdateFromOperator` can be disabled and the
  entire repository stays green. The gateway test that carries the claim
  exercises a hand-written fake.

### Operator-surface error fidelity

- **`ErrOwnerQuiesced` is mapped** (409/503, message preserved). It is the *next
  instance of the exact shape* `ErrAlreadyExists` had — newly reachable via
  create's `checkQuiesced`, currently canned to a generic 500 with the text
  swallowed. Its own doc says the refusal "is loud and surfaces to the caller";
  for an ownership-strict cutover, "this process was superseded" is precisely
  the message that must not be dropped. `ErrEntityNotLifecycleManaged` likewise.
- **413 on oversize bodies**, via a shared helper. `MaxBytesReader` surfaces
  `*http.MaxBytesError` through the generic 400 branch on **all three** POST
  lanes — a pre-existing class the create lane joined, and the OpenAPI has
  advertised 413 for two of them since before this work.
- **WebSocket upgrade is GET-only.** `POST ?stream=true` currently routes into
  the WS branch, skipping create and answering with gorilla's plain-text `Bad
  Request` — breaking the uniform `{"error": ...}` envelope the package doc
  guarantees.

### One claim withdrawn

The **workflow-mismatch guard is removed, not fixed.** Measured: every production
`Participant` returns `Workflow()` from a package constant, so a request body
cannot declare a workflow at all and the guard cannot fire. It passed review-side
testing only because the gateway's *fake* participant has a JSON-decodable
workflow field — a test that reconstructs a shape production does not have. The
real route/body binding is the entity-ID pattern check above. The equivalent
correct check (`Workflow.Name` equals the Schema's constant) belongs in
`Register`, alongside the `Participant` assertion.

## Capabilities

### Modified Capabilities

- `lifecycle`: add requirements for operator-initiated creation — entity-ID
  pattern enforcement, committed-birth reporting, operator attribution,
  registration-time `Participant` validation, and the must-exist/no-auto-vivify
  contract stated at the Manager rather than the gateway.

## Impact

- **Code**: `pkg/lifecycle/manager.go` (pattern gate, source-aware create,
  causal response, `Register` validation), `pkg/lifecycle/workflow.go` /
  `tags.go` (schema validation), `gateway/lifecycle-gateway/handlers.go` (error
  mapping, 413 helper, WS method gate), `openapi.go`.
- **Consumers**: gh#814's reporter (semdragon). No sister lockstep — every
  change either fixes a wrong answer or converts a 500 into a correct status.
- **Breaking-ish**: `Register` starts rejecting a workflow whose Schema does not
  implement `Participant`. That configuration could only ever panic at runtime,
  so the failure moves from first-request to boot, which is the intended
  direction; call it out in the adopter note.
- **Acceptance still owed**: the fresh-volume → create → transition → restart →
  history-replay e2e. It is **not** discharged by this change and must not be
  tracked only on the issue a PR closes.
