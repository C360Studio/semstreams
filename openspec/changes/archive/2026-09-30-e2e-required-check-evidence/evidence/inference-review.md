# #1222 inference adoption review

Mode: bounded implementation review. Base `fe6e2cc03e16f5db47e293f55939548572f204cc`;
worktree `/Users/coby/.codex/worktrees/e2e-required-proof/semstreams`. Accepted design
SHA-256 `a31a5c690da763b124a7d1e1727836e57a270f943b5b61576216d7f386b9b48c`.
No tests, source edits, Git mutations, Docker or GitHub actions were performed.

## Findings

### HIGH I1 — tiered_required_evidence.go:11 — optional declaration breaks legacy fallback

`CheckRequirements` returns nil for semantic-fallback, but current
`cmd/e2e/runner.go:23` treats interface presence as adoption. The legacy fallback
selection (`selection.go:42`) consequently reaches strict DeclareChecks with an empty
set before Setup and refuses execution. Fixing that bridge alone is insufficient:
the constructor supplies evidence IDs (`main.go:460`), and `TieredScenario.Execute`
at `tiered.go:563` independently declares the same empty set.

Smallest correction: distinguish the known execution-only variant from adopted
variants at both boundaries. Required selections must still reject absent/empty
declarations; known legacy fallback must execute and remain unattested. Add a test
through the actual resolver/constructor/declaration bridge proving fallback reaches
its lifecycle and does not gain required proof, plus an empty required-catalog refusal.

Refutation: the resolver does deliberately leave fallback `required=false`; the
unconditional adopted declaration overrides that intent. This is a static trace,
not a test run. The heavy 8b/frontier tasks actually pass `--variant semantic`
(`taskfiles/e2e/semantic.yml:45,63`); they do not traverse this nil-catalog branch.
No identical heavy-variant failure is asserted, and their Task provenance is outside
this seven-file freeze.

### HIGH I2 — tiered_required_evidence_test.go:15 — required negative sensitivity is unproven

The accepted mutation control must detect omitted observation/query or weakened
identity acceptance. The recorded forced-false match rejects healthy inputs; it
does not establish that wrong/empty identity incorrectly passing is detected. Its
raw output and initial missing-symbol RED are not retained artifacts. Current tests
exercise exact/foreign identity and an actual HTTP query path, but none is retained
as the required compiled negative-control experiment. Canonical testing policy
requires it for consequential proof acceptance and this concrete plausible fault.

Complete and retain the bounded agreed negative control, with source/command hashes,
baseline, compiled intended-assertion failure and restored baseline; or present a
specific constrained deferral for review. No such deferral is accepted here. The
earlier rejected broad tail replacement does not establish that this different
control cannot be completed; this review authorizes no workaround for that denial.
The author's corrected **UNVERIFIED** label is accurate, but completion remains open.

### MEDIUM I3 — core_slow_consumer.go:230 — present null is recorded as absent

The named `slow-consumer.drop-available-absent` check uses map-value equality to nil.
A decoded `"dropped_available": null` therefore passes exactly like a missing key
and records `<nil>` for both. This predicate predates adoption, but is now exposed
as proof of absence. The production fallback emits a present false value
(`natsclient/client.go:1605`); that case fails correctly, which bounds the exposure.

Use map key presence for the absence assertion and retain presence/value evidence.
Test absent, present-null and present-false through named recording/finalization.
Other required slow-consumer fields compare against concrete expected values and
do not pass when missing. Earlier passing checks and the first failure are retained;
unevaluated later conditions remain missing.

## Verified conformance and limits

The declared sets match the accepted initial catalog. Observation code adopts the
existing Result recorder/finalizer rather than creating another acceptance owner.
Existing fatal stage behavior and B0/B2 diagnostic behavior are unchanged.

Controlled search compares full fixture identity in the actual executor response:
`testdata/semantic/documents.jsonl:2` supplies operations/doc-ops-001;
`examples/processors/document/payload_document.go:58` defines its minted suffix;
the tier records its observed authority before stages. `validate_search.go:20–52`
uses the configured GraphQL consumer and compares exact identity, not substring or
hit count. The bridge test uses HTTP and checks passed/failed named observations.
It supplies a canned response and does not validate request variables. Apparent
query/response shape mismatch was refuted by the gateway's raw response wrapper at
`gateway/graph-gateway/component.go:1524`; no schema-shape finding is raised.

Graph roundtrip calls the actual probe, which checks authoritative replacement,
GraphQL, mutation trace and KV evidence before returning details (`graph_roundtrip.go:
178–225`); the wrapper additionally compares authority/trace-derived entity identity.
Structural checks retain the existing metrics-plus-component-inventory absence
predicate, not a stored-community count. Semantic availability comes from the actual
semembed HTTP health check (`validate_infra.go:262,301`) and its final comparison
(`validate_search.go:509`), not an arbitrary successful callback. Failure earlier in
execution leaves later required observations absent and cannot satisfy complete proof.

Focused tests provide deterministic identity cases, HTTP recording, structural
recording, existing metric failures and slow-consumer partial failure. They do not
prove assembled ingestion/query behavior or cover every new named observation at
its outer lifecycle boundary. Existing finite examples suit these private predicates;
no new exported parser is introduced. No PBT/fuzz exploration is claimed.

## Frozen evidence

All seven hashes matched before/after review, under `test/e2e/scenarios/`:

| File | SHA-256 |
| --- | --- |
| tiered.go | 453eddc4477d6384c95419f62cb24f43414d659b73d77c33dec4b4001cd2c1cf |
| tiered_structural.go | fc8a5c603a5e765b4dcbe58e02f7043b7bd6ab68d7dbba5e7b5e3fd771db08b2 |
| validate_infra.go | 991aac171c6b809682e07013fd68ae3719274a9a0cb9043152f96e0ec020a298 |
| validate_search.go | b270b6386cb2abe0f278c1036209808bebc4a67df07ea04de18afeee6dcd3b41 |
| core_slow_consumer.go | edcd0efc22347a46fbe2c08c6330b400a3bb2b63541a04711bca4286e4e408ec |
| tiered_required_evidence.go | 78751562110e6313984f69a93f870b434dbd89d2a649b10986ef027a8c5e4423 |
| tiered_required_evidence_test.go | a771c38e1192f04cc3c4f95393a47587a00cdfbd81fa9272221d50f85118ed1e |

Checkpoint `/private/tmp/semstreams-1222-inference-adoption.md` SHA-256
`23054b89c262f1c1e92afa82da148fda23a88cd3b38d2bef7d7cca9f8145f81f`.
Read/hashed raw focused logs (prefix `/private/tmp/semstreams-1222-inference-focused-`):
`green.log` = `e101c7dc53fc119563b93354cae4abaf6cb412ef4b09d1d4d9a5361d4777d941`;
`race.log` = `b495f29b620b7507c46b12293921bb48d14edd4a9f663f485546e85839033143`.
They report package success; exact focused/race invocation is author-recorded in the
checkpoint, not embedded in those single-line logs. Dependencies/CLI were not frozen.
Prior gopls cache loading was unavailable; bounded source searches/read ranges used.
Raw logs must be retained durably before integration. No broad gate or E2E proof.

**CHANGES REQUESTED — I1 and I2; also correct I3.** This verdict concerns this bounded
adoption and its immediate compatibility seam, not Writer W1/C5 or whole-feature readiness.
