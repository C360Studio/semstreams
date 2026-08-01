# Tasks — discovery-under-stream-shapes (gh#810, gh#822)

**Amend a task line when the work HAPPENS, not only when it succeeds.** An unamended
line is indistinguishable from a skipped gate six weeks later — that misreading has
cost three sessions. A deliberate not-done gets `[~]`, its reasoning, AND propagation
into the spec delta: a `[~]` stops the implementer and does not stop the archiver.

**Run `task openspec:queue` before archiving.**

## 0. Ruling needed before section 3

- [ ] 0.1 **RULING REQUIRED — BLOCKED until answered; this gates section 3 and the tag scope.** Confirm the default-subject move is out of scope for v1.0.0-beta.160. The baton
      states this issue's scope two ways: the Fable ruling (session 20) includes a
      **default-subject move**; the tag roadmap (gh#840) replaces it with **gh#822 subject
      export**. The move is breaking (gh#810 says so and asks for lockstep), and the roadmap
      calls .160 additive with "no breaking wave planned". This change assumes the move was
      dropped deliberately. **If that reading is wrong, stop** — it belongs in a breaking wave
      with sister lockstep, not in an additive tag. Record the ruling either way.

## 1. Provisioning guard — the seam that closes the class

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
