# Inventory: R1 registered completion projection and optional research boundary

base: fd9dac345bf2099d634f4f30b372cb0a44730882
comparison-parent: 417beae5552f8f15ad3540edd7d8504c87174c13
worktree: /Users/coby/Code/c360/semstreams-wt/codex/gh1146-agentic-loop-restart
dirty-source-manifest: /private/tmp/gh1146-validator-resume.9SLJyk/final-source.sha256
dirty-source-manifest-sha256: 5d8e4d7ba397ef62fb7e0aa0ff843e29b376121cbe83af5e2dc20b71c619b0b0

## Scope and checkpoint

R1 introduced a core-dispatch dependency on optional research and a registered-decode → payload-marshal →
private shadow-projection path. This inventory identifies the existing owners and seams needed to assess a
bounded correction while preserving the accepted mixed activity view. It does not select an API or alter
research completion ownership.

The parent independently reports 61/61 frozen source entries matching and no source movement. The current
HEAD contains documentation reconciliation, not a new runtime checkpoint. The broad wire inventory remains
immutable historical evidence at `/private/tmp/gh1146-dispatch-final.Bdd1Kx/wire-contract-inventory.md`.
Its independent refresh found 498 pins: 494 unchanged, four moved in the dispatch delta, zero content drift.
This narrower checkpoint does not adopt that inventory's unrelated audit scope or claim its omissions closed.

Previously completed full active-artifact/current-spec/ADR reads were reused. This refresh read the complete
reconciliation diff for proposal/design/dispatch delta, all 165 current task lines, the canonical architect
contract, orchestration-check, its complete 622-line concept reference, and ADR-103. Source inspection was
limited to this boundary and its already-identified producer, reader, registry, and projection seams.

## Claimed gap and dependency ownership

- `componentregistry/register.go:20` — `agenticdispatch "github.com/c360studio/semstreams/processor/agentic-dispatch"`
- `test/fixtures/corecomposition/imports.go:8` — `_ "github.com/c360studio/semstreams/componentregistry"`
- `test/fixtures/corecomposition/imports.go:9` — `_ "github.com/c360studio/semstreams/payloadbuiltins"`
- `test/contract/core_composition_deps_test.go:17` — `"/agentic/research",`
- `test/contract/core_composition_deps_test.go:25` — `t.Errorf("core dependency closure contains forbidden package fragment %q", forbidden)`
- `processor/agentic-dispatch/loop_wire.go:9` — `"github.com/c360studio/semstreams/agentic/research"`
- `processor/agentic-dispatch/http_activity.go:15` — `"github.com/c360studio/semstreams/agentic/research"`
- `openspec/specs/framework-composition/spec.md:34` — `Core, graph-research, and optional-adapter composition MUST use separate Go import roots. Importing core registration`

The concrete import is introduced by the R1 mixed-view implementation, not required by the core composition
contract. A graph-ingest or core-composition test reaching research through core registration is evidence of
the transitive dependency; it is not evidence that graph-ingest should understand SearchResult.

## Stored facts, current interpreters, and validation

- `processor/agentic-dispatch/loop_wire.go:112` — `type completionWire struct {`
- `processor/agentic-dispatch/loop_wire.go:133` — `if err := json.Unmarshal(raw, &w); err != nil {`
- `processor/agentic-dispatch/loop_wire.go:150` — `if err := json.Unmarshal(raw, payload); err != nil || payload.Validate() != nil {`
- `processor/agentic-dispatch/loop_wire.go:187` — `base, err := c.decoder.Decode(raw)`
- `processor/agentic-dispatch/loop_wire.go:194` — `if result, ok := base.Payload().(*research.SearchResult); ok {`
- `processor/agentic-dispatch/loop_wire.go:208` — `payload, err := json.Marshal(base.Payload())`
- `message/decoder.go:46` — `if err := json.Unmarshal(data, msg); err != nil {`
- `message/base_message.go:195` — `if err := m.payload.Validate(); err != nil {`
- `message/base_message.go:224` — `if err := m.Validate(); err != nil {`
- `message/base_message.go:301` — `payload := m.registry.Create(m.msgType.Domain, m.msgType.Category, m.msgType.Version)`
- `message/base_message.go:311` — `if err := json.Unmarshal(wire.Payload, msgPayload); err != nil {`

The current completion reader distinguishes fixed private raw terminal records from registered envelopes.
The former are decoded through the private union and then validated as a concrete ordinary terminal payload.
The latter are reconstructed by the registry and explicitly validated. SearchResult is then asserted
concretely. Other registered terminals are decoded again by agentterminal, serialized back to bytes, and
read through the private union.

Registry reconstruction and payload validation are separate operations: Decoder.Decode does not itself call
Payload.Validate. Current explicit validation is an obligation to preserve, not something supplied implicitly
by successful JSON decoding. This inventory does not authorize removing the private raw-KV family or migrating
every persisted record into an envelope.

## Research producer and optional composition

- `agentic/research/register.go:34` — `Factory:         func() any { return &SearchResult{} },`
- `agentic/research/result.go:45` — `Evidence []fusion.Evidence `json:"evidence,omitempty"``
- `agentic/research/result.go:50` — `Synthesis string `json:"synthesis"``
- `agentic/research/result.go:55` — `DecompTrace *DecompTrace `json:"decomp_trace,omitempty"``
- `agentic/research/result.go:60` — `TokensUsed int `json:"tokens_used,omitempty"``
- `agentic/research/result.go:67` — `Iterations int `json:"iterations,omitempty"``
- `agentic/research/result.go:84` — `func (p *SearchResult) Validate() error {`
- `processor/research-graph-synthesize/component.go:480` — `envelope := message.NewBaseMessage(result.Schema(), result, ComponentName)`
- `processor/research-graph-synthesize/component.go:490` — `if err := c.loops.PutSnapshot(ctx, loopID, envelopeBytes); err != nil {`
- `processor/research-graph-synthesize/component.go:496` — `if err := c.loops.PutSearchResult(ctx, loopID, envelopeBytes); err != nil {`
- `processor/research-graph-synthesize/component.go:512` — `if err := c.loops.PutLoopCompletion(ctx, loopID, envelopeBytes); err != nil {`
- `frameworkcapabilities/graphresearch/register.go:477` — `return research.RegisterPayloads(registry)`
- `cmd/semstreams/main.go:791` — `if graphresearch.Selected(cfg) {`
- `cmd/semstreams/main.go:792` — `if err := graphresearch.RegisterPayloads(reg); err != nil {`
- `cmd/e2e-semstreams/main.go:424` — `if err := graphresearch.RegisterPayloads(reg); err != nil {`

SearchResult already has its own registered, rich wire representation. The producer writes that same envelope
to snapshot, per-stage result, and COMPLETE_ locations. Rendering a small activity DTO neither replaces nor
justifies weakening that representation. Research has no payload LoopID, TaskID, terminal timestamp, or outcome;
the current accepted activity mapping obtains identity from the canonical COMPLETE_ key.

The eleven existing intermediate namespaces are declared together in research/constants.go: request received;
classify complete/snapshot; route complete/snapshot; execute complete/snapshot; assess complete/snapshot;
search-result complete; synthesize snapshot. Dispatch currently imports their concrete owner to exclude them.
This is a second coupling in addition to asserting SearchResult; fixing only the payload assertion leaves it.

- `agentic/research/constants.go:104` — `RequestReceivedPrefix = "research.request.received."`
- `agentic/research/constants.go:106` — `ClassifyCompletePrefix = "classify.complete."`
- `agentic/research/constants.go:108` — `ClassifySnapshotPrefix = "classify.snapshot."`
- `agentic/research/constants.go:110` — `RouteCompletePrefix = "route.complete."`
- `agentic/research/constants.go:112` — `RouteSnapshotPrefix = "route.snapshot."`
- `agentic/research/constants.go:114` — `ExecuteCompletePrefix = "execute.complete."`
- `agentic/research/constants.go:116` — `ExecuteSnapshotPrefix = "execute.snapshot."`
- `agentic/research/constants.go:118` — `AssessCompletePrefix = "assess.complete."`
- `agentic/research/constants.go:120` — `AssessSnapshotPrefix = "assess.snapshot."`
- `agentic/research/constants.go:122` — `SearchResultCompletePrefix = "search_result.complete."`
- `agentic/research/constants.go:124` — `SynthesizeSnapshotPrefix = "synthesize.snapshot."`

## Accepted consumers and invariants

- `openspec/changes/agentic-loop-restart-safety/specs/agentic-dispatch/spec.md:291` — `Bare canonical LoopID keys SHALL validate as `LoopEntity` with key/ID equality. `COMPLETE_<canonical LoopID>` SHALL`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-dispatch/spec.md:292` — `validate by completion family and remain activity-only. Known research namespaces SHALL be ignored as non-loop`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-dispatch/spec.md:293` — `records. Every other key SHALL poison as malformed would-be loop state.`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-dispatch/spec.md:296` — `suffix supplies its activity identity. Its aggregate `TokensUsed` SHALL NOT populate directional Loop token fields.`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-dispatch/spec.md:298` — `Current-loop and unknown-key poison SHALL disable AutoContinue and authoritative listing until a greater-revision`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-dispatch/spec.md:306` — `- **AND** synthesis, success, complete state, and iterations project through the existing Loop activity shape`
- `processor/agentic-dispatch/http_activity.go:329` — `if !strings.HasPrefix(key, completeKeyPrefix) {`
- `processor/agentic-dispatch/http_activity.go:364` — `if record.entity == nil || (userID != "" && record.entity.UserID != userID) {`

Activity, current listing, and AutoContinue share one view but do not share authority semantics. COMPLETE_ is
activity-only. Current listing and selection use the bare LoopEntity, not a completion-derived replacement.
Completion poison remains observable without becoming current-loop authority poison. Current/unknown-key poison
blocks authority conveniences until healing. Deletion, purge, expiry, readiness, revision, and immutable DTO
obligations remain in R1; none is removed by correcting type ownership.

Ordinary terminal ancestry must survive too. completionWire carries ParentLoopID from the `parent_loop` JSON
field. The existing agentterminal.Event has no ParentLoopID member, although it carries RunID and RunEntityID.
Replacing one projection with the other unchanged would therefore lose an existing activity field.

No new exported symbol, port, subject, bucket, or configuration field is proposed at this inventory checkpoint.
Present consumers are the existing dispatch activity/list/selection paths and their HTTP/SSE callers, not
hypothetical future readers.

## Existing problem-shape instances

The bounded shape is: reconstruct a registered type, validate it, expose an explicitly admitted read projection;
classify stored keys as included, excluded, or poison without acquiring the producer's state ownership.

- `pkg/graphview/view.go:38` — `type DecodeFunc[T any] func(key string, value []byte, meta EntryMeta) (decoded T, keep bool, err error)`
- `pkg/graphview/view.go:625` — `val, keep, err := v.decode(key, entry.Value(), EntryMeta{Revision: rev, Created: created})`
- `pkg/graphview/view.go:630` — `case !keep:`
- `message/rule_readable.go:64` — `RuleFields() map[string]any`
- `internal/agentterminal/terminal.go:64` — `// Event is the normalized internal projection shared by dispatch, AgentRun,`
- `internal/agentterminal/terminal.go:65` — `// and OTel. The source payload remains authoritative; this is not a wire type.`
- `internal/agentterminal/terminal.go:119` — `if err := base.Payload().Validate(); err != nil {`
- `internal/agentterminal/terminal.go:124` — `switch payload := base.Payload().(type) {`
- `internal/agentterminal/terminal.go:184` — `return Event{}, reject(ReasonPayload, "payload %T is not a loop terminal event", base.Payload())`
- `payloadregistry/registry.go:71` — `Contracts []contract.Contract `json:"-"``
- `pkg/projection/contract/contract.go:40` — `// Contract declares the graph shape emitted by one projection. It validates`

Graphview already provides the include/exclude/poison callback shape. Message behavioral interfaces already let
a typed payload declare an optional capability discovered through type assertion. RuleReadable is an existing
payload-owned projection example, but deliberately excludes free-form result content and is not an activity
serializer. agentterminal already owns a validated ordinary-terminal internal projection, but its closed family
and ancestry omission matter. Registry Contracts describe graph mutation shape, not activity DTO serialization.

The structural searches found no ready-made generic completion/activity behavioral interface in the named
surface. They did find existing specific projections and generic graphview decoding. This is not a claim that
every framework callback or every external package was exhaustively searched.

No durable, communication, or runtime-coordination primitive is proposed; the same-class collision-table trigger
has not fired. No establishing-pattern adoption sweep is claimed complete.

## Separate inherited defect and ownership boundary

- `frameworkcapabilities/graphresearch/executor.go:248` — `loopEntity := agentic.NewLoopEntity(loopID, "", research.PipelineRole, "", persistedIntent.MaxIterations)`
- `frameworkcapabilities/graphresearch/executor.go:249` — `loopEntity.State = agentic.LoopStateExecuting`
- `agentic/events.go:98` — `if e.TaskID == "" {`
- `processor/agentic-tools/loop_result.go:132` — `var event agentic.LoopCompletedEvent`
- `processor/agentic-tools/loop_result.go:133` — `if err := json.Unmarshal(entry.Value, &event); err != nil {`
- `processor/agentic-tools/loop_result.go:141` — `total := len(event.Result)`

The inherited research path creates a taskless executing LoopEntity, writes a registered SearchResult, and does
not perform the ordinary loop's terminal-state/event sequence. read_loop_result instead reads COMPLETE_ as raw
LoopCompletedEvent. Prior codec-only reproduction confirmed that a valid SearchResult envelope can yield a
successful raw unmarshal with empty ordinary result fields; that reproduction was not end-to-end readback proof.

Issue #1288 owns that inherited mismatch. It does not authorize inventing TaskID, relaxing LoopCompletedEvent
validation, promoting research activity into bare current authority, adding another terminal publication, or
transferring this introduced R1 dependency regression elsewhere.

Orchestration-check was applied. Its exclusive producer-state ownership and operational-artifact distinction
keep this investigation at the read boundary. A changed completion owner, rule path, lifecycle, or publication
sequence would be a separate design question, not an implied rendering fix.

## Adopter seam inventory

For a developer composing a core SemStreams binary, the existing contract is to register core components and
payloads, then explicitly select research only when wanted. Doing nothing beyond core composition must not pull
research into the dependency closure. The current regression is discoverable in the framework's closure test,
not prevented by the caller's core import. The developer should not need to understand dispatch's research
rendering implementation or compensate for it.

For a developer composing graph research, the existing obligations are explicit capability selection and
payload registration before decoding. Unregistered envelope decoding fails; it must not become raw-field
fallback. The developer should not need to teach core dispatch a concrete research type or repeat its eleven
private key prefixes merely to retain the supported activity view.

For an HTTP/SSE dashboard author, existing Loop activity fields and current LoopInfo retain their documented
meanings. Dropping research completion, ancestry, or readiness/error behavior would be a wire-semantic change,
even if Go still compiled. Prior SemTeams inventory found generic activity projection consumption; this refresh
does not assert its current checkout or perform a new sister-repository sweep.

These seams concern framework-owned type identity and observed stored values. They do not require callers to
predict retention, deadlines, storage limits, readiness, or a new correlation identity. No additional adopter
knob has been justified.

## Evidence limits and adjacent claims

R1 remains decision-held. The current combined gate stopped at core dependency closure; later full unit and
integration stages did not run. Existing focused mixed-view checks are checkpoint evidence, not final acceptance.

Adjacent authority remains: framework-composition's separate import roots; ADR-103's single type authority;
ADR-079 poison honesty; ADR-081 one shared decode-once view; the existing dispatch delta's mixed-key contract;
#1288 inherited research readback/lifecycle; #1158 broader registered-subject enforcement; #1112 graph-ingest
validation; #1244 later transition design. Parent live checks found no new owner ruling changing R1 and no
overlapping claim on this correction.

Unknown until reviewed design and implementation proof: the smallest concrete correction spanning both optional
type projection and namespace classification; its exact public/private surface cost; complete projection parity
across raw and enveloped ordinary terminals; and final core plus selected-research validation. No option is
selected by this inventory.

## Refresh searches and reads

All repository commands ran read-only in the named worktree. No tests, Docker runs, source/spec writes, or Git
mutations were performed.

1. Read `.agents/contracts/semstreams-architect.md` completely.
2. Read `.agents/skills/orchestration-check/SKILL.md` and its complete required
   `docs/concepts/14-orchestration-layers.md`; read ADR-103 completely.
3. `git diff fd9dac345^ fd9dac345 -- openspec/changes/agentic-loop-restart-safety/proposal.md
   openspec/changes/agentic-loop-restart-safety/design.md
   openspec/changes/agentic-loop-restart-safety/specs/agentic-dispatch/spec.md`;
   read all current tasks.
4. Located broad inventory sections with `rg -n '^##|^###'`; reused bounded claimed-gap, registration,
   problem-shape, carrier-pairing, and adjacent-claim evidence. One combined historical excerpt was truncated;
   no completeness assertion is based on that truncated output.
5. Read loop_wire.go 1–225, http_activity.go 1–185 and 318–385, message behaviors/rule-readable, decoder 35–54,
   registry 1–82, agentterminal 18–190, graphview 24–41 and 609–640, and projection contract 1–115.
6. `gopls workspace_symbol -matcher=fuzzy 'Projection'` and the same command for `Completion`.
   Both used GOPROXY/GOSUMDB off and the existing task-local GOCACHE/GOPLSCACHE.
7. `git grep -n -E 'type (Registration|Registry|Metadata|DecodeFunc)|Projector|Projection|ProjectionValidator|Factory|GraphMutation'
   -- payloadregistry message component/registry.go`.
8. `rg --files agentic/research pkg/graphview pkg/contract`; pkg/contract was absent.
   Initial guessed search_result.go, kv_keys.go, and graphview/types.go paths were also absent; actual files were
   resolved before making claims.
9. `git grep -n -E 'SearchResult|Prefix|type DecodeFunc|if !keep' -- agentic/research pkg/graphview | head -95`;
   this bounded locator returned the result, namespace, and graphview homes subsequently read.
10. Read research/result.go 37–118, synthesize/component.go 472–535, graphresearch/executor.go 242–254,
    register.go 468–480, events.go 89–103, loop_result.go 108–151, and dispatch loop_projection_test.go 1–145.
11. `git grep -n -E 'agentic-dispatch|componentregistry|payloadbuiltins' -- componentregistry
    test/fixtures/corecomposition`; read closure test 1–48 and production composition 783–798.
12. `git grep -n -E 'research|optional' -- openspec/specs/framework-composition/spec.md`;
    read refreshed dispatch delta 283–357. Prior complete spec reads remain the baseline authority.

Stop for independent INVENTORY review.
