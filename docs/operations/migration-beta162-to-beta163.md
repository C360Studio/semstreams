# Migration notes — v1.0.0-beta.162 → v1.0.0-beta.163

SemStreams-owned record of what each downstream product must do to adopt the beta.163 wave. Sister repositories are
**read-only** to SemStreams agents (owner ruling on #1100, 2026-08-26, O-11/O-12): no issues, comments, or edits are made
there — every obligation is recorded here, at the sister's pinned SHA, and linked from the landing PR. One `##` section per
landing; later wave items (#1095 re-slot and reorder, gh606) append their own sections below.

Every `file:line` below was read at the named SHA on 2026-08-26 and re-read at SemStreams `origin/main` `7e7ea76e`.

## Single type authority (ADR-103)

### What changes on the wire

After this landing, graph-ingest rejects any `entity.create` whose `entity.message_type` is not registered in the receiving
binary's payload registry, with the closed outcome code `message_type_unregistered` (class invalid; detail `message_type` = the
key). Reads, `entity.reconcile`, `triple.append`, `entity.delete`, and entities already stored are unaffected; only new births
of an unknown type are refused. `projection.Contract` literals keep compiling (the contract types move to
`pkg/projection/contract` with aliases). A product that creates through `pkg/projection.MutationClient.Create` may omit
`entity.MessageType`: the client fills it from the bound contract (owner ruling O-17); a non-empty stamp that conflicts with
the contract is rejected. `projection.Contract.MessageType` is now the structured `message.Type` — on the wire
`{"domain":…,"category":…,"version":…}`, the same shape `EntityState.message_type` has — so a dotted-string literal
(`MessageType: "semmachina.campaign_entity.v1"` or `agentic.LoopExecutionMessageType().Key()`) no longer compiles and a
rule pack's `"message_type": "…"` no longer loads: write `MessageType: YourMessageType()` (the `message.Type` builder every
product already has) and `"message_type": {"domain": …, "category": …, "version": …}`. `Register` refuses a domain,
category, or version containing `.`. Registered framework entity types now validate their writer's full contract at
publication (`Validate()` gates `BaseMessage.MarshalJSON`). Two boot-time consequences for a composition root that builds its own graph-ingest: the
graph-ingest factory refuses to construct without a payload registry (`Dependencies.PayloadRegistry`), and with
`enable_hierarchy: true` it refuses to construct unless that registry holds `graph.hierarchy_container.v1` — both are
registered by `payloadbuiltins.Register`, so a root that calls it (semmachina, semdev do) sees no change. Full mechanics:
`openspec/specs/payload-registry/spec.md`, `openspec/specs/graph-ingest/spec.md`.

### The one obligation

Register every `message.Type` you stamp on `entity.create`, in the binary that hosts graph-ingest, with its ADR-054 floor and —
where you hold a birth contract — that contract. The pattern is the framework's own `storage/objectstore/stored_message.go:88-103`:

```go
// RegisterPayloads registers this product's graph-born types with the supplied
// registry. Called from the composition root after payloadbuiltins.Register.
func RegisterPayloads(reg *payloadregistry.Registry) error {
	return reg.Register(&payloadregistry.Registration{
		Factory:         func() any { return &CampaignEntity{} }, // a Graphable payload: EntityID(), Triples(), Schema(), Validate(), MarshalJSON
		Domain:          Domain,                                  // e.g. "semmachina"
		Category:        CategoryCampaignEntity,                  // e.g. "campaign_entity"
		Version:         SchemaVersion,                           // e.g. "v1"
		Description:     "Campaign entity born by the campaign gate",
		IndexingProfile: vocabulary.IndexingProfileControl,       // ADR-054 floor; "" = control + metered gap
		Contracts:       []contract.Contract{CampaignBirthContract()}, // the birth contract you hold today, if any
	})
}
```

Verification, in your own tree: a production-decoder round-trip test per type (marshal a fully populated entity, decode
through `message.NewDecoder(reg)`, assert the concrete type, `EntityID()`, and the predicate set of `Triples()`), plus one
`entity.create` against a beta.163 graph-ingest that returns `applied`.

### semmachina — pinned `841c45e`

- **Types stamped on `entity.create` (4):** `semmachina.campaign_entity.v1` (`internal/campaign/gate.go:32-36`, stamped `:395`),
  `semmachina.turn_state.v1` (`internal/turn/recorder.go:32-36`, stamped `:344`), `semmachina.knowledge_grant_entity.v1` and
  `semmachina.revelation_receipt_entity.v1` (`internal/projectioncontract/contracts.go:106-109`; stamped through
  `internal/knowledge/granter.go:282-284`). None is registered: `internal/payload/constants.go:63-147` records each as
  "deliberately NOT registered: no message of this type is ever published"; `internal/payload/registry.go:27-72` registers ten
  other categories. `internal/boot/components.go:51` also births lifecycle participants (`lifecycle.harness.v1`) — covered by
  `payloadbuiltins.Register`, which `cmd/semmachina/main.go:99` already calls.
- **Day one after upgrade:** every campaign, turn, knowledge-grant, and revelation birth is rejected `message_type_unregistered`;
  existing entities keep reading; lifecycle births keep working.
- **Obligation:** add the four to `payload.RegisterPayloads` (`internal/payload/registry.go`) with factories (Graphable payloads
  carrying the fields your triple builders read); floors: `campaign_entity` and `turn_state` `control` (machinery),
  `knowledge_grant_entity` and `revelation_receipt_entity` `content` if their text is retrieval-worthy, else `control`;
  contracts: the seven birth contracts in `internal/projectioncontract/contracts.go:77-109` bound to their types (a contract's
  `MessageType` may be left zero — `Register` fills it; where set, it is now `message.Type{…}`, not the dotted string at
  `contracts.go:106-109`). Invert the "deliberately unregistered" tests
  (`submitaction_test.go:364` and any sibling) — the rationale they pin is the one ADR-103 retires.
- **Verification:** four round-trip tests; `TestCategoryCampaignEntity_*` and friends assert registration instead of its absence.

### semdev — pinned `ca3956a`

- **Types stamped (2):** `semdev.intake_event.v1` (`internal/intake/record.go:63`, stamped through `graphown.Creator.Create` at
  `:91`), `semdev.standards_source.v1` (`internal/standards/sync.go:143`, stamped `:186`). Lessons use the framework's
  `agentic.agent_lesson.v1` (`internal/graphown/contracts.go:444`) — registered by the framework. The registry is built at
  `internal/boot/runtime.go:623-624` from `payloadbuiltins.Register` only. `runtime.go:537,656` and `boot.go:361` birth lifecycle
  participants — covered.
- **Day one:** intake records and standards sources are rejected at birth; lessons and lifecycle births keep working.
- **Obligation:** a `RegisterPayloads` in `internal/intake` and `internal/standards` (or one in `internal/graphown`) called from
  `runtime.go:624` after the builtins; floors: `intake_event` `control`, `standards_source` `content`; contracts: the graphown
  contracts for those two (`internal/graphown/contracts.go`) bound to the types. The structured contract type also breaks
  compilation where a contract literal sets a dotted string: `internal/graphown/contracts.go:444` and
  `test/conformance/standards_contracts_test.go:102-103` — use the `message.Type` builder instead of `.Key()`.
- **Verification:** two round-trip tests; `graphown.Creator.Create` against beta.163 returns `applied`.

### semconnect — pinned `d0d06e0`

- **Types stamped (11):** `c360.csapi-{system,datastream,procedure,deployment,sampling-feature,property,control-stream,command,system-event,feasibility,schema-artifact}.v1`
  (`gateway/cs-api/projection_contracts.go:29-39` — those vars are already structured `message.Type`s and are fine; the
  compile breaks are the contract literals that flatten them, `projection_contracts.go:45,60` and
  `projection_contracts_test.go:26` — carry the `message.Type` value instead of `.Key()`), stamped at
  `gateway/cs-api/graph_mutations.go:159`. semconnect holds **no
  registry**: `cmd/cs-api-server` calls neither `payloadbuiltins.Register` nor `payloadregistry.New`; its one registration
  (`message/oms/register.go:16-22`, OMS observation) is exported for a host. The 11 stamps reach the **host's** graph-ingest.
- **Day one:** every CS-API resource birth is rejected by the host's graph-ingest.
- **Obligation:** export `RegisterPayloads` from `gateway/cs-api` for the 11 types (floor `content` — the value its contracts
  already declare at `projection_contracts.go:44-64`; contracts: `representationContract`/`birthOnlyContract` bound to each
  type), and have the **host composition root** call it after `payloadbuiltins.Register`. `message/oms.RegisterPayloads` is the
  in-tree model for the shape.
- **Verification:** round-trip tests in `gateway/cs-api`; one host-side boot that registers both and creates a system resource.
- **Ruled (#1114, 2026-08-27, option (a)):** semconnect builds its own composition root — the pattern semmachina
  (`cmd/semmachina/main.go:98-99`), semdev (`internal/boot/runtime.go:623-624`), semteams (`cmd/semteams/main.go:872-873`)
  and semsource (`cmd/semsource/run.go:279-280`) already follow: a `cmd/<binary>/main.go` that builds
  `payloadregistry.New()`, calls `payloadbuiltins.Register(reg)`, then `csapi.RegisterPayloads(reg)` (the export above),
  injects `reg` through `component.Dependencies.PayloadRegistry`, and hosts graph-ingest itself. The framework binary gains
  no registration seam; the unmodified `cmd/semstreams` image in `semconnect/deploy/compose.yml:19-49` and
  `conformance/compose.yml:55-73` cannot host CS-API births after this tag. Sisters stay read-only: this is the
  instruction, not a change made for semconnect.

### semteams — pinned `8a70b7e7`

- **Types stamped:** none of its own on `entity.create`. Its contracts re-declare the framework's loop-execution and lesson
  contract structure with the framework's key builders (`cmd/semteams/main.go:971,998`); lifecycle births through `Manager`
  (`cmd/semteams/flowtemplates/loader.go:200`) carry `lifecycle.harness.v1` — registered by `payloadbuiltins.Register`. Own
  registered types: `research/artifact.go:253`, `devviaspec/plan.go:148`, `semsource/payload.go:39-69`.
- **Day one:** nothing is rejected.
- **Obligation:** the two re-declared contracts set `MessageType` from `.Key()` (`cmd/semteams/main.go:971,998`), which no
  longer compiles against the structured field — replace them with `agentic.LoopExecutionContract()` and
  `agentic.LessonContract()` (the contracts now live beside their types; `openspec/specs/agentic-lessons/spec.md` "External
  lesson composition uses the framework-owned contract snapshot").
- **Verification:** boot; the loop-execution and lesson contracts validate unchanged.

### semmem — pinned `b909cbf` — for downstream-owner validation

The local tree is pre-rename and stale: `go.mod:1` declares module `github.com/c360/semmem` and `go.mod:6,10` replace
`github.com/c360/semstreams` (the old import path) with `../semstreams`; it does not build against
`github.com/c360studio/semstreams`. Its `EntityState` shape (`entity/types.go:180-192`: `Edges`, `ObjectRef`, `MessageType string`)
predates ADR-091. It stamps nine `semmem.entity.{spec,doc,issue,discussion,discussion_comment,pr,pr_review,code,decision}.v1`
strings (`entity/types.go:187-365`, `processor/{spec,docs,decision}/processor.go:240,247,332,357`) and registers none.

- **The finding that opened #1100** (a lesson had no payload type at all) is closed by this landing: `agentic.agent_lesson.v1` is
  a registered Graphable payload with a factory, so a lesson can arrive on a fact-lane or import-lane input as itself.
- **Obligation when semmem rejoins the current module path:** register its nine types with floors (`content` for prose-bearing
  spec/doc/discussion/decision, `control` for the rest unless retrieval-worthy) and any birth contracts it adopts. The federation
  MVP document this issue cites is not in any local tree; the downstream owner validates this section.

### Not affected

- **semsource** (`4093d3c`): births on the fact lane as the registered `semsource.entity.v1` (`graph/event_payload.go:22-31`);
  mutation-lane use is `Reconcile` only (`processor/supersession/lifecycle.go:303,325`) — no registration obligation.
  ONE compile fix from the structured contract type: `graph/contract.go:71` sets `MessageType: EntityType.Key()` in a
  `projection.Contract` literal; write `MessageType: EntityType` (it is already a `message.Type`).
- **semdragon** (`07f4de9`, pinned to beta.135): writes through the pre-ADR-091 subject and directly to KV
  (`questdag/unit.go:673`, `graphclient.go:79-95`); already off the current mutation surface. `questdag.unit.v1` and the dynamic
  `semdragons.<event>.v1` keys become obligations only if it returns to the typed mutation API.

## Composition validation substrate (ADR-100) — `GET <components>/gaps` is removed

### What changes on the wire

`GET <components>/gaps` — the operation key `/gaps` in the generated document, served at `/components/gaps` because
ComponentManager mounts under the `components` prefix (`service/service_manager.go:1683-1686` maps the service name
`component-manager` to the URL prefix `components`) — is **removed without an alias**,
together with its response body (`disconnected_nodes`, `orphaned_ports`, `objectstore_gaps`, and the `summary` object
carrying `total_gaps` / `critical_gaps` / `optional_gaps` / `critical_port_count` / `has_issues`). The route 404s and the
operation is gone from `specs/openapi.v3.yaml`. Its Go surface goes with it:
`ComponentManager.ValidateFlowConnectivity`, `ComponentManager.DetectObjectStoreGaps`, and the `ComponentGap` type.

The operation was a **second composition judgment**. It re-derived the connection graph on demand and applied its own
severity table, which disagreed with the canonical one: an input declared `"external": true` — fed from outside the
composition, an operator statement introduced in this same landing — is not an orphan to the canonical validator, while
`/gaps` reported it as `no_publishers`, `required=true`, `critical_port_count=1`, `has_issues=true`. ADR-100 D3 admits one
validator and one findings vocabulary, so the second judgment is retired rather than re-projected.

### What replaces it

| You wanted | Use |
|---|---|
| The findings for the composition that is running | `GET <components>/validate` — the `composition.Result` ComponentManager retained at boot, verbatim (`status`, `errors`, `warnings`, `graph`). Boot now **refuses** on an error finding, so a running process has none. |
| The connection graph of the composition that is running | `GET <components>/flowgraph` (JSON, or `?format=mermaid`) |
| The findings for a composition before you boot it | The `validate <config-path>` verb (`composition/cli.Main`, wired into `semstreams`; `--validate` reports the same findings), or `composition.Validate(catalog, cfg)` in Go |
| The same judgment in a test | `composition.AssertValid(t, catalog, cfg)` |
| The same judgment from an agent | The read-only tools `validate_composition` and `composition_graph`, under the existing `component_catalog` gate |

The finding types `disconnected_node` and `orphaned_port` survive by name in the canonical vocabulary; a required stream
input with no publisher is an `orphaned_port` **error** there (severity, not a separate "critical" count), and an input
declared `"external": true` raises no such finding. `GET <components>/paths` (reachability from input components) is
unchanged — it is a projection, not a judgment.

### Downstream action

Measured across the local sister checkouts on 2026-08-26 (`grep -rn "gaps"` over `*.ts`, `*.tsx`, `*.svelte`, `*.go`,
`*.py`, excluding generated files): **no repository calls the operation from hand-written code.** Every hit is a
generated OpenAPI type — `semstreams-ui/src/lib/types/api.generated.ts:703`, `semteams/ui/src/lib/types/api.generated.ts:715`,
`semspec/ui/src/lib/types/{api,semstreams}.generated.ts:600,715`, `semdragon/ui/src/lib/api/generated.d.ts:91` and
`semdragon/ui/static/openapi.json:58`.

- **Every UI repository**: regenerate types from the new `specs/openapi.v3.yaml`. The `/gaps`
  operation key disappears; nothing else in the component surface changes shape except `/flowgraph`
  and `/validate`, which gain typed schemas (`Graph`, `Result`).
- **Anyone who added a call since that measurement**: replace a `has_issues` check with `result.status !== "valid"` from
  `<components>/validate`, and a `critical_port_count` check with `result.errors.length`. Do not reimplement the severity
  rule client-side — that is the defect this landing removes.
- **Anyone whose component has an input fed from outside the composition** (a UI, a peer process, a rule action): declare
  `"external": true` on that input port, and restate it in any named override (a named merge is a complete replacement).
  Without it, boot refuses with `orphaned_port … no_publishers` and prints the one-line remedy.
