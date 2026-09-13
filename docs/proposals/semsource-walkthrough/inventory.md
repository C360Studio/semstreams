# SemSource walkthrough inventory

base: bb08a2f29d174d85bac3c845bce5c07c9130ea92
sister-base: 4093d3ce421371f4a99d7168e372552899bf6795
issue: https://github.com/C360Studio/semstreams/issues/1299

## Scope and measurements

This inventory covers explanatory documentation for source-to-context composition, identity, capability names,
product profiles, and introductory links. It proposes no runtime, configuration, API, CI, or spec change.
SemSource is a read-only example at the recorded commit and SemStreams beta.160; its adoption of current main is unproven.
The observations below are source/document evidence. No builds, services, or sister mutations informed this inventory.
A baseline diff against 8e41e46f2dfd757e9350fe27f2004b00c40388b4 found no changes to the inherited runtime evidence.
The bounded scope does not re-enumerate every framework caller, downstream product, or historic review finding.
The architect supplied the inventory; the root session materialized it and added the explorer's document-body trace.
After inventory review, README and index pins were refreshed for the explanatory edits. The original README wording
is preserved at the declared base; the two current README pins describe the edited introduction.

## Framework surface inventory

- `README.md:3` — `> A Go framework for explicit semantic context and observable actions.`
- `README.md:24` — `- **Local dependencies** — keep storage and selected model services at the site when disconnected operation matters`
- `openspec/project.md:14` — `SemStreams is a **framework, not a product**. It owns primitives and contracts;`
- `openspec/project.md:45` — `- **Products own their domain semantics and compose SemStreams primitives.**`
- `openspec/project.md:47` — `- **SemSource** — source discovery/parsing, binary/media by-reference, source`
- `docs/basics/01-what-is-semstreams.md:15` — `The core opinion: **users should decide what to enable**. SemStreams provides the building blocks; you compose them based on your constraints and requirements.`
- `docs/concepts/13-agentic-systems.md:7` — `The agentic components are an **optional extension** to SemStreams. The core system — event ingestion,`
- `docs/README.md:68` — `- [Agentic Quickstart](basics/07-agentic-quickstart.md) - Get started with agents`
- `docs/README.md:54` — `- [Query Access](concepts/11-query-access.md) - HTTP facade, typed adapters, and MCP status`
- `docs/concepts/11-query-access.md:43` — `No general graph front door currently serves every caller. The remote endpoint is`
- `docs/basics/06-configuration.md:38` — `- **Validate before you boot**: `composition.Validate` over the configuration document, or the`
- `docs/concepts/00-real-time-inference.md:93` — `Tiers define the embedding **method**—not what data gets embedded. Embeddings require text content.`
- `docs/concepts/00-real-time-inference.md:97` — `| **0** | Structural | Disabled | None | No |`
- `docs/concepts/00-real-time-inference.md:98` — `| **1** | Statistical | BM25 (pure Go) | Lexical (term matching) | Yes |`
- `docs/concepts/00-real-time-inference.md:99` — `| **2** | Semantic | Neural (external) | Semantic (meaning matching) | Yes |`
- `pkg/fusion/doc.go:3` — `// GraphQueryClient, plus the product-supplied Lens SPI (lens.go). It is a`
- `pkg/fusion/doc.go:9` — `// result in their own payload types; products supply a Lens.`
- `openspec/specs/fusion/spec.md:158` — `### Requirement: The projection reports view-revision observations, never a coherence claim`
- `openspec/specs/fusion/spec.md:189` — `### Requirement: Unhydrated seeds are reported, never inferable as absent`
- `openspec/specs/fusion/spec.md:238` — `### Requirement: A Miss licenses no absence claim`
- `openspec/specs/fusion/spec.md:278` — `### Requirement: Body hydration failure is reported per node, never silent`
- `openspec/specs/agentic-tools/spec.md:230` — `### Requirement: Effect metadata is descriptive and does not alter execution control`
- `docs/basics/07-agentic-quickstart.md:147` — `A tool named in `approval_required` is not executed when the model calls it.`

## Pinned SemSource evidence

Paths in this table belong to SemSource. The immutable links supply their baseline identity; they are checked
against that Git object separately from the framework's local inventory verifier.

| Surface | Evidence at the pinned version |
| --- | --- |
| Framework dependency | [SemStreams v1.0.0-beta.160](https://github.com/C360Studio/semsource/blob/4093d3ce421371f4a99d7168e372552899bf6795/go.mod#L8) |
| Component composition | [Registers framework and product factories](https://github.com/C360Studio/semsource/blob/4093d3ce421371f4a99d7168e372552899bf6795/cmd/semsource/run.go#L264) |
| Payload composition | [Explicit instance registry](https://github.com/C360Studio/semsource/blob/4093d3ce421371f4a99d7168e372552899bf6795/cmd/semsource/run.go#L278) |
| Document parsing | [Reads bytes, splits passages and offloads passage bodies](https://github.com/C360Studio/semsource/blob/4093d3ce421371f4a99d7168e372552899bf6795/handler/doc/entities.go#L289) |
| Document facts | [Path, title, ordinal, parent, section and body handles](https://github.com/C360Studio/semsource/blob/4093d3ce421371f4a99d7168e372552899bf6795/handler/doc/entities.go#L174) |
| Body reference | [Content hash, store.Put and StorageReference](https://github.com/C360Studio/semsource/blob/4093d3ce421371f4a99d7168e372552899bf6795/handler/doc/entities.go#L356) |
| Graphable envelope | [EntityID, Triples and StorageRef methods](https://github.com/C360Studio/semsource/blob/4093d3ce421371f4a99d7168e372552899bf6795/graph/event_payload.go#L57) |
| Ingest publication | [BaseMessage then PublishToStream](https://github.com/C360Studio/semsource/blob/4093d3ce421371f4a99d7168e372552899bf6795/internal/entitypub/publisher.go#L335) |
| Framework graph composition | [Graph ingest subject and ENTITY_STATES output](https://github.com/C360Studio/semsource/blob/4093d3ce421371f4a99d7168e372552899bf6795/cmd/semsource/run.go#L746) |
| Product lens | [Converts body predicates to StorageReference](https://github.com/C360Studio/semsource/blob/4093d3ce421371f4a99d7168e372552899bf6795/source/fusion/lens/docs/docs.go#L111) |
| Framework fusion | [NewEngine with product retrieval and body resolver](https://github.com/C360Studio/semsource/blob/4093d3ce421371f4a99d7168e372552899bf6795/processor/code-context/component.go#L209) |
| Product context API | [Code and document context HTTP operations](https://github.com/C360Studio/semsource/blob/4093d3ce421371f4a99d7168e372552899bf6795/README.md#L474) |
| Product readiness | [Older readiness/absence prose; conflicts with current fusion contract](https://github.com/C360Studio/semsource/blob/4093d3ce421371f4a99d7168e372552899bf6795/processor/source-manifest/readiness.go#L7) |
| Product identity | [Older segment order; not current ADR-102 guidance](https://github.com/C360Studio/semsource/blob/4093d3ce421371f4a99d7168e372552899bf6795/entityid/entityid.go#L38) |
| Profile numbering | [Structural, Tier 0 statistical, Tier 1 semantic, Tier 2 instruct](https://github.com/C360Studio/semsource/blob/4093d3ce421371f4a99d7168e372552899bf6795/configs/tiers/README.md#L8) |
| Statistical profile | [BM25 embedder](https://github.com/C360Studio/semsource/blob/4093d3ce421371f4a99d7168e372552899bf6795/configs/tiers/tier0-statistical.json#L8) |
| Semantic endpoint | [Local HTTP model endpoint](https://github.com/C360Studio/semsource/blob/4093d3ce421371f4a99d7168e372552899bf6795/configs/tiers/tier1-semantic.json#L30) |
| Instruct limitation | [Wiring example, not a supported profile](https://github.com/C360Studio/semsource/blob/4093d3ce421371f4a99d7168e372552899bf6795/configs/tiers/README.md#L143) |
| Development overlay | [Separate development configuration enables clustering](https://github.com/C360Studio/semsource/blob/4093d3ce421371f4a99d7168e372552899bf6795/configs/tiers/tier2-compose-dev.json#L26) |

## Adopter seam inventory

### Agent-assisted Go builder

Must know framework/product ownership and explicit component and payload composition. Public identity and product
boundary differ in emphasis; pinned product code shows composition. Domain semantics and capability choices belong
to the builder. Framework-owned subjects, bucket details and startup ordering should be absorbed by composition;
describing the current product wiring does not endorse it as the ideal adopter burden.

### Agent consuming source context

Must understand relevant source/index readiness, body references and partial results. SemSource beta.160 readiness
prose differs from current framework miss semantics. Consumers need the evidence returned and its limits; a missing
result is not an absence proof. Sources, observed index progress and body resolution are distinct observations.

### Local deployment operator

Must select a product profile, endpoint placement, mounted paths and optional services. Product numbers differ from
framework tiers; the instruct wiring example is explicitly not a supported profile. Available resources and required
retrieval capability are intrinsic choices. No measured edge resource or disconnection guarantee was established here.

### Application policy author

Effects describe tools; configured controls decide admission and approval. Existing specs and quickstart describe
metadata and control separately. Applications decide when humans participate; runtime loops are optional.

## Adjacent claims and evidence limits

SemSource beta.160 readiness prose is not current SemStreams fusion truth; current misses license no absence claim.
Structural/statistical/semantic names identify capabilities; the repositories' numeric tier labels are not equivalent.
The older product entity-ID helper is not a template for current framework identity semantics in ADR-102.
The instruct wiring example and runnable development overlay have different flags and support claims.
Source-body references and fusion demonstrate an inspected pattern, not a guarantee that every entity has a body.
README offline/sync wording supplies no independently exercised disconnected-operation guarantee in this inventory.
No new exported runtime surface is proposed; the consumers at birth are the four document audiences above.
The problem shape is an explanatory map across existing contracts; Query Access already separates admitted access.
No same-class collision table is triggered: this scope establishes no durable or coordination primitive.
Issue #1299 owns this documentation task; existing runtime work and sister adoption stay outside it.

Independent documentation review additionally found a pinned production-path mismatch. SemSource's docs lens
at source/fusion/lens/docs/docs.go:50-61 routes source paths to prefix resolution and ordinary prose to NL resolution.
Its component uses fusionnats.New at processor/code-context/component.go:133-142. At the beta.160 framework commit
8403a2218000e45a31c5132fbfe01af42ed04f14, pkg/fusion/fusionnats/client.go:252-259 forwards the query unchanged;
lines 280-286 and 350-354 require an entity-ID prefix. The guide links both immutable sources and limits its exercise
to prose with BM25 or neural retrieval. This is a recorded adoption limitation, not a sister mutation or a fix claim.

## Searches

The architect refreshed these bounded searches and line-numbered reads. Limited results support only named facts.

- Framework: `rg --files docs | rg '(source|context|tier|inference|fusion|query-access|governed)'`.
- Framework: tracked literals `SemSource|semsource|body|ready|Graphable|fusion` in context-construction and ADR-062;
  first 35 matches read.
- Framework: `rg --files openspec/specs | rg '(fusion|graph-research|framework-composition)'`; fusion spec read fully.
- Framework: tracked fusion requirement headings `A Miss|Body hydration|The projection|Unhydrated`.
- Framework: diff from 8e41e46f2dfd757e9350fe27f2004b00c40388b4 over README, docs, specs and pkg/fusion.
- SemSource: `rg --files | rg '(README|ready|manifest|blob|body|config|main.go)'`; first 60 names inspected.
- SemSource: tracked README literals `fusion|readiness|readyz|body_ref|body.ref|manifest|storage_ref|semsource|stream`;
  first 65 matches read.
- SemSource: tracked fusion/body/store literals in source/fusion, ast-source, doc-source and code-context;
  first 70 matches read; attempted internal/run path did not establish absence.
- SemSource: tracked composition literals `Register|WireGraphRuntime|WithPayloadRegistry|NewComponentManager|Graph|Expand`
  in cmd/semsource/run.go; first 45 matches read.
- SemSource: tracked flags `enable_clustering|clustering_llm|embedder_type|query_classification|answer_synthesis|seminstruct`
  in the instruct wiring example and development overlay.

The explorer's bounded document trace additionally located and read handler/doc/entities.go, graph/event_payload.go,
internal/entitypub/entity_state.go, internal/entitypub/publisher.go, the docs lens, code-context and MCP query handlers.
Tracked literal groups included `EntityID|Triples|StorageRef|StorageReference|BodyStore|Put|publishEntity|Payload`,
`IngestEntityStates|bodyStore|DocBody|passage|bodyKey`, and `doc_context|code_context|callFusion|code.v1|docs.v1`.
Vocabulary search `DocBodyStore|DocBodyKey|DocChunkIndex|DocSection|DocFilePath|CodeBelongs` returned 58 lines;
entityid search `func Build|strings.Join|Sprintf|PlatformSemsource` returned 18 lines. Incorrect guessed paths
source/doc, source/predicates.go, source/doc.go and entityid/id.go were resolved through tracked searches.
No gopls, tests, live requests or current-framework compatibility proof was performed for this source inventory.

Review follow-up reads: SemSource docs lens lines 45-70, code-context lines 125-145, and docs lens test lines 56-76;
`git show v1.0.0-beta.160:pkg/fusion/fusionnats/client.go` (resolve/prefix validation) and engine_lens.go lines 152-153.
The docs lens test's fake resolver does not validate the production adapter's source-path interpretation.
