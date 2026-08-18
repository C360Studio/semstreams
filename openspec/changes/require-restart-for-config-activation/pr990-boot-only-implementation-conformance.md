# PR #990 boot-only implementation conformance

## Checkpoint identity

- Branch: `codex/pr990-boot-only-cutover`
- Reconstruction baseline: `58bf0d5252ccea70e4b8021a95ccdea3acdf1982`
- Worktree state: uncommitted implementation; file-and-line anchors below describe this checkpoint only.
- Binding disposition SHA-256:
  `40b2534b604a14f64aacbb8f4db86bdbc38129f3f114e0ac40118c9f7259fc41`
- Passed inventory SHA-256:
  `5256057932030c7e854a3889ae2756fbec577870ee5e5c9c7c0e8ab86874541d`
- Status: `IMPLEMENTATION CONFORMANCE PENDING INDEPENDENT REVIEW`
- Credit: zero lifecycle, proof, release, archive, or tag-readiness credit.

This ledger survives task compaction. It is not a completion claim. Before integration, every `PENDING` row must gain
final-commit evidence and every `BLOCKED` row must be resolved or carry the owner's explicit signed deviation.

## Evidence anchors

- `C01`: `service/component_manager.go:153-167` captures one existing-config value during construction.
- `C02`: `service/component_manager.go:267-340` constructs enabled boot components and seals Registry.
- `C03`: `service/component_manager.go:352-355` states and implements no drain/reconcile/post-boot Start lane.
- `C04`: `service/component_manager.go:797-807` retains the exact construction config and concrete handle.
- `C05`: `service/component_manager.go:881-899` supplies captured security/model dependencies to factories.
- `C06`: `service/component_manager_http.go:555-599` exposes boot config as GET-only value observation.
- `C07`: `service/component_manager_boot_only_integration_test.go:20-123` proves later component and model writes do
  not change runtime membership, identity, config, or factory dependency identity.
- `R01`: `component/registry.go:100-161` stores declarations without lifecycle state or live handles.
- `R02`: `component/registry.go:219-261` is the internal-token-gated boot admission seam.
- `R03`: `component/registry.go:330-360` admits once by name and seals.
- `R04`: `component/registry.go:683-730` implements the single and complete snapshot reads; clone functions begin at
  `component/registry.go:748`, `:758`, `:772`, and `:780` for declarations, ports, fact slices, and nested fact values.
  Stable file
  SHA-256: `43cc8ecf6b8fb2a07041bb7c3ea426d80637ea8237a304a7ec61b54b4b9a898f`.
- `R05`: `component/registry_boot_admission_test.go:12-89` mutates and rereads both snapshot forms, including nested
  JetStream subjects, projected facts, stream facts, and exclusive resources; `:61-64` rechecks one-time capture.
  Tests at `:108-177` prove no handle/replacement API, no partial admission, complete snapshots, and seal rejection.
  Stable file
  SHA-256: `979c369a6ab47ba9b81746331f9dd51a01d6528441834b2428798e007880a575`.
- `F01`: `flowstore/flow.go:11-30` contains authoring and audit data without runtime lifecycle fields.
- `F02`: `flowstore/manager.go:48-160` validates and persists authoring CRUD.
- `F03`: `engine/engine.go:18-20` declares validator/compiler ownership without lifecycle.
- `F04`: `engine/engine.go:49-112` preserves validation and produces detached component candidates.
- `F05`: `service/flow_service.go:161-176` routes CRUD, validate, explicit publication, and observations only.
- `F06`: `service/flow_service.go:195-212` documents authoring-only and name-keyed observation semantics.
- `F07`: `service/flow_service.go:250-305` implements Flow CRUD only.
- `F08`: `service/flow_service.go:308-339` defines the private progress response and compile-before-publish path.
- `F09`: `service/flow_service.go:341-411` sorts, prevalidates, writes sequentially, observes each write, and reports
  exact progress plus reboot requirement.
- `F10`: `service/flow_service.go:414-430` preserves saved-or-draft validation.
- `F11`: `service/flow_publish_test.go:46-194` proves prevalidation, observation failure, partial progress, retry, and
  reboot reporting.
- `F12`: `service/flow_service_test.go:90-143` proves CRUD does not publish and explicit retry uses real Config Manager.
- `F13`: `service/flow_surface_test.go:12-69` proves lifecycle routes absent and CRUD schemas retained.
- `T01`: `processor/agentic-tools/executors/flows.go:16-22` limits the tool dependency to authoring CRUD.
- `T02`: `processor/agentic-tools/executors/flows.go:36-133` exposes and dispatches only five Flow CRUD tools.
- `P01`: `config/manager.go:174-232` makes foreign platform identity a fatal Start error before arbitration, watches,
  or writes. `config/manager.go:634`, `config/manager.go:693`, and `config/manager.go:715` retain ordinary component
  delete, put, and push behavior with detached no-op branches removed.
- `P07`: `config/manager_integration_test.go:568-646` proves the real-NATS mismatch before and after complete
  key/value/revision snapshots. `internal/bootstrapobservability/config_manager_integration_test.go:18` proves
  `StartValidatedConfigManager` propagates that failure.
- `P02`: `internal/lifecyclejoin/generation.go:27-99` is preserved lifecyclejoin behavior; the path has no diff.
- `P03`: `service/component_manager.go:355-730` retains the existing boot Start/Stop owner mechanics after removal of
  the dynamic configuration lanes. This requires independent diff review before credit.
- `P04`: `model/watch.go:12` is the preserved model watcher surface; production `model/**` has no diff.
- `P05`: production `processor/rule/**` has no diff; separate Rule target-state specs remain unchanged.
- `P06`: production `natsclient/**`, ACK handling, CronScheduler, and `test/e2e/client/websocket.go` have no diff.
- `A01`: owner allowed the one-line test-only census correction at
  `natsclient/consumer_policy_callsite_test.go:60` after removal of `service/flow_runtime_stream.go`.
- `A02`: owner approved fatal Config Manager Start on foreign shared-bucket platform identity, overriding the
  byte-for-byte Config Manager preservation ruling only for this prerequisite.
- `A03`: `internal/maxdelivery/boot_order_test.go:19` owns the binary boot-order proof. Its E2E branch at `:64-73`
  requires `completeE2EPhaseA` before Registry, ServiceManager, and service construction; `:90-96` requires the E2E
  phase to connect, run `StartValidatedConfigManager`, ensure streams, and only then enter steady logging; `:279-285`
  proves the named setup functions actually construct Registry/ServiceManager and configure services. Stable file
  SHA-256: `f2d770bd75b3926dbde37a0c99d32cc35b22cc58ad859ecf4bbb0b91418c40a7`.
- `M01`: `docs/operations/migration-boot-only-flow-activation.md` owns adopter migration truth.
- `O01`: strict OpenSpec validation is required at the final checkpoint; no durable verification artifact is attached
  yet.

## Resolved owner-authorized deviation

### DEV-01: foreign identity must fail before publication can exist

The original Config Manager returned nil without writing while detached. Post-write value observation could detect a
missing or different value, but not a detached no-op when the local value already equaled the request. Exact persisted
names were therefore impossible without exposing a new status knob or write receipt.

The owner approved the narrower prerequisite in `A02`: foreign shared-bucket platform identity is a fatal Config
Manager Start error. `P01` and `P07` implement and prove the failure before arbitration, watchers, or writes; `A03`
proves the E2E root completes validated Config Manager startup before dependent construction. No detached running
publisher exists, and ordinary publication still observes each successful write.

This is an explicit owner-authorized deviation from the original byte-for-byte Config Manager preservation ruling. It
does not authorize any other Config Manager redesign and remains pending independent reviewer confirmation.

## Ruling registry

### Boot composition

- `B01`: ComponentManager reads existing configuration once during construction.
- `B02`: ComponentManager composes one fixed enabled component set.
- `B03`: no component or model-registry configuration subscription remains.
- `B04`: later writes do not mutate runtime component identity, membership, or config.
- `B05`: generic runtime component-config PUT and `watch_config` retire.
- `B06`: Registry admits boot declarations and then seals.
- `B07`: Registry exposes defensive declaration values and no live handle.
- `B08`: ComponentManager solely owns concrete runtime handles.
- `B09`: replacement reservation, runtime replacement identity, removal transition, and same-instance mutation retire.

### Flow authoring

- `W01`: Flow remains authoring CRUD plus existing validation and compilation.
- `W02`: save/update changes only flowstore.
- `W03`: explicit publication validates, compiles, sorts names, and uses the existing Config Manager write.
- `W04`: publication is upsert-only and omission never deletes.
- `W05`: partial failure reports exact persisted names and failed name; retry is safe.
- `W06`: success reports unchanged runtime and required reboot.
- `W07`: Flow lifecycle state, routes, tools, telemetry, logs, and streams retire without aliases.
- `W08`: flowstore makes no runtime lifecycle or current-membership claim.

### Rule, preserved surfaces, and exclusions

- `Q01`: Rule code, storage, watchers, and current behavior remain unchanged.
- `Q02`: separate Rule hot-reload target state is not advanced or credited.
- `S01`: ordinary Config Manager behavior remains; foreign identity fatal Start is the sole owner-approved exception.
- `S02`: model watcher and model-registry behavior remain unchanged.
- `S03`: validator construction and existing factory behavior remain unchanged.
- `S04`: lifecyclejoin and owner Start/Stop mechanics remain unchanged.
- `S05`: ACK, consumer, NATS shutdown, and recovery mechanics remain unchanged.
- `S06`: CronScheduler and E2E WebSocket behavior remain unchanged.
- `D01`: multi-key configuration atomicity remains a deferred finding.
- `D02`: partial watcher creation/arbitration remains a deferred finding.
- `D03`: validator constructor effects remain a deferred finding.
- `X01`: no workflow-run monitor or Flow-monitor replacement is introduced.
- `X02`: no boot comparison/provenance or new Flow state model is introduced.
- `X03`: no new storage, subject, bucket, registry, lifecycle wrapper, or coordination protocol is introduced.
- `X04`: no compatibility alias or parallel retired path is introduced.
- `X05`: no sister repository is mutated.
- `X06`: only accepted production territory is changed; unexpected paths require owner review.

## Conformance map

| ID | Evidence | Status |
|---|---|---|
| B01 | `C01` | PASS |
| B02 | `C02`, `C04` | PASS |
| B03 | `C03`, `C07` | PASS |
| B04 | `C07` | PASS |
| B05 | `C06`; `service/component_manager_boot_only_test.go:10-38` | PASS |
| B06 | `R02`, `R03`, `R05` | PASS |
| B07 | `R01`, `R04`, `R05` | PASS |
| B08 | `C04`; `service/service_manager.go:1093-1112` | PASS |
| B09 | `R01`, `R05`; `service/component_manager_boot_only_test.go:20-38` | PASS |
| W01 | `F01`-`F04`, `F07`, `F10` | PASS |
| W02 | `F02`, `F07`, `F12` | PASS |
| W03 | `F05`, `F08`, `F09`, `F12`, `A02`, `P07`, `A03` | PASS pending review |
| W04 | `F09`, `F11` | PASS |
| W05 | `F09`, `F11`, `F12`, `A02`, `P07`, `A03` | PASS pending review |
| W06 | `F08`, `F09`, `F11`, `F12` | PASS |
| W07 | `F05`, `F13`, `T01`, `T02` | PASS |
| W08 | `F01`, `F06` | PASS |
| Q01 | `P05` | PENDING final diff evidence |
| Q02 | active Rule specs unchanged; `proposal.md` and `tasks.md` | PASS |
| S01 | `P01`, `P07`, `A02`, `A03` | OWNER-APPROVED DEVIATION; review PENDING |
| S02 | `P04` | PENDING final diff evidence |
| S03 | `engine/validator.go`; factory census unchanged | PENDING independent review |
| S04 | `P02`, `P03` | PENDING independent review |
| S05 | `P06`, `A01` | PENDING independent review |
| S06 | `P06` | PENDING final diff evidence |
| D01 | `design.md` D8 | PASS as deferred only |
| D02 | `design.md` D8 | PASS as deferred only |
| D03 | `design.md` D8 | PASS as deferred only |
| X01 | `F05`, `F13`, `T02` | PASS |
| X02 | `F01`, `F06` | PASS |
| X03 | current diff inventory | PENDING independent review |
| X04 | `F05`, `F13`, `T02` | PENDING migration review |
| X05 | repository boundary; no sister-repository action | PASS |
| X06 | current diff inventory plus `A01` | PENDING independent review |

## Verification map

| Gate | Evidence | Status |
|---|---|---|
| Boot set | `C02`, `C07` | UNVERIFIED/PENDING — test source exists; no durable run artifact attached |
| Later writes | `C07` | UNVERIFIED/PENDING — integration source exists; no durable run artifact attached |
| Registry seal/value-only | `R05`; `component/registry_integration_test.go:56-153` | UNVERIFIED/PENDING — integration source exists; no durable run artifact attached |
| Flow CRUD no-publish | `F12` | UNVERIFIED/PENDING — test source exists; no durable run artifact attached |
| Validation/compile | `F04`, `F10`; `engine/compile_test.go` | UNVERIFIED/PENDING — test source exists; no durable run artifact attached |
| Publication ordering/progress | `F11`, `F12`, `A02`, `P07`, `A03` | UNVERIFIED/PENDING — race/integration source exists; no durable run artifact attached |
| Retired surfaces | `F13`, `T02` | UNVERIFIED/PENDING — test source exists; no durable run artifact attached |
| Preserved-surface diff | `P01`-`P06`, `A01` | PENDING independent review |
| Repository compile | all packages | UNVERIFIED/PENDING — no durable run artifact attached |
| Focused integration/race | `config`, bootstrap observability, `service` | UNVERIFIED/PENDING — no durable run artifact attached |
| Lint | `task lint` | UNVERIFIED/PENDING — no durable run artifact attached |
| Schema generation/no drift | `task schema:generate`; schema diff | UNVERIFIED/PENDING — no durable run artifact attached |
| Full repository race/contracts | final commands | PENDING |
| Core/CRUD E2E | final commands | PENDING |
| Strict OpenSpec | `O01` | UNVERIFIED/PENDING — no durable final-checkpoint artifact attached |

## Completion rule

The sole semantic deviation has explicit owner authority and focused proof, but implementation conformance still needs
independent review, the exact final commit, and remaining gates. Only then may task 1.4 be checked. This change still
receives no lifecycle, controlled-restart, dirty-recovery, effect-before-ACK, release, archive, or tag credit.
