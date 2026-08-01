# Tasks — discovery-under-stream-shapes (gh#810, gh#822)

> ## ⚠ REWORK REQUIRED — read before writing any code (Fable shape gate, 2026-08-01)
>
> The first implementation attempt is **PR #847, now DRAFT**. Its framework seams were built,
> tested and mutation-checked — and two of them **do not do what they claim**. The Codex round
> found it; the deferral of gh#842 had already been granted on the premise the guard falsified.
>
> **What went wrong, so it is not repeated rather than merely avoided:**
> - `DecodeQueryReply` / `IsPublishAck` / `ErrPublishAck` shipped with **zero production callers**.
>   The motivating `tool.list` path still unmarshals a publish ack into an empty catalog. The bug
>   was not fixed for the caller that motivated it.
> - A pub-ack detector **already existed** at `gateway/graph-gateway/component.go:1475`, in use,
>   with a doc comment naming the same overlap problem. A second drifting definition was added
>   without grepping for the first.
> - The "provisioning guard" is a **detached goroutine with a discarded result**, wired into no
>   provisioning seam. It does not fail at boot. gh#842's deferral was argued on the claim that it
>   does.
> - The ack key set was **hand-listed** and already stale against pinned `nats.go v1.52.0`, which
>   defines `PubAck.Value` as `json:"val,omitempty"` — a value-bearing ack bypasses detection.
> - Wildcard matching handles the **filter side only**, though task 1.2 required both sides.
> - The subject-export test compares `QuerySubjects()` against its own backing slice — tautological,
>   never drives `setupQueryHandlers`.
>
> **Binding constraints for the rework (Fable, recorded so this starts implementation-ready):**
> 1. **ONE declared-subject registry**, derived from the same declarations the handlers register
>    from, consulted **SYNCHRONOUSLY at all three provisioning seams**. A failed check is a **boot
>    error, not a log line**.
> 2. **ONE pub-ack detector.** Retire the gateway's private detector *into* the canonical decoder.
>    The duplicate dies by consolidation, not coexistence.
> 3. **Ack key set DERIVED from the pinned `jetstream.PubAck` type** (reflection over json tags),
>    never hand-listed, so a nats.go upgrade shifts it automatically. Its test asserts against
>    fixture ack **bytes**, not the same reflection — non-tautological.
> 4. **Wildcard intersection in BOTH directions** (declared subject tokens × stream filter tokens).
> 5. **`DecodeQueryReply` ships WITH its motivating caller wired** (`tool.list`) or does not ship.
>    Zero-caller surface is the phantom rule; no exception for our own APIs.
> 6. New exported `graph`/`natsclient` surface is core API and needs a **recorded Fable shape
>    review** before invention — the gate skipped last time.
>
> **gh#842's deferral is now CONDITIONAL on this:** valid if and only if the synchronous
> fail-at-boot guard lands in .160. Ship the guard advisory or warn-only and the default-subject
> move returns to .160 scope as breaking-with-lockstep. Neither may be decided separately again.


**Amend a task line when the work HAPPENS, not only when it succeeds.** An unamended
line is indistinguishable from a skipped gate six weeks later — that misreading has
cost three sessions. A deliberate not-done gets `[~]`, its reasoning, AND propagation
into the spec delta: a `[~]` stops the implementer and does not stop the archiver.

**Run `task openspec:queue` before archiving.**

## 0. Ruling needed before section 3

- [x] 0.1 **RULED 2026-08-01 (owner/Fable): the default-subject move is OUT of gh#810 and OUT of
      v1.0.0-beta.160 — DEFERRED to the next breaking wave, not dropped. Filed as gh#842.**
      The scoping divergence was real: the session-20 ruling had three parts, the tag roadmap
      listed two while declaring .160 additive, and the supersession was never recorded.
      **The reasoning that makes deferral correct rather than convenient — record it, because a
      deferral without its argument is indistinguishable from an omission:** the provisioning
      guard in section 1 *changes this item's urgency class*. The move was in the original ruling
      because the failure was SILENT DATA LOSS; once the guard exists the same misconfiguration
      fails loudly at boot with a one-line documented remedy. What remains is "defaults should
      compose out of the box" — real, but a COORDINATION-COST item that breaks every existing
      discovery caller, and it rides the next lockstep (v1.0.0 the natural boundary) at
      essentially zero marginal cost. **Corollary recorded on gh#842: the deferral is only valid
      BECAUSE the guard ships. If gh#810 does not land, this reverts to silent data loss and the
      argument collapses.**

- [ ] 0.2 **BLOCKED — REWORK REQUIRED, read the banner at the top of this file before writing code.** PR #847 is DRAFT: its guard is a detached goroutine wired to no provisioning seam, its decoder shipped with zero production callers, and it added a second pub-ack detector beside the gateway's existing one. gh#842's deferral is CONDITIONAL on the synchronous fail-at-boot guard landing in .160.

## 1. Provisioning guard — the seam that closes the class

> **ATTEMPTED IN PR #847, NOT LANDED.** The pure primitives (`SubjectFilterCaptures`,
> `FindSubjectCaptures`) exist and are mutation-checked against the naive prefix test, but 1.2 is
> incomplete (filter-side wildcards only) and 1.3's decision was made WRONG: it reports rather than
> refuses, and asynchronously, which is what falsified gh#842's premise. Reuse the primitives;
> redo the policy and the wiring.


- [ ] 1.1 Enumerate the declared request/reply subjects from their **owning components**, not
      from a router or a registration table — those answer a different question, and this repo
      has two instances of that exact miss (#787, #792). Scrape rather than hand-maintain.
- [ ] 1.2 Implement the overlap test between stream subject filters and declared request/reply
      subjects. Wildcards on BOTH sides: `tool.>` covers `tool.list`, and a declared subject
      could itself contain a wildcard. Cover `*` vs `>` semantics explicitly — token-position
      wildcards are the wrong tool for a suffix question (`predicate_index.go` documents that
      lesson).
- [ ] 1.3 Decide and record: **refuse to start** vs **loud WARN**. gh#810 leans fail-closed;
      the framework's fail-closed boot rule agrees; but a refusal turns an existing working-ish
      deployment into a boot failure. Whichever wins, the reasoning goes in the code, not only
      the PR.
- [ ] 1.4 The report MUST name the capturing stream AND the captured subject AND the override.
      "Subject collision detected" without those three sends an operator reading configs by
      hand — the same tax gh#837's `"put community failed"` imposed.
- [ ] 1.5 Test: a stream covering a declared subject is reported; one covering nothing is not;
      a subject declared AFTER the stream is still caught (the class-closure property).
- [ ] 1.6 **Mutation-check 1.5**: make the overlap test always return false and confirm the test
      FAILS. A guard that cannot fail is the failure mode this repo keeps paying for.

## 2. Pub-ack rejection in the canonical decoder

- [ ] 2.1 `graph.UnwrapQueryResponse` (`graph/query_contracts.go:91`) rejects a JetStream publish
      ack instead of decoding it. Verified as the right home: it is already the gateway's decoder
      (`gateway/graph-gateway/component.go:1741`) and is what gh#785 names as canonical.
- [ ] 2.2 Detect the ack by its **shape** (`stream` + `seq`), and pin what "ack" means in one
      place so the discriminator cannot drift from the envelope's reserved key set.
- [ ] 2.3 Test both directions: a publish ack fails with an error naming it; a genuine EMPTY
      result still decodes as an empty result. The second is the one that matters — conflating
      "captured request" with "nothing registered" is the whole bug, and a rejection that also
      rejects legitimate empties would trade one wrong answer for another.
- [ ] 2.4 **Mutation-check 2.3** by feeding the exact body from gh#810 (`{"stream":"TOOL","seq":1}`)
      through the pre-fix decoder and confirming it produced an empty catalog.
- [ ] 2.5 Audit the other in-repo shape-knowers gh#785 lists (agentic-tools executors,
      graph-clustering query reads, e2e client). Not necessarily migrated here — but record
      which of them can still decode an ack into an empty result, so the remainder is a known
      set rather than an assumption.

## 3. Export the request-subject list (gh#822)

- [ ] 3.1 Export the set from the SAME declaration `setupQueryHandlers` registers from
      (`processor/graph-query/query.go:30-50`), so it cannot drift from what is served.
- [ ] 3.2 Test that the exported set and the registered handlers agree exactly, in BOTH
      directions. A test asserting only "every exported subject is registered" passes on an
      export that omits half the surface — and SemSource's hand-maintained copy was found to be
      incomplete in exactly that way (it omitted `graph.query.byName`).
- [ ] 3.3 Reply on gh#822 with the exported symbol so SemSource can make their
      `TestNoSemSourceSubjectCollidesWithTheSubstrate` gate exact. Their side is theirs to
      change — communicate, do not edit ([[sister repos are hands-off]]).
- [ ] 3.4 Record whether gh#819 and gh#820 (same "computed but never exposed" shape) are closed
      by this or remain open. Same class; do not leave a reader inferring it.

## 4. Restore the e2e stage to a live guard

> **SEQUENCING (owner ruling 2026-08-01): gh#811 lands BEFORE gh#810's coverage gate is claimed,
> preferably whole as its own tiny PR.** This fell out of task 4.3 below. Without that ordering
> gh#810 would satisfy the E2E-coverage rule's *letter* while shipping the exact false-green class
> gh#811 names — a restored stage inside a tier that structurally cannot fail. Claiming coverage
> from an unfailable gate is worse than claiming none, because it retires the coverage gap on
> paper.


- [ ] 4.1 Remove the crud-tools config override so the stage runs on the **default** subject,
      and confirm it goes GREEN. The revert that put it back on the default was deliberate; this
      is the landing it was waiting for.
- [ ] 4.2 Confirm the stage would still catch a regression: re-introduce the `tool.>` stream
      coverage and verify the stage fails. Otherwise it returns to green for the wrong reason.
- [ ] 4.3 **The crud-tools tier carries `ignore_error: true` (gh#811)** — so `task e2e:crud-tools`
      exits 0 on scenario failure and this stage structurally CANNOT gate. Either gh#811 lands
      first or the stage's verdict must be read from the log, and which one is true gets recorded
      here. A stage that looks like coverage and is not is worse than no stage.

## 5. Gates

- [ ] 5.1 `gofmt`, `task lint`, `go vet ./...` plain + `-tags=integration` + `-tags=live_llm`, `-mod=readonly`.
- [ ] 5.2 BOTH suites: `go test -race ./...` AND `go test -race -tags=integration -p 2 -count=1 ./...`. Grep `^FAIL` — pipeline exit codes report the tail stage. **Host contention is real on this laptop (three projects share one Docker daemon); a container-start failure is gh#736's class — verify green standalone before attributing it to code, and treat CI as the arbiter.**
- [ ] 5.3 `task schema:generate` + `git diff schemas/ specs/` clean.
- [ ] 5.4 `go test ./test/contract/...`.
- [ ] 5.5 `task e2e:agentic` and `task e2e:crud-tools` — the two tiers on the touched path.
- [ ] 5.6 `semstreams-reviewer` pass on the full diff.
- [ ] 5.7 Owner-run Codex round; arm `--auto` only AFTER it closes.
- [ ] 5.8 Owner CONFIRM-CLOSE before closing gh#810 and gh#822.
- [ ] 5.9 Archive: `agentic-tools` has a written Purpose already; `graph-query`'s is written too.
      Confirm neither regresses to a `TBD - created by archiving` stub.
