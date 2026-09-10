# Tasks: honor-predicate-datatype (#1267)

The owner's INVENTORY PASS was granted 2026-09-09 (PR #1269 comment 5603956923); implementation is unblocked and the
remaining hold is section 10. Task 1 was deliberately first: the issue marked the `"5.0"^^xsd:double` consequence
**inferred from the code path, not reproduced**, and a design premise that has never been observed is a hypothesis.
Nothing else started until it was a failing assertion.

## 0. Gate — CLEARED 2026-09-09

- [x] 0.1 GATE CLEARED. Q1–Q7 ruled by the owner 2026-09-09 (#1267 comment 5601338922) and carried by the design,
      delta, tasks and ADR-107. **INVENTORY PASS granted 2026-09-09** (PR #1269 comment 5603956923). The reproduced
      `^^<@id>` defect is filed as **#1272** at owner direction and is closed by this PR alongside #1142 (Q7).
      Implementation is unblocked; no hold remains on this change.
- [x] 0.2 Note for the implementing session: the two consequences task 1 exists to prove were **already reproduced
      empirically** on 2026-09-07 (throwaway in-package probe, removed; evidence recorded on PR #1269). Turtle emits
      `"acme.ops.gcs.robotics.drone.002"^^<@id>` — a relative-reference datatype IRI, invalid RDF — and
      `"5.0"^^xsd:double` for a declared `int` after the JSON round trip. Task 1 therefore commits a fixture for
      known behavior; it is not an open investigation. Assert on `^^<@id>`, not `^^@id`: the serializer wraps the
      datatype in angle brackets, and a probe asserting the unwrapped form PASSES while the defect is present.

## 1. Prove the two consequences before changing anything

- [x] 1.1 Add `vocabulary/export/datatype_roundtrip_test.go`: build a `graph.EntityState` whose triple has
      `Object: 5` under a predicate registered `WithDataType("int")`, pass it through `graph.MarshalEntityState` and
      `graph.UnmarshalEntityState`, assert the decoded `Object` is `float64(5)`, then `export.SerializeToString` to
      N-Triples and assert the emitted literal is today's `"5.0"^^<http://www.w3.org/2001/XMLSchema#double>`. This is
      the whole issue in one fixture; it proves the JSON-round-trip half, which is the load-bearing half, not just the
      classifier half.
- [x] 1.2 In the same file, assert the second, larger consequence found at design time: a triple carrying
      `Datatype: message.EntityReferenceDatatype` (`"@id"`) whose object is a canonical entity ID currently serializes
      as a LITERAL with datatype `@id` — not an IRI, and not valid RDF. `@id` is validated at the authoritative
      persistence seam (`graph/entity_predicate_contract.go:168`), so this is reachable production state in the
      family's only RDF emitter.
- [x] 1.3 Assert the third: a triple whose `Datatype` is `"rdf:JSON"` (`vocabulary/agentic/predicates.go:962`) emits
      `^^rdf:JSON`, an unexpanded prefix rather than an IRI, because `expandDatatypePrefix`
      (`vocabulary/export/object.go:166-176`) knows only `xsd:`.
- [x] 1.4 Commit these as PASSING assertions of current behavior. They flip to the target assertions in task 5; a
      fixture written after the fix reconstructs the bug and proves nothing.

## 2. The closed vocabulary and its mapping

- [x] 2.1 Add the seven untyped string constants to `vocabulary/predicates.go`, beside `IndexingProfile*`
      (`:304-319`), which is the in-package prior art for exactly this shape. Untyped, NOT a named type: a named type
      would break `WithDataType(string)` and the `DataType: "..."` struct-literal path the owner's constraint protects.
- [x] 2.2 Add the legacy→canonical map and `canonicalDataType(string) (string, error)`. Sixteen of the 42 measured
      family spellings are accepted. Under the pragmatic canon (owner ruling Q1) **seven are already canonical and
      need no change at all** — `string`, `entity_id`, `int`, `float`, `bool`, `datetime`, `json`, which between them
      cover the overwhelming majority of declarations — and **nine normalize**: `float64`/`number` → `float`,
      `time.Time`/`timestamp` → `datetime`, `int64` → `int`, `array` → `json`, `entity_ref`/`reference` →
      `entity_id`, `boolean` → `bool`. The other 26 (semdragon Go payload struct names, 30 sites in
      `domain/vocab.go`) are refused. Per-spelling counts are in `design.md` §2.3. The map is a declaration-time
      normalizer, never a runtime alias table.
- [x] 2.3 Unit-test normalization idempotence over the whole domain (`canonical(canonical(x)) == canonical(x)`), the
      total-on-legacy-set property, and refusal outside it. This is the invariant that makes amend-registration safe;
      write it from the spec requirement, not from the map's implementation.
- [x] 2.4 Add the `test/contract/` guard over the **export mapping**, not over constant equality. Q1 makes the
      declaration values deliberately distinct from the per-triple markers (`entity_id` is not `@id`; `json` is not
      `rdf:JSON`), so the invariant to enforce is that every canonical declaration value has exactly one mapping in
      `vocabulary/export/` and that `entity_id` maps to a resource rather than a literal. **The import cycle no longer
      binds this design**: only `vocabulary/export/` needs `message.EntityReferenceDatatype`, and it may import
      `message` freely — the cycle (`vocabulary` → `message` → `payloadregistry` → `vocabulary`,
      `payloadregistry/registry.go:31`) would only have bound a core `vocabulary` constant, which Q1 removed the need
      for.

## 3. Enforce at the one registration seam

- [x] 3.1 Change `validatePredicateMetadataLocked` to take `*PredicateMetadata`, normalize `DataType` in place, and
      refuse an unrecognized value naming both the value and the accepted set. Both entry points already route through
      it (`vocabulary/registry.go:308`, `:371`), so this covers the struct-literal path three sisters use.
- [x] 3.2 Confirm empty stays legal — semlink registers `PredicateMetadata{Name: predicate}` at three sites; refusing
      empty breaks its boot for no gain.
- [x] 3.3 Test the amend path explicitly (gh#410): register with a legacy spelling, re-register without the option,
      assert the canonical value survives and re-validation does not refuse it.

## 4. Migrate the 195 in-repo call sites

- [x] 4.1 Replace every `WithDataType("...")` string literal with the constant. Distribution to expect:
      `string` 140, `float64` 17, `int` 14, `time.Time` 10, `timestamp` 8, `bool` 4, `int64` 1, `entity_ref` 1.
- [x] 4.2 Update the 25 test-file assertions that compare against the legacy spelling (enumerated in the inventory's
      Searches section) — they will fail on the normalized value, which is the guard working.
- [x] 4.3 Add the `test/contract/` completeness guard: `ClearRegistry`, `builtins.Register()`, walk
      `ListRegisteredPredicates()`, assert every framework-declared predicate carries a datatype from the closed set.
      Validation enforces the SET; this guard enforces COMPLETENESS, which validation deliberately does not (empty is
      legal). Show it capable of failing.

## 5. Export honors the declaration

- [x] 5.1 Rewrite `classifyObject` (`vocabulary/export/object.go:41`) to the three-step precedence: per-triple
      datatype, then `vocabulary.GetPredicateMetadata(t.Predicate).DataType`, then `classifyByGoType`. The registry
      lookup is already done in this package by `resolvePredicateIRI` (`export.go:209`), so no new dependency.
- [x] 5.2 Make the entity-reference datatype classify as `objectResource` via `resolveSubjectIRI`, in
      `classifyWithExplicitDatatype` and in the new declaration branch alike. This closes 1.2.
- [x] 5.3 Teach `expandDatatypePrefix` the `rdf:` prefix. This closes 1.3.
- [x] 5.4 Implement the never-fabricate rule: a declared integer over an observed value with a fractional part falls
      back to observation. Property-test it — the invariant is "the emitted lexical form always round-trips to the
      observed value", not a list of examples.
- [x] 5.5 Flip 1.1/1.2/1.3 to the target assertions.

## 6. #1142 rides here

- [x] 6.1 In `classifyString` (`object.go:118`), classify an absolute IRI (`http://`, `https://`, `urn:` scheme) as
      `objectResource` before the literal fallback. Same function, same decision, same review; the sibling issue's own
      suggested direction, which it marks non-prescriptive.
- [x] 6.2 Fixture: an `rdf:type` triple whose object is an absolute IRI emits `<iri>`, not `"iri"`.
- [x] 6.3 Confirm no ambiguity with canonical entity IDs — a 6-part dotted ID carries no scheme, so the two branches
      cannot both match.

## 7. Docs, ADR, and the migration note

- [x] 7.1 Rewrite the doc comments on `PredicateMetadata.DataType` (`predicates.go:359-360`) and `WithDataType`
      (`registry.go:111-112`). Both currently say "the expected **Go type**", which is the axis this change retires.
- [x] 7.2 Rewrite the `Units` and `Range` doc comments to say documentation-only, per the requirement (owner-gated —
      see design Q4).
- [x] 7.3 Fix the false doc comment at `registry.go:227`: it says consumers read `PredicateMetadata.Role`; the named
      consumer reads `Weight` (`pkg/fusion/fusionvocab/signals.go:48`). One line, independent of the Role scope
      ruling (design Q5).
- [x] 7.4 Add the three-way rendering table (Go type / XSD IRI / JSON Schema) to `vocabulary/README.md` (replacing the
      Go-type-inference table at `:44-53` of `vocabulary/export/README.md` and extending `README.md:136`) and
      `docs/basics/04-vocabulary.md:81-89`.
- [x] 7.5 Land ADR-107 (owner-ruled, Q6, widened): the decision recorded is the **boundary rule** — semantic-web
      vocabulary lives at the export edge for interop only; the core carries pragmatic triples with RDF*-like
      statement metadata. The datatype vocabulary and the retirement of the Go-type axis are its first application,
      not its subject. Cross-references ADR-074 and ADR-106; the mechanics stay in the `predicate-contract` spec. A
      draft is already on this branch — review it against the final spec text before ticking.
- [x] 7.7 File "honor `Units`/`Range`" against #1264 (owner ruling Q4), so the deferred decision has a home rather
      than remaining an undocumented gap in a Tier 1 frozen struct.
- [x] 7.6 Write `docs/operations/migration-predicate-datatype.md`: the closed set, the 16-row mapping table, the 26
      unmappable semdragon spellings with their file, the semsource `data_type` wire-value change, and the note that
      semlink's datatype-free registrations stay legal. SemStreams-owned; sister owners implement.

## 8. Gates

- [x] 8.1 `task lint`, `go test -race ./...`, `go test -tags=integration -race -p 2 ./...`, `task schema:generate` +
      clean `git diff schemas/ specs/`, `go test ./test/contract/...`. **All green on `40b8bcf7`**: lint exit 0;
      `go test -race ./...` exit 0, 153 ok; `go test -race -tags=integration -p 2 ./...` exit 0, 153 ok;
      `task schema:generate` then `git status --porcelain schemas/ specs/` empty; `go test ./test/contract/...` ok.
      Also `go run ./cmd/entity-id-audit .` passed (1310 candidates) — `task lint` does NOT run it, CI's Lint job
      does — and `task spec:properties` 74/74.
- [x] 8.2 `task api:compat` naming its base explicitly (the default base moves). Expect ADDITION findings for the new
      constants and ZERO REMOVAL findings — no signature moves. Record that this instrument is BLIND to the semsource
      wire-value change, which is why 7.6 exists. **Run `API_COMPAT_MODE=report ./scripts/api-compat.sh`, base
      `v1.0.0-beta.162` → HEAD `40b8bcf7`: 62 Tier 1 packages compared, 50 clean, 12 incompatible, 0 removed,
      0 added.** `vocabulary` (`release/tier1-packages.txt:95`) and `vocabulary/export` (`:100`) are BOTH in the
      compared set and BOTH clean — the seven constants and `IsValidDataType` are compatible additions and no
      signature moved. *(10.3 later unexported `IsValidDataType`; since it never appeared in a release, that removes
      nothing from the baseline. 10.10 re-runs this against the final head.)* None of the 12 is a package this change touches; all 12 predate it on `main` since
      beta.162. As predicted, the instrument reports nothing about the semsource `data_type` wire value, which
      is why 7.6 exists.
- [x] 8.3 `openspec validate honor-predicate-datatype --strict` and
      `task inventory:verify -- openspec/changes/honor-predicate-datatype/inventory.md`. **Both green**: "Change
      'honor-predicate-datatype' is valid"; inventory `pins=150 ok=150 moved=0 ambiguous=0 drift=0 malformed=0
      unparsed=0` after refreshing the base `232b1e7d` → `40b8bcf7` (76 MOVED auto-updated, 16 DRIFT + 2 AMBIGUOUS
      re-derived by hand — this change rewrote the lines they pin).
- [x] 8.4 Name the e2e tier if the owner rules this BREAKING. The in-repo blast radius is registration-time refusal at
      boot, so `task e2e:core` is the tier that would catch a half-migrated binary; file a coverage gap if the RDF
      export path is genuinely untested end-to-end in this repo (it has zero in-repo callers).
      **`task e2e:core` GREEN, exit 0, on `40b8bcf7`** — 7/7 ports free, all services healthy, readiness and
      heartbeat agreeing at 12/12 healthy components, `core-minted-authority` and `core-graph-roundtrip` scenarios
      both passing. It is the right tier for this change because the containerized binary
      (`cmd/e2e-semstreams/main.go:48`) imports `vocabulary/builtins`, so it walks the exact registration path a
      refused datatype panics on; a half-migrated binary could not have reached "healthy".
      **Coverage gap CONFIRMED and FILED as #1276** (2026-09-09): no e2e tier exercises `vocabulary/export` at all
      — the package has zero in-repo callers, and the family's only production RDF emitter is semconnect's CS API
      gateway. The four output changes in §5 of the migration note are covered by unit fixtures through the
      authoritative ENTITY_STATES seam and by nothing end-to-end. #1276 carries it; it does not gate this change.
- [x] 8.5 Re-measure the Codex/Claude held-file union before implementation — PR #1262 was 7 files at design time and
      grows.

## 9. Spec sync

- [x] 9.1 Spec sync — **DONE, and it carries a second owner ruling.**
      The 2026-09-09 ruling sized the ratchet scenario at "29 of 156 at the composition root — the walk the
      scenario's own words name". But task 10.4 (ruled in the same comment) fixed the guard to walk the production
      union, so the guard ships 218 predicates / 79 bare, not 156 / 29. The two rulings pointed at different walks.
      Escalated rather than resolved silently. **Owner ruled 2026-09-10: the union walk, 79.** Rationale recorded
      with the ruling: the union is what a running binary actually holds, and restricting the guard to the
      composition root would drop the ambient init() set — 77 predicates, 65 of them bare, including every
      `graph.rel.*`, `lifecycle.transition.*` and `entity.identity.type` — out of ratchet coverage entirely.
      All three walks were measured before the question was asked, not after: init() only 77/65 bare, composition
      root only (`ClearRegistry()` + `builtins.Register()`) 156/29, production union 218/79. The ruling's 29 of 156
      reproduces exactly; it simply names a different walk than the one the guard performs.
      The scenario is amended to a behavioural shrink-only ratchet — *the set of framework predicates declaring no
      datatype can only shrink* — with **no count written into the spec**: the count depends on the walk, this
      effort has produced four different ones (65, 29, 79, and the discredited 81), and a number in a spec drifts
      the moment the walk changes. The GIVEN names the walk instead.
      Applied with `openspec archive honor-predicate-datatype -y`: 3 requirements added to
      `openspec/specs/predicate-contract/spec.md`, change archived as `2026-09-10-honor-predicate-datatype`.
      **The 27 `// spec:` citations are not stranded** — `task spec:properties` reports **99/99 resolving** after
      the archive, when they now have to resolve against `openspec/specs/` rather than `openspec/changes/`.
      `task openspec:validate` 53 passed / 0 failed; `task openspec:queue` empty.
      The stale prose reference to the old scenario name in `test/contract/predicate_datatype_contract_test.go:175`
      was updated with it.

## 10. Post-review repairs — HOLD, this change cannot merge until they land

`semstreams-reviewer` returned **CHANGES REQUESTED** on `52bf1add` (1 BLOCKING, 4 HIGH). The owner ruled every open
question 2026-09-09 (PR #1269 comment 5606966440). Everything below is ruled work, not open design.

- [x] 10.1 **Normalizer → owner option (d), no-legacy.** Delete `dataTypeCanonicalization`
      (`vocabulary/predicates.go:386-412`) entirely. `validatePredicateMetadataLocked` accepts ONLY the seven
      canonical values plus absent; everything else is refused. This is stricter than the (a) and (c) options the
      owner was offered — their words were "i agree with (c) but we do not need to support legacy, we just need
      migration notes." It resolves the BLOCKING finding at its root: `double`/`boolean` were accepted while
      `integer`/`dateTime` panicked, an arbitrary split with no owner sign-off. It also dissolves review MEDIUM-8 —
      with no map there is no undated permanent bridge on a Tier 1 frozen package.
      Measured bill as quoted in the ruling, family-wide read-only: **809 sister declarations already canonical
      (92%), 67 need migration** — ~27 semdragon Go struct names already accepted under Q2, plus ~37 additional sites
      (d) costs over (a). Per repo: semspec 16 · semteams 12 · semsource 6 · semconnect 1 · semboids 1 · semdragon 1.
      Concentrated in `array` 20 and `number` 13. In-repo sites were migrated under task 4.1, so this repo is
      unaffected.
      *(Re-measured under 10.9 at the same SHAs: **58**, not 67 — semteams is 3 rather than 12 and semdragon's struct
      names are 30 sites over 26 distinct names. Flagged on the migration note; the ruling's decision is unaffected.)*
      **DONE `19380b65`.** The map is deleted; `validateDataType` accepts the seven plus absent and refuses the
      rest, and `validatePredicateMetadataLocked` no longer rewrites `meta.DataType`. Tests inverted with the
      ruling: the ten retired spellings are now their own refusal corpus (kept apart from the never-accepted
      samples — these are the values whose treatment REVERSED, so a quietly reintroduced map would still refuse a
      Go struct name while accepting these), and the normalization-idempotence test, which lost its subject, is
      replaced by `TestRegistrationStoresTheDeclaredValueUnchanged`.
      Green: `go test -race ./vocabulary/... ./test/contract/...` exit 0, `task lint` exit 0,
      `go run ./cmd/entity-id-audit .` exit 0 (1330 candidates) — the audit annotation went out with the map row
      it pinned. Sweeps for `DataType: "<legacy>"` and `WithDataType("<legacy>")` return none in-repo.
      **Mutation-checked at the wiring, not the primitive**: deleting the `validateDataType` call from
      `validatePredicateMetadataLocked` still builds and turns `TestRegistrationRefusesALegacySpellingOnBothPaths`
      and `TestBothRegistrationEntryPointsRefuseTheSameValue` RED; restored, both green.
      **Note for 10.4/10.5**: `test/contract` stayed GREEN under that mutation. Expected — the guard walks the
      framework's own declarations, which are all canonical either way — but it means the contract guard is not a
      second line of defense for the enforcement seam, only for the declaration ratchet.
- [x] 10.2 Rewrite `design.md` §2.3 and the `predicate-contract` delta's normalization scenario — both currently
      specify normalize-not-refuse, which (d) reverses. The delta's MODIFIED/ADDED block must restate EVERY scenario.
      **DONE.** The delta is an **ADDED** block, not MODIFIED, so it is self-contained and the restate-every-scenario
      rule had nothing to catch here — verified before editing rather than assumed. Requirement prose now says refuse
      rather than translate; the normalization paragraph is replaced by store-what-was-declared plus the stability
      property that A3 actually needs; scenario *a recognized legacy spelling normalizes once at declaration* becomes
      *a retired legacy spelling is refused at declaration*.
      `design.md`: §2.3 retitled and reframed as the migration bill (the measurement survives, the mapping does not);
      §2.4's pointer rationale, §2.5's I2/I3/I4, §4.3's adopter-seam row, §10's problem shape and its
      `expandDatatypePrefix` prior-art bullet, and §12's idempotence justification all follow the ruling. §9's
      Options D and E are annotated rather than rewritten — the record of what was weighed is worth keeping, and it
      contains the lesson: D's bill was quoted at ~950 call sites (all declarations) when the real figure is the 67
      that are not already canonical, never measured because the option had already been rejected.
      **Code followed the design, not the reverse**: §2.4 said the `*PredicateMetadata` pointer existed so the
      validator could normalize in place, so with normalization gone `validatePredicateMetadataLocked` now takes the
      value. Nothing in it mutated any more; a pointer that exists to mutate, in a function that does not, is the
      next defect's seam.
      Green: `openspec validate --strict` exit 0, `go test -race ./vocabulary/... ./test/contract/...` exit 0,
      `task lint` exit 0, `task spec:properties` 99/99.
      **`task inventory:verify` is RED and deliberately left so** — `pins=150 ok=105 moved=38 ambiguous=2 drift=5`.
      All five DRIFT pins are `vocabulary/registry.go:339,402,419`, the exact call sites and signature this task
      rewrote; the 2 AMBIGUOUS at `:554` are the pre-existing pair task 8.3 re-derived by hand. Refreshing now would
      be wasted: 10.3-10.7 move pins again. The refresh is 10.10's, against the final head.
- [x] 10.3 **`IsValidDataType` → unexported** (owner: "in package"). No exported addition to Tier 1 under ADR-106;
      both consumers are contract tests, and `dataTypeCanonicalization` is already visible in-package.
      **DONE.** `isValidDataType` is in-package. The `test/contract/` consumers could not simply follow it there —
      the export-rendering guard needs `vocabulary/export`, so it cannot live inside package `vocabulary` — so the
      contract file now builds its own oracle, `canonicalDataTypes`, from the seven **exported constants**.
      That is better than relocating the calls, and the reason is the one already written into
      `predicate_datatype_test.go`: a guard that asks the implementation "is this canonical?" agrees with whatever
      the implementation now believes and cannot see a value dropped from the set. The exported constants are what
      an adopter writes against, so they are the right thing to walk. The hand-copied seven-constant list in the
      ratchet's failure message collapses into the same variable, and the hard-coded `!= 7` count now checks the
      oracle's own length as well, so an eighth constant cannot pass silently.
      Green: `go test -race ./vocabulary/... ./test/contract/...` exit 0, `gofmt -l` clean.
      Tier 1 compat is unaffected by construction — `IsValidDataType` was added by THIS change and has never
      appeared in a release, so unexporting it removes nothing the baseline `v1.0.0-beta.162` contains. CI's
      `Tier 1 API Compatibility` job is the check; task 8.2's note that "the seven constants and `IsValidDataType`
      are compatible additions" is now stale for the second half.
- [x] 10.4 **Guard repairs (review HIGH-1).** `frameworkPredicateDataTypes`
      (`test/contract/predicate_datatype_contract_test.go:286-300`) reads the ambient registry FIRST and lets it win;
      production is the reverse (`init()` runs, then `main` calls `builtins.Register()`, which amends). Reverse the
      precedence, and drop the two exemptions that are not bare in production: `agent.loop.role` and
      `agent.run.origin-entity-id`, both declared `string` at `vocabulary/agentic/register.go:450-452,490-492`.
      **DONE `122cd7a8`.** Premise measured before implementing, not taken on the review's word — a throwaway probe
      in `test/contract` printed all three walks: ambient (77 predicates) holds both names present with datatype
      `""`; `ClearRegistry()`+`builtins.Register()` (156) holds both as `string`; ambient-then-amend (218) holds both
      as `string`. Exactly 2 predicates differ between ambient-first and production order, and the bare count moves
      **81 → 79**. The two declarations were also read directly at `vocabulary/agentic/register.go:450-452,490-492`.
      The fix goes further than reversing precedence, because the clearing step was the second half of the bug:
      `Register` amends from what is already in the registry (gh#410), so clearing between the two sources destroys
      any datatype a builtins registration inherits rather than restates. The collector now calls
      `builtins.Register()` on top of the ambient registry and reads — which is not an approximation of a running
      binary, it is what the binary does. Exemption set is 79 entries.
      **Mutation-checked**: restoring ambient-first precedence turns the ratchet RED naming exactly
      `[agent.loop.role agent.run.origin-entity-id]`; restored, green. The fix and the exemption removal are
      therefore coupled — neither is green without the other.
- [x] 10.5 **Guard denominator (review HIGH-2).** The ratchet passes over an EMPTY registry — the only assertions are
      `len(noncanonical) > 0` and `len(unexpectedlyBare) > 0`, both trivially false over an empty map, and a mutation
      emptying both collector loops left it green. Assert the denominator beside them.
      **DONE `122cd7a8`.** Two checks, not one. The walk must find a plausible registry (floor 100 against a measured
      218, the measured figure named in the message so a later reader knows what moved). And **every exemption must
      name a predicate that is really registered and really bare** — which is the stronger of the two, because it
      would have caught 10.4's two false exemptions on its own, without anyone re-deriving the precedence bug.
      `TestPredicateDataTypeRatchetCanFail` now also states on the record that `auditPredicateDataTypes(nil)` finds
      nothing, so it is explicit that the audit function cannot be its own denominator guard.
      **Mutation-checked twice**: emptying the collector → RED on the denominator floor ("the walk found 0 framework
      predicates"); re-adding `agent.loop.role` as a false exemption → RED naming it and the `"string"` it declares.
      Both restored green. Note the first mutation is the one the review reported as leaving the guard GREEN.
- [x] 10.6 Sweep the 20 retired-spelling `// DataType:` comments in `vocabulary/agentic/predicates.go` (review
      HIGH-4); the ~40 `// DataType: string (entity ID)` lines are #1275's evidence trail — leave them to that issue.
      **DONE.** Exactly 20, and the census says which: `time.Time` 10, `float64` 9, `int64` 1 → `datetime`, `float`,
      `int`. The `string` (105) and `bool` (4) comments were already canonical and are untouched, so #1275's evidence
      trail is intact.
      **Each replacement was cross-checked against the registration rather than mapped blind**: a script walked every
      one of the 20 comments to the const it documents and looked up that const's `WithDataType` in
      `vocabulary/agentic/register.go`. **16 of 20 agree exactly** with the canonical value written in. The other 4
      register no datatype at all — `ops.diagnosis.confidence`, `ops.config.accuracy`, `ops.config.cost-per-task`,
      `ops.config.p95-latency`, all in the 79-entry exemption set — so for those the comment documents the value's
      shape and not a declaration; canonical spelling is still what an adopter should write, and **#1277** owns
      actually declaring them.
      Repo-wide sweep confirms the review's scoping was right: all 20 retired-spelling `// DataType:` comments were
      in that one file, and none remain anywhere (`grep -rnE` over `*.go`, exit 1). Green: `go test -race` on
      `./vocabulary/... ./test/contract/` exit 0.
- [x] 10.7 Delete `geo:point` from `message/triple.go:84` (review MEDIUM-6). The doc comment offers adopters a value
      that emits `^^<geo:point>` — a relative-reference datatype IRI, invalid RDF, the #1272 mechanism one prefix
      over. Q5 set the precedent for fixing a false doc comment in this change.
      **DONE.** Deleted, and the comment now states the RULE rather than trading one example for another: only
      `xsd:` and `rdf:` expand, an absolute IRI passes through, any other prefix lands as a relative reference and is
      not valid RDF. That is the adopter-seam form — a reader who has never opened `export/object.go` can no longer
      derive an invalid value from this comment, whichever prefix they reach for. `rdf:JSON` replaces it in the
      examples, since that one actually expands.
      Two dependent references followed, because deleting the example without them would have left two documents
      pointing at something that no longer exists: `vocabulary/export/object_test.go:252` keeps its deliberate
      `{"geo:point", "geo:point"}` residual pin — it pins real behaviour and is worth keeping — but its comment no
      longer says the doc comment shows the value; and `docs/operations/migration-predicate-datatype.md:139-140`
      cited the doc comment as the source of the example, so it now states the rule too.
- [x] 10.8 Refresh `design.md`'s stale status text (review MEDIUM-5): `:15-17`, `:78-79` and the Process note at
      `:661-666` still say the design is ungated, the INVENTORY PASS not granted, and #1272 unfiled. All three are
      false and `design.md` is what gets archived.
      **DONE.** The gate was verified by reading PR #1269 comment 5603956923 itself ("approved for 1 and file 2"),
      not by trusting task 0.1's summary of it. Status header now says GATED AND IMPLEMENTED and names the later
      option (d) ruling that changed the design *after* the gate; the ruling-scope bullet and the #1272 bullet are
      annotated rather than deleted, since they record what that particular ruling did and did not cover.
      The Process note is kept and reframed as history: the order still deserves recording, and the useful lesson
      turned out not to be the one it predicted — the gate protects against an inventory that is wrong, and it did
      not protect against a premise the owner later decided differently.
      **A fourth site the review did not list**: `tasks.md`'s own header still opened "Implementation waits on the
      owner's INVENTORY PASS". Same staleness, same fix.
- [x] 10.9 Add the (d) rows to `docs/operations/migration-predicate-datatype.md` — the ~37 additional sites, per
      repository. Under (d) the migration note is the WHOLE adopter story, not a supporting document: nothing
      normalizes any more, so an unmigrated sister panics at boot.
      **DONE, and it is a rewrite rather than added rows** — the note was written for the normalizing draft, so §2
      ("the mapping — what your stored value becomes") was describing something that no longer happens. §2 is now
      "What you must change" with an Action column; §3's discovery table loses the **nowhere** row entirely, which
      is the ruling stated as an adopter seam: every affected declaration moves from the worst rank on the scale to
      the best one available. §4 splits semsource's bill into the half that now announces itself at boot and the
      half that is still silent. The header says read §2 and §3 before upgrading.
      **The numbers were re-measured rather than copied, and they disagree with the ruling.** Read-only sweep on
      2026-09-10 at the SAME SHAs the 2026-09-09 pass used, so it is a reproduction: `git status --porcelain`
      captured before and after each repo and verified byte-identical, no `go` command run in any sister. Five of
      seven rows reproduce exactly. **semteams measures 3, not 12**, and semdragon's struct names measure 30 sites
      over 26 distinct names, not ~27. **Family total 58, not 67**, out of 775 declarations — 92% already canonical.
      Recorded in the note as a flagged discrepancy, not a silent correction: the ruling turned on the principle and
      58 is smaller than the bill the owner accepted, so the decision is unaffected, but the owner should know the
      figure quoted in their own ruling did not reproduce. **The walk is published with the numbers** — all three
      declaration paths, `*.go` including tests — because this effort has now produced several different counts of
      the same thing and every one of them depended on the walk.
      The note's own verification snippet was **run against semteams read-only and returns exactly `3 number`**, so
      the command an adopter is told to run reproduces the table's figure.
- [ ] 10.10 **HOLD — this change cannot merge until every task in this section lands.** The hold lives here, on
      the last task in the section, because `scripts/openspec-queue.sh:64,145` matches caveats against
      UNCHECKED lines only — parked on 10.1 it vanished from the queue the moment 10.1 was ticked, leaving
      a still-blocked change reading as an ordinary fraction.
      Re-run every gate in section 8 and re-review. The change is BREAKING for sisters, so `task e2e:core`
      must be green again before it lands.
