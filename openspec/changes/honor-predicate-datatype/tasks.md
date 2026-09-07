# Tasks: honor-predicate-datatype (#1267)

Implementation waits on the owner's INVENTORY PASS recorded on PR #1269. Task 1 is deliberately first: the issue marks
the `"5.0"^^xsd:double` consequence **inferred from the code path, not reproduced**, and a design premise that has
never been observed is a hypothesis. Nothing else starts until it is a failing assertion.

## 0. Gate — BLOCKED on the owner's INVENTORY PASS and the Q1–Q7 rulings

- [ ] 0.1 BLOCKED: no task below starts until the owner records INVENTORY PASS on PR #1269 **and** rules Q1–Q7 in
      `design.md`. Q1 (RDF-shaped vs Go-shaped axis) is load-bearing for tasks 2–8; Q4 governs whether the spec
      delta's third requirement is struck; Q2 governs whether the semdragon boot break lands now or is staged.
      This marker exists so `task openspec:queue` reports the hold instead of reading `0/N — ok`.
- [ ] 0.2 Note for the implementing session: the two consequences task 1 exists to prove were **already reproduced
      empirically** on 2026-09-07 (throwaway in-package probe, removed; evidence recorded on PR #1269). Turtle emits
      `"acme.ops.gcs.robotics.drone.002"^^<@id>` — a relative-reference datatype IRI, invalid RDF — and
      `"5.0"^^xsd:double` for a declared `int` after the JSON round trip. Task 1 therefore commits a fixture for
      known behavior; it is not an open investigation. Assert on `^^<@id>`, not `^^@id`: the serializer wraps the
      datatype in angle brackets, and a probe asserting the unwrapped form PASSES while the defect is present.

## 1. Prove the two consequences before changing anything

- [ ] 1.1 Add `vocabulary/export/datatype_roundtrip_test.go`: build a `graph.EntityState` whose triple has
      `Object: 5` under a predicate registered `WithDataType("int")`, pass it through `graph.MarshalEntityState` and
      `graph.UnmarshalEntityState`, assert the decoded `Object` is `float64(5)`, then `export.SerializeToString` to
      N-Triples and assert the emitted literal is today's `"5.0"^^<http://www.w3.org/2001/XMLSchema#double>`. This is
      the whole issue in one fixture; it proves the JSON-round-trip half, which is the load-bearing half, not just the
      classifier half.
- [ ] 1.2 In the same file, assert the second, larger consequence found at design time: a triple carrying
      `Datatype: message.EntityReferenceDatatype` (`"@id"`) whose object is a canonical entity ID currently serializes
      as a LITERAL with datatype `@id` — not an IRI, and not valid RDF. `@id` is validated at the authoritative
      persistence seam (`graph/entity_predicate_contract.go:168`), so this is reachable production state in the
      family's only RDF emitter.
- [ ] 1.3 Assert the third: a triple whose `Datatype` is `"rdf:JSON"` (`vocabulary/agentic/predicates.go:962`) emits
      `^^rdf:JSON`, an unexpanded prefix rather than an IRI, because `expandDatatypePrefix`
      (`vocabulary/export/object.go:166-176`) knows only `xsd:`.
- [ ] 1.4 Commit these as PASSING assertions of current behavior. They flip to the target assertions in task 5; a
      fixture written after the fix reconstructs the bug and proves nothing.

## 2. The closed vocabulary and its mapping

- [ ] 2.1 Add the seven untyped string constants to `vocabulary/predicates.go`, beside `IndexingProfile*`
      (`:304-319`), which is the in-package prior art for exactly this shape. Untyped, NOT a named type: a named type
      would break `WithDataType(string)` and the `DataType: "..."` struct-literal path the owner's constraint protects.
- [ ] 2.2 Add the legacy→canonical map and `canonicalDataType(string) (string, error)`. Sixteen of the 42 measured family spellings are accepted —
      fourteen normalize, and `string`/`boolean` are already canonical; the other 26 (semdragon, 30 sites in
      `domain/vocab.go`) are refused. The map is a declaration-time normalizer, never a runtime alias table.
- [ ] 2.3 Unit-test normalization idempotence over the whole domain (`canonical(canonical(x)) == canonical(x)`), the
      total-on-legacy-set property, and refusal outside it. This is the invariant that makes amend-registration safe;
      write it from the spec requirement, not from the map's implementation.
- [ ] 2.4 Add the `test/contract/` guard asserting `vocabulary.DataTypeEntityRef == message.EntityReferenceDatatype`
      and `vocabulary.DataTypeJSON == agentic.TodoRecordJSONDatatype`. A shared Go constant is impossible: `vocabulary`
      → `message` → `payloadregistry` → `vocabulary` is a real import cycle (`payloadregistry/registry.go:31`).

## 3. Enforce at the one registration seam

- [ ] 3.1 Change `validatePredicateMetadataLocked` to take `*PredicateMetadata`, normalize `DataType` in place, and
      refuse an unrecognized value naming both the value and the accepted set. Both entry points already route through
      it (`vocabulary/registry.go:308`, `:371`), so this covers the struct-literal path three sisters use.
- [ ] 3.2 Confirm empty stays legal — semlink registers `PredicateMetadata{Name: predicate}` at three sites; refusing
      empty breaks its boot for no gain.
- [ ] 3.3 Test the amend path explicitly (gh#410): register with a legacy spelling, re-register without the option,
      assert the canonical value survives and re-validation does not refuse it.

## 4. Migrate the 195 in-repo call sites

- [ ] 4.1 Replace every `WithDataType("...")` string literal with the constant. Distribution to expect:
      `string` 140, `float64` 17, `int` 14, `time.Time` 10, `timestamp` 8, `bool` 4, `int64` 1, `entity_ref` 1.
- [ ] 4.2 Update the 25 test-file assertions that compare against the legacy spelling (enumerated in the inventory's
      Searches section) — they will fail on the normalized value, which is the guard working.
- [ ] 4.3 Add the `test/contract/` completeness guard: `ClearRegistry`, `builtins.Register()`, walk
      `ListRegisteredPredicates()`, assert every framework-declared predicate carries a datatype from the closed set.
      Validation enforces the SET; this guard enforces COMPLETENESS, which validation deliberately does not (empty is
      legal). Show it capable of failing.

## 5. Export honors the declaration

- [ ] 5.1 Rewrite `classifyObject` (`vocabulary/export/object.go:41`) to the three-step precedence: per-triple
      datatype, then `vocabulary.GetPredicateMetadata(t.Predicate).DataType`, then `classifyByGoType`. The registry
      lookup is already done in this package by `resolvePredicateIRI` (`export.go:209`), so no new dependency.
- [ ] 5.2 Make the entity-reference datatype classify as `objectResource` via `resolveSubjectIRI`, in
      `classifyWithExplicitDatatype` and in the new declaration branch alike. This closes 1.2.
- [ ] 5.3 Teach `expandDatatypePrefix` the `rdf:` prefix. This closes 1.3.
- [ ] 5.4 Implement the never-fabricate rule: a declared integer over an observed value with a fractional part falls
      back to observation. Property-test it — the invariant is "the emitted lexical form always round-trips to the
      observed value", not a list of examples.
- [ ] 5.5 Flip 1.1/1.2/1.3 to the target assertions.

## 6. #1142 rides here

- [ ] 6.1 In `classifyString` (`object.go:118`), classify an absolute IRI (`http://`, `https://`, `urn:` scheme) as
      `objectResource` before the literal fallback. Same function, same decision, same review; the sibling issue's own
      suggested direction, which it marks non-prescriptive.
- [ ] 6.2 Fixture: an `rdf:type` triple whose object is an absolute IRI emits `<iri>`, not `"iri"`.
- [ ] 6.3 Confirm no ambiguity with canonical entity IDs — a 6-part dotted ID carries no scheme, so the two branches
      cannot both match.

## 7. Docs, ADR, and the migration note

- [ ] 7.1 Rewrite the doc comments on `PredicateMetadata.DataType` (`predicates.go:359-360`) and `WithDataType`
      (`registry.go:111-112`). Both currently say "the expected **Go type**", which is the axis this change retires.
- [ ] 7.2 Rewrite the `Units` and `Range` doc comments to say documentation-only, per the requirement (owner-gated —
      see design Q4).
- [ ] 7.3 Fix the false doc comment at `registry.go:227`: it says consumers read `PredicateMetadata.Role`; the named
      consumer reads `Weight` (`pkg/fusion/fusionvocab/signals.go:48`). One line, independent of the Role scope
      ruling (design Q5).
- [ ] 7.4 Add the three-way rendering table (Go type / XSD IRI / JSON Schema) to `vocabulary/README.md` (replacing the
      Go-type-inference table at `:44-53` of `vocabulary/export/README.md` and extending `README.md:136`) and
      `docs/basics/04-vocabulary.md:81-89`.
- [ ] 7.5 Draft ADR-107 (owner-gated — design Q6): the declared predicate datatype is a closed, RDF-shaped vocabulary
      and the Go-type axis is retired. Cross-repo contract, therefore an ADR; the mechanics stay in the spec.
- [ ] 7.6 Write `docs/operations/migration-predicate-datatype.md`: the closed set, the 16-row mapping table, the 26
      unmappable semdragon spellings with their file, the semsource `data_type` wire-value change, and the note that
      semlink's datatype-free registrations stay legal. SemStreams-owned; sister owners implement.

## 8. Gates

- [ ] 8.1 `task lint`, `go test -race ./...`, `go test -tags=integration -race -p 2 ./...`, `task schema:generate` +
      clean `git diff schemas/ specs/`, `go test ./test/contract/...`.
- [ ] 8.2 `task api:compat` naming its base explicitly (the default base moves). Expect ADDITION findings for the new
      constants and ZERO REMOVAL findings — no signature moves. Record that this instrument is BLIND to the semsource
      wire-value change, which is why 7.6 exists.
- [ ] 8.3 `openspec validate honor-predicate-datatype --strict` and
      `task inventory:verify -- openspec/changes/honor-predicate-datatype/inventory.md`.
- [ ] 8.4 Name the e2e tier if the owner rules this BREAKING. The in-repo blast radius is registration-time refusal at
      boot, so `task e2e:core` is the tier that would catch a half-migrated binary; file a coverage gap if the RDF
      export path is genuinely untested end-to-end in this repo (it has zero in-repo callers).
- [ ] 8.5 Re-measure the Codex/Claude held-file union before implementation — PR #1262 was 7 files at design time and
      grows.

## 9. Spec sync

- [ ] 9.1 Apply the delta to `openspec/specs/predicate-contract/spec.md` as the last content commit, reviewed with the
      code.
