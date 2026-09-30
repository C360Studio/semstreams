# Tasks

## 1. Design

- [x] 1.1 Premises pinned at `1b1accf4` and the sweep recorded (`design.md` P1–P8); decisions D1–D5; rejected R1–R4.
- [x] 1.2 Spec deltas: `graph-ingest` (validate-then-fence on the Graphable lane), `payload-registry` (identity-failure
      shape of a registered Graphable). `openspec validate --strict` exit 0.

## 2. Delivery

- [x] 2.1 `processor/graph-ingest`: `decodeEntity` validates the decoded `BaseMessage` before extraction and fences a
      panic from the payload's `EntityID()`/`Triples()`/`StorageRef()`/`IndexingProfile()` into a classified error on
      the existing poison path (D1, D2). Tests through `decodeEntity` with a registered test payload: (a) a payload
      whose `Validate()` fails is rejected and its `EntityID()` is never called; (b) a payload whose `EntityID()`
      panics yields an error, no panic escapes, `c.errors` increments; (c) the same for `Triples()`; (d) a conforming
      payload ingests unchanged. Fails-without-fix evidence for (a)–(c) via the `cp` backup and checksum ritual
      (`docs/contributing/01-testing.md` § 164–183), recorded in `evidence.md`.
- [x] 2.2 `agentic`: `LoopExecutionEntity.EntityID()` returns `""` through the `Try` form with the line-pinned
      sentinel annotation; `Triples()` omits `parent`/`reply_to` on a constructor error (D3). Tests: malformed
      org/platform/loop ID → `""`, no panic; malformed `ParentLoopID`/`InReplyTo` → the triple is absent, the others
      present, no panic; `Validate()` returns an error on each, no panic. `go run ./cmd/entity-id-audit .` exit 0.
      (Malformed reference: `Validate()` errors when no other spawn-identity fact remains, else the triple is omitted
      per D3; scope note in `evidence.md`.)
- [x] 2.3 Docs (D5): one sentence each in `.agents/skills/new-payload/SKILL.md` and
      `docs/concepts/15-payload-registry.md`; `docs/operations/migration-fact-lane-validate.md` (the contract
      tightening, who is affected, the one check).
- [x] 2.4 Gates on the branch: `task check`; `go test -race` for `./processor/graph-ingest/... ./agentic/...`;
      `task schema:generate` with an empty `git diff schemas/ specs/`; `openspec validate --strict`.

## 3. Review and landing

- [ ] 3.1 `semstreams-reviewer` on the diff; findings fixed and re-reviewed.
- [ ] 3.2 Breaking gate: `task e2e:core` green at the final code revision, Docker window announced first
      (`docker compose ls -q | wc -l` and e2e processes both 0 before starting); run id recorded in `evidence.md`.
- [ ] 3.3 Archive as the last content commit; undraft; merge per the protocol's gate (main's CI re-read in the
      command that arms auto-merge; flake #1421 needs its own waiver if it reddens a required job).
