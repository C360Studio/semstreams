# Documentation audit evidence inventory

base: ea22e6a4e75d12bf7f6050c6d8121de1d089b191

Three SemStreams explorers collected onboarding, graph/agentic, and operations/contributor observations for #1302.
The root session materialized selected source pins and separately checked navigation, issue coverage, the logging
and gated-DAG examples, and the rule-watch disagreement. The claim commit adds only an audit scope record.

These are source observations, not executed snippets, runtime tests, model calls, or whole-page correctness seals.
The corpus and mechanical method are recorded in [methods.md](methods.md); the collectors' searches and coverage
limits are in [searches.md](searches.md). Agreement between a guide and a spec does not establish code conformance.

## Learning paths and retirement labels

- `docs/README.md:78` — `- [Workflow Quickstart](basics/08-workflow-quickstart.md) - Get started with workflows`
- `docs/README.md:79` — `- [Workflow Configuration](advanced/09-workflow-configuration.md) - Complete schema reference`
- `docs/README.md:86` — `- [Native one-shot lifecycle migration target][native-lifecycle] - **PENDING** until the simplify runtime and proof`
- `docs/basics/07-agentic-quickstart.md:386` — `- [Workflow Quickstart](08-workflow-quickstart.md) - Multi-step workflow orchestration`
- `docs/basics/08-workflow-quickstart.md:3` — `> **Legacy guide.** This page describes the retired `processor/reactive``
- `docs/basics/08-workflow-quickstart.md:67` — `"github.com/c360studio/semstreams/processor/reactive"`
- `docs/advanced/09-workflow-configuration.md:3` — `> **Legacy reference.** This page describes the retired`
- `docs/advanced/10-reactive-workflows.md:3` — `> **Legacy reference.** This page describes the retired`
- `docs/concepts/23-parallel-agents.md:3` — `> **Legacy pattern guide.** This page documents the older`
- `docs/concepts/14-orchestration-layers.md:3` — `semstreams has two orchestration layers — **rules** and **components**.`
- `docs/concepts/14-orchestration-layers.md:11` — `This document is the canonical "how we do workflows in semstreams"`
- `docs/concepts/22-context-construction.md:180` — `"context": "${steps.build_context.output}"`
- `docs/concepts/22-context-construction.md:254` — `- [Workflow Configuration](../advanced/09-workflow-configuration.md) - Workflow processor`

## Onboarding and package examples

- `README.md:71` — `task dev:start`
- `README.md:97` — `You should see your sensor entity ID under `data.entitiesByPrefix.entities`. If`
- `taskfiles/dev.yml:56` — `- go build -o bin/semstreams ./cmd/semstreams`
- `taskfiles/dev.yml:84` — `- ./bin/semstreams --config configs/hello-world.json`
- `configs/hello-world.json:97` — `"name": "iot_sensor",`
- `cmd/semstreams/main.go:536` — `if err := componentregistry.Register(componentRegistry); err != nil {`
- `cmd/e2e-semstreams/main.go:409` — `if err := iotsensor.RegisterPayloads(reg); err != nil {`
- `cmd/e2e-semstreams/main.go:912` — `if err := iotsensor.Register(registry); err != nil {`
- `component/registry.go:268` — `msg := fmt.Errorf("unknown component factory '%s'", config.Name)`
- `docs/basics/05-first-processor.md:170` — `err := payloadregistry.Register(&payloadregistry.Registration{`
- `docs/basics/05-first-processor.md:1356` — `- [ ] Payload registered in init() with payloadregistry.Register()`
- `payloadregistry/registry.go:98` — `func (pr *Registry) Register(registration *Registration) error {`
- `examples/processors/iot_sensor/payload.go:106` — `func RegisterPayloads(reg *payloadregistry.Registry) error {`
- `docs/concepts/15-payload-registry.md:75` — `function. There is no `init()` and no package-level singleton — the registry is an explicit instance`
- `docs/basics/05-first-processor.md:1170` — `{Name: "input", Type: "nats", Subject: "raw.sensor.>"},`
- `component/ports.go:53` — `type PortDefinition struct {`
- `component/ports.go:64` — `Config   Portable `json:"config" schema:"editable,type:object,description:Typed semantic port configuration"``
- `component/README.md:216` — `return registry.RegisterWithConfig(component.RegistrationConfig{`
- `component/registry.go:171` — `if registration.Ports == nil {`
- `component/registry.go:173` — `fmt.Errorf("factory %q declares no ports: set Registration.Ports to the pure port declarer the constructor derives its ports from", name),`
- `examples/processors/iot_sensor/register.go:19` — `Ports:       DeclarePorts,`
- `message/README.md:20` — `import "github.com/c360/semstreams/message"`
- `go.mod:1` — `module github.com/c360studio/semstreams`
- `message/README.md:85` — `msg := message.NewBaseMessage(payload)`
- `message/base_message.go:121` — `func NewBaseMessage(msgType Type, payload Payload, source string, opts ...Option) *BaseMessage {`
- `message/README.md:97` — `err := json.Unmarshal(data, &msg)`
- `message/base_message.go:296` — `fmt.Errorf("no payload registry configured; use message.NewDecoder(reg).Decode(data)"),`
- `message/decoder.go:33` — `func NewDecoder(reg *payloadregistry.Registry) *Decoder {`

## Agent examples and evidence interpretation

- `docs/basics/07-agentic-quickstart.md:166` — `"type": "publish_agent",`
- `docs/basics/07-agentic-quickstart.md:169` — `"prompt": "Temperature anomaly detected. Analyze sensor {{.EntityID}}..."`
- `processor/rule/actions.go:1688` — `return errors.New("subject is required for publish_agent action")`
- `processor/rule/execution_context.go:291` — `result = strings.ReplaceAll(result, "$entity.id", ec.EntityID)`
- `docs/basics/07-agentic-quickstart.md:302` — `json.Unmarshal([]byte(call.Arguments), &args)`
- `agentic/tools.go:210` — `Arguments map[string]any `json:"arguments,omitempty"``
- `docs/concepts/22-context-construction.md:49` — `- **Exact token budgets** - Know size before dispatch`
- `pkg/context/tokens.go:23` — `return len(s) / DefaultCharsPerToken`
- `docs/concepts/13-agentic-systems.md:222` — `A trajectory captures the complete execution path of an agentic loop:`
- `docs/concepts/13-agentic-systems.md:435` — `Trajectories are stored in NATS KV (`AGENT_TRAJECTORIES`) on loop completion:`
- `docs/concepts/13-agentic-systems.md:439` — `│ Key: <loop-uuid>                            │`
- `openspec/specs/agentic-loop/spec.md:234` — `v1.<base32-sha256(loop_id)>.<attempt_id>`
- `openspec/specs/agentic-loop/spec.md:480` — `No response SHALL return or imply complete, partial, unknown-completeness, fully captured, gap free, or an equivalent`
- `docs/advanced/08-agentic-components.md:753` — `Every response says `coverage: observed`. A terminal fact is an observation, not a seal or completeness proof.`
- `processor/agentic-loop/README.md:298` — `Stores one immutable `TrajectoryFactV1` observation per attempt. The bucket uses history 1 and no TTL; append-only`

## Operations and contributor APIs

- `component/registry.go:44` — `// (e.g., restart when a named runtime dependency changes). Kept as`

- `docs/operations/05-model-registry.md:12` — `and hot-reloads at runtime — components that depend on it auto-restart`
- `docs/operations/05-model-registry.md:13` — `when the key changes.`
- `docs/operations/05-model-registry.md:167` — `| **Components in flow configs** | The `agentic-loop`, `agentic-model`, `agentic-dispatch`, `graph-query`, `graph-embedding` factories | Auto-restarted by ComponentManager when their factory declares `component.DepModelRegistry` |`
- `component/registry.go:47` — `// DepModelRegistry signals that a component consumes`
- `component/registry.go:50` — `// Later model_registry writes require process restart.`
- `openspec/specs/framework-composition/spec.md:317` — `The composition root SHALL construct ComponentManager from one read of the existing configuration. ComponentManager`
- `openspec/specs/framework-composition/spec.md:334` — `#### Scenario: Later model-registry write waits for a later process`
- `docs/operations/23-natsclient-test-helpers.md:104` — `require.NoError(t, component.Stop(5*time.Second))`
- `component/lifecycle.go:63` — `type LifecycleComponent interface {`
- `component/lifecycle.go:67` — `Stop(ctx context.Context) error`
- `docs/operations/migration-restore-go-lifecycle-ownership.md:30` — `Both `component.LifecycleComponent` and `service.Service` use `Stop(context.Context) error`. There is no duration`
- `docs/operations/migration-restart-safe-nats-client.md:5` — `The landed migration removes SemStreams lifecycle state that duplicated native Go and NATS ownership. It deletes`
- `docs/contributing/03-schema-generation.md:109` — `URL     string `schema:"type:string,desc:Target endpoint URL"``
- `component/schema_tags.go:254` — `case "description":`
- `component/schema_tags.go:309` — `fmt.Errorf("unknown directive: %s", key),`
- `docs/contributing/03-schema-generation.md:164` — `Factory: func() (component.Component, error) {`
- `component/registry.go:31` — `type Factory func(rawConfig json.RawMessage, deps Dependencies) (Discoverable, error)`
- `component/registry.go:62` — `Schema       ConfigSchema `json:"schema"`       // Schema as static metadata (Feature 011)`
- `component/registry.go:64` — `Ports        PortDeclarer `json:"-"`            // Static port declaration (not serializable)`
- `docs/contributing/01-testing.md:369` — `go test -bench=. -benchmem ./processor/graph/clustering/...`
- `README.md:267` — `- **Go 1.25+** — [Download](https://go.dev/dl/)`
- `go.mod:3` — `go 1.26.3`
- `configs/deployments/README.md:25` — `**`production-small.json`** - Small edge device (2GB RAM, 2 cores)`
- `configs/deployments/README.md:28` — `- Aggressive reconnection for intermittent connectivity`
- `configs/deployments/production-small.json:19` — `"max_memory_mb": 512,`
- `configs/deployments/production-small.json:20` — `"max_cpu_percent": 50`

## Package reference and spec disagreements

- `processor/rule/docs/entity-watching.md:115` — `| Authoritative `WatchAll` guard | Validates `ENTITY_STATES` values and advances the revision barrier |`
- `processor/rule/docs/entity-watching.md:120` — `Pattern bootstrap waits for the authoritative guard and bypasses coalescing so recovery semantics keep`
- `processor/rule/entity_watcher.go:24` — `// Contract validation rides the pattern-watch input path itself: every value a`
- `processor/rule/entity_watcher.go:28` — `// dedicated ENTITY_STATES contract-guard watcher — with zero configured`
- `processor/rule/entity_watcher.go:34` — `if len(bucketPatterns) == 0 {`
- `openspec/specs/rule-entity-watching/spec.md:22` — `The rule processor MUST validate the complete authoritative `ENTITY_STATES` watch before pattern-specific bootstrap`
- `pkg/logging/doc.go:42` — `// Publishes log records to NATS subjects in the format logs.{source}.{level}.`
- `pkg/logging/nats_handler.go:88` — `subject := fmt.Sprintf("logs.%s.%s", r.Level.String(), source)`
- `processor/gated-dag/doc.go:82` — `// Marker and edge predicates are FREE-FORM: graph-ingest validates only the`
- `processor/gated-dag/config.go:424` — `if err := vocabulary.RequireDeclaredPredicate(val); err != nil {`
- `graph/embedding/README.md:60` — `embedder := embedding.NewBM25Embedder(corpus)`
- `graph/embedding/bm25_embedder.go:87` — `func NewBM25Embedder(cfg BM25Config) *BM25Embedder {`
- `graph/clustering/README.md:115` — `provider := clustering.NewQueryManagerGraphProvider(queryManager)`
- `graph/clustering/provider.go:46` — `func NewQueryManagerProvider(qm RelationshipQuerier) *QueryManagerProvider {`
- `openspec/specs/fusion/spec.md:4` — `TBD - created by archiving change fusion-per-facet-edges. Update Purpose after archive.`

## Navigation and document authority

- `doc.go:1` — `// Package semstreams provides a stream processor that builds semantic knowledge graphs`
- `doc.go:12` — `//   - Offline-capable: NATS JetStream provides local persistence and sync`
- `docs/operations/27-framework-package-boundary-clean-break.md:3` — `**Historical cutover evidence — not an active release procedure.**`
- `docs/proposals/semsource-walkthrough/README.md:6` — `The [worked chapter](../../basics/09-building-semsource.md) is the adopter-facing document;`
- `docs/proposals/semsource-walkthrough/README.md:7` — `[inventory.md](inventory.md) records the bounded source investigation.`

- `docs/basics/04-vocabulary.md:377` — `See [Vocabulary Package](../../vocabulary/README.md) for the full API reference and [Vocabulary Documentation](../vocabulary/) for architecture guides.`
- `docs/concepts/25-phased-agentic-chains.md:296` — `- [ADR-039: Tool-Call Governance via Rules](../adr/039-rule-driven-tool-governance.md) — the per-role tool-allowlist primitive`
- `docs/contributing/03-schema-generation.md:864` — `- **[SCHEMA_TAGS_GUIDE.md](./SCHEMA_TAGS_GUIDE.md)** - Complete reference for writing schema tags`
- `docs/contributing/03-schema-generation.md:867` — `- **[OPENAPI_INTEGRATION.md](./OPENAPI_INTEGRATION.md)** - OpenAPI spec generation and usage`
- `docs/ROADMAP.md:3` — `## Alpha Blockers`
- `docs/ROADMAP.md:5` — `Items requiring completion before alpha release.`
- `docs/advanced/05-index-reference.md:17` — `*Indexes exist and are populated. Graph providers for clustering integration are a future enhancement (see [Roadmap](../ROADMAP.md)).`
- `openspec/project.md:73` — `- **`openspec/specs/<capability>/spec.md` — current truth.** Requirement +`
- `openspec/project.md:81` — `- **`docs/adr/` — genuine decisions only.** Irreversible choices and cross-repo`
- `openspec/project.md:85` — `- **`docs/0X-*.md` — retired gradually.** "How it works" content migrates into`
- `docs/adr/README.md:37` — `Every ADR already in this directory is preserved untouched. Many predate OpenSpec`
- `.agents/protocol.md:11` — `| What is wanted, what kind, is it decided | GitHub issue + labels (`type:` / `area:` / `class:` / `status:` / `horizon:`) | `status:needs-decision` is the owner's docket; a ruling is posted as an issue comment and the label removed. `status:blocked` names its blocker in a comment. |`
- `.agents/protocol.md:12` — `| What gates the next tag | GitHub milestone named for the intended version | Membership is the gate: in or out; an unruled item is out. `horizon:pre-v1` means before v1.0.0, not before the next tag. |`

## Instruction overlap and ownership

- `.claude/commands/opsx/archive.md:34` — `**If any artifacts are not `done`:**`
- `.claude/commands/opsx/archive.md:37` — `- Proceed if user confirms`
- `.claude/commands/opsx/archive.md:45` — `**If incomplete tasks found:**`
- `.claude/commands/opsx/archive.md:48` — `- Proceed if user confirms`

- `AGENTS.md:7` — `- Go 1.25 + NATS JetStream (KV, ObjectStore)`
- `CLAUDE.md:28` — `- Go 1.25 + NATS JetStream (KV, ObjectStore)`
- `AGENTS.md:145` — `## Key Packages`
- `CLAUDE.md:196` — `## Key Packages`
- `.agents/protocol.md:3` — `Canonical. `CLAUDE.md` and `AGENTS.md` carry a pointer to this file plus the three gates (claim, merge, close) inline;`
- `.agents/README.md:3` — `The files in `contracts/` are the tracked, platform-neutral behavioral authority. Platform adapters are intentionally`
- `.agents/README.md:37` — `Canonical skills live in `skills/`. Read the relevant `SKILL.md` fully; the platform adapters add discovery`
- `.agents/README.md:56` — `The remaining Claude tools (OpenSpec workflows, `e2e-doctor`, `tag-release`) stay platform-specific. Their`
- `.claude/skills/openspec-archive-change/SKILL.md:7` — `author: openspec`
- `.claude/skills/openspec-archive-change/SKILL.md:9` — `generatedBy: "1.5.0"`
- `.claude/commands/opsx/archive.md:156` — `- Don't block archive on warnings - just inform and confirm`
- `docs/contributing/06-openspec-change-discipline.md:49` — `Before archive:`
- `docs/contributing/06-openspec-change-discipline.md:57` — `If those statements are not all true, the change is not ready to archive.`

## Adopter seam inventory

| Reader | What current material asks them to know | Default consequence | Where they learn |
| --- | --- | --- | --- |
| Go builder | Registry lifetime, port grammar, factory composition, module/toolchain version | Copied forms disagree with signatures or admission checks | Compile, boot or decoder error |
| Agent application author | Rule subject, variable substitution and tool argument representation | Copying the quickstart reaches a required-field error or invalid Go conversion | Compile or action-validation error |
| Context consumer | Estimated tokens and the limits of observed trajectory evidence | Several guides promise more precision or completeness than current contracts | Conflicting docs/spec; no completeness proof |
| Operator/test author | Whether configuration restarts components and which Stop API applies | Runtime instructions conflict with boot selection; examples use a duration overload | Compiler error for Stop; docs for restart requirement |
| Contributor with an agent | Canonical, generated and intentionally inline instruction ownership | Maintained copies overlap; generic archive warnings differ from project completion gates | Conflicting instruction text |

The needed domain choices belong to the adopter. Framework-owned composition, decoder and lifecycle facts should
be discoverable through current interfaces and checked examples, rather than reconstructed from conflicting prose.
No new exported runtime or coordination primitive is proposed by the audit; a collision-table design is not triggered.

## Existing instance of the problem shape

This is a document-authority problem: multiple maintained explanations carry the same framework fact.
The existing shared-role arrangement in `.agents/README.md:3` and shared skills at `.agents/README.md:37`
already separate canonical content from thin discovery adapters. The protocol at `.agents/protocol.md:3`
explicitly preserves three inline gates. Those are current instances of consolidation with intentional repetition;
there is no evidence here that a new documentation system or runtime primitive is needed.

## Counterexamples and unproven claims

Only the introductory identity/edge range in root `doc.go` was inspected; its body was not fully audited.
The explicit historical-cutover banner and separate SemSource chapter/evidence record already demonstrate
current-versus-historical authority separation.

All four workflow pages already have legacy banners. The issue is their continued presentation in current learning
paths and incompatible how-to bodies, not absence of retirement labels. The inspected Query Access sections agree
with the limited HTTP/MCP contracts; the integration-runner guidance agrees with CI. The SemSource walkthrough
identifies its version and evidence limits. The ten platform role adapters are already thin.

Constructor selection alone does not disprove runtime BM25 fallback. ObjectStore history and complete propagation
of context metadata through aliases were not verified. No runtime benchmark substantiates the resource figures in
the deployment README. Exact overlapping text is not automatically dispensable content.

The generic rule README was not the #765 target. The separate entity-watching guide and current capability spec
describe a dedicated authoritative guard that the inspected implementation explicitly excludes. That disagreement
needs contract reconciliation; a docs audit does not decide which runtime guarantee to change.

## Adjacent claims

- #457 and #486 — legacy workflow guidance and misleading parallel/fan-out interpretation.
- #668 — gated-DAG predicate guidance; current validation uses RequireDeclaredPredicate.
- #765 — a dedicated authoritative WatchAll guard described in entity-watching docs.
- #1002 — logging package docs reverse the emitted subject segments.
- #1136 — GraphRAG attribution/retrieval claims; its owner ruling includes a wire change, not docs alone.
- #1218 — conflicting lifecycle sentinels, distinct from the duration-versus-context example error.
- #828 — ADR rationale and later supersession; follow its owned decision process and preserve history.
- #1260 — relationship/property guidance; useful decision support, not redundant material to remove wholesale.
- #1034 — the fusion spec's placeholder Purpose.
- #1163 — product-boundary wording about SemDev/SemSpec; the issue records an owner ruling.
- #1133 — two type-authority wording/anchor residues, not broad payload-onboarding coverage.
- #1234 / PR #1298 — separate issue-pattern audit; its claimed files are outside this edit scope.
- PR #1254 owns auth inventory; #1156/#1159 own settlement/restart work; #1141 owns HTTP read work.
  Their proposed behavior is not counted as shipped main behavior here.

## Searches

See [searches.md](searches.md) for the collector records and read limits, and [methods.md](methods.md) for the
reproducible full-corpus mechanical pass. Failed filename guesses and truncated displays never establish absence.
Root additionally read 12 existing issue bodies and captured 258 open issue summaries with placement metadata.
No external URL availability, live sister state, runtime behavior, or every exported Go comment was audited.
