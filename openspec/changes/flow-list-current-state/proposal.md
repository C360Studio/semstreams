# Proposal — flow-list-current-state

Slice B of the accepted flow-CRUD design (`docs/proposals/gh1008-1010-flow-crud-design.md`, owner-accepted
2026-08-23; §"Slice B: #1010 current-state List"). Closes #1010.

## Why

`flowstore.Manager.List` is two-phase and fails closed (`flowstore/manager.go:212-230`): it enumerates keys through
the raw bucket (`:214`), reads each key (`:221`), and aborts the whole list on any per-key error (`:222-225`). A Flow
deleted by one client between the enumeration and its read turns an unrelated client's `GET /flowbuilder/flows` into
a 500 — 12% of list calls under churn in the issue's reproduction — and the abort is classified `WrapTransient`, so a
caller with correct retry logic retries a request that would succeed on its own.

The empty bucket is a second defect of the same shape. The raw `bucket.Keys` returns `jetstream.ErrNoKeysFound` for
an empty bucket, List wraps it transient (`:215-217`), and each consumer then decides what "empty" means by itself:
`handleListFlows` string-matches `"no keys found"` into `{"flows":[]}` (`service/flow_service.go:269-271`), the
startup default-flow import repeats the substring (`:128`), and the FlowExecutor `list_flows` tool never reaches its
own empty branch (`processor/agentic-tools/executors/flows.go:179-184`) — a model listing an empty store gets a tool
error. `natsclient.KVStore.Keys` already normalises the empty bucket to `nil, nil` (`natsclient/kv.go:494-508`); Flow
List is the one Pattern-B manager that bypasses it.

A third defect rides on the abort: List re-wraps every per-key failure transient (`:223`), and `errs.IsFatal` /
`errs.IsTransient` resolve the outermost classified error (`pkg/errs/errs.go:155-163,199-207`), so a stored record
that does not decode — fatal from `Manager.Get` (`:109`) — reaches every List consumer as transient. Slice C's failure
projection (deadline → 504, transient → 503, corrupt → 500) needs the class the Manager assigned, not the class the
loop re-stamped.

Message-substring branching on a classified failure is the shape `openspec/specs/nats-kv-keys/spec.md:167` forbids.

## What Changes

- **`Manager.List` reads current state.** Keys are enumerated through `KVStore.Keys`, so an empty bucket is a
  successful non-nil empty `[]*Flow`. A key that is absent at its read — `errors.Is(err, natsclient.ErrKVKeyNotFound)`,
  which `KVStore.Get` returns for a never-created and for a tombstoned key (`natsclient/kv.go:76-93`) and which
  survives `Manager.Get`'s wrap through `ClassifiedError.Unwrap` (`pkg/errs/errs.go:121-124`) — is omitted. Every
  other per-key failure (transport, permission, deadline or cancellation, a stored record that does not decode) aborts
  the list with a nil result and is returned with the classification `Manager.Get` gave it; List no longer re-wraps it.
  No message text is inspected; no ordering is promised.
- **Empty is a normal outcome for every consumer.** `GET /flows` responds `200 {"flows":[]}` with a present, non-null
  array; the startup default-flow import proceeds on the typed empty list; the real FlowExecutor `list_flows` returns
  a completion whose content is exactly `No flows configured.` with no error attachment. Both substring branches are
  deleted; the tool code is unchanged and its empty branch becomes reachable.
- **`FlowListResponse`.** A `service` HTTP-boundary type (`Flows []flowstore.Flow` tagged `json:"flows"`) registered
  in `ResponseTypes` and referenced by `GET /flows` `200`, so the generated OpenAPI declares `flows` required, typed as
  an array of the Flow object schema, with no nullable item. Value elements are what make the items non-nullable: the
  generator renders a pointer element as `anyOf [..., null]` (`service/schema.go:12-20`). The handler builds the
  response from the Manager's `[]*Flow` through one small conversion.
- **A package-private list seam.** `Manager.beforeListGet` — nil in production, set only from `package flowstore`
  tests — is invoked immediately before each per-key read, so the vanished-key and per-key-failure proofs are
  deterministic (the Slice A `beforeUpdateWrite` precedent, `flowstore/manager.go:26-33`).
- **The raw bucket handle goes.** `Manager.bucket` (`:23`) exists "for operations like Keys()" and List is its only
  reader (`grep -n 's\.bucket' flowstore/*.go` → `:214`); once List reads through `KVStore` the field is removed
  rather than left as a stale claim.

### Consumers

- semstreams-ui: the Flow list route (`src/routes/flows/+page.ts:7-36`), `flowApi.listFlows`
  (`src/lib/services/flowApi.ts:82-97`), Ops summary `fetchFlowList` (`src/lib/services/opsSummaryApi.ts:196-215`),
  and the E2E list and orphan-cleanup helpers (`e2e/helpers/backend-helpers.ts:57-80,339-374`) — the churn that
  reproduces #1010. All keep working; they stop seeing random 500s. (Locations per the accepted inventory; the sister
  repo is hands-off and was not re-read.)
- Model-facing `list_flows` through every FlowExecutor composition (`cmd/semstreams/main.go:245,710`;
  `cmd/e2e-semstreams/main.go:185,421`): unchanged tool code.
- `FlowService.Start` default-flow import (`service/flow_service.go:113,126-158`): same outcome, typed path.
- In-repo e2e client `ObservabilityClient.GetFlows` (`test/e2e/client/observability.go:87-114`) decodes `flows` into
  a slice; unchanged, and it has no callers at this baseline.
- Generated OpenAPI clients gain `FlowListResponse`.

## Non-goals

- Slice C (#1008 vocabulary, exact HTTP error messages, List failure projection deadline→504 / transient→503 /
  fatal→500, must-exist DELETE) and Slice D (the six Get projections). `GET /flows` keeps its opaque `500` on a List
  failure; Slice B only guarantees that the Manager preserves the failure's class so Slice C can project it.
- `Manager.Get` is untouched: a missing key still surfaces from Get as a transient-classified error carrying
  `natsclient.ErrKVKeyNotFound`; Slice D owns typed absence → 404.
- No change to the `FlowManager` interface (`executors/flows.go:16-22`), its in-memory fake, or the `list_flows` tool
  text. The design quotes the tool's empty content as `No flows configured`; the code emits `No flows configured.`
  (`flows.go:184`) and this change pins the emitted literal. Dropping the period is an owner call, not Slice B.
- No ordering, pagination, or partial-result-as-success semantics.
- The sibling Pattern-B lists (`persona/manager.go:145-161`, `flowtemplate/manager.go:111-124`) skip every per-key
  failure including corrupt records; they are precedent, not target state, and are not changed.
- Other `"no keys found"` substring spellings outside the Flow path (`service/message_logger_http.go:497`,
  `graph/clustering/storage.go:286,434,481`, `graph/clustering/summary_store.go:237`) are same-class debt to file,
  not Slice B work.
- No `$ref` reuse inside generated schemas: the generator inlines nested struct schemas (`service/schema.go:46-54`;
  precedent `specs/openapi.v3.yaml:1759-1780`), so `flows.items` is an inline Flow object schema. Changing the
  generator is not Slice B.
- No ADR: Slice B changes no decision, only a read's failure handling. Not BREAKING.
- The named E2E scenarios `flow-list-current-state` (core) and `flow-crud-tools-empty` (`task e2e:crud-tools`)
  belong to the combined-candidate proof, not to this PR — see tasks §8.
- No new exported surface on `natsclient`, `graph`, `message`, or `pkg/*`.

## Impact

- **Affected spec:** `flow-authoring` (ADDED requirements; no existing requirement text changes).
- **Affected code:** `flowstore/manager.go` (List, the seam, the raw-bucket field),
  `flowstore/manager_integration_test.go`, `service/flow_service.go` (`FlowListResponse` and its builder,
  `handleListFlows`, `ensureDefaultFlowFromConfig`, OpenAPI response ref and `ResponseTypes`),
  `service/flow_surface_test.go`, `service/flow_service_test.go`,
  `processor/agentic-tools/executors/flows_integration_test.go` (new), `specs/openapi.v3.yaml` (regenerated).
- **Not breaking.** `Manager.List`'s signature is unchanged; on success paths the only behavioural change is that
  empty and churn now succeed. Additive schema. No BREAKING commit, so no e2e tier is mandated by the hard rule.
- **Rollback boundary:** one PR. Reverting restores the raw `Keys` call and the two substring branches and removes
  `FlowListResponse` on regeneration; stored bytes are untouched either way. Slices C and D do not depend on this
  revert beyond re-losing the preserved failure class.
