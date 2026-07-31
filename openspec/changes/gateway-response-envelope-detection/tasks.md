# Tasks — gateway response envelope detection

**Task-line discipline (from the gh#731/#733 close-out):** amend a line when the work HAPPENS, not
only when it succeeds. That change closed carrying `NOT RUN` on two gates that had run, because the
lines were written ahead of the work and never revisited — a state file predicting a GAP costs a
session re-litigating finished work, exactly as a file predicting success costs a false green.

## 1. Design sign-off (before implementation)

- [ ] 1.1 Fable design review of `design.md`. This is BREAKING on consumer surface and adds a new
      exported symbol to the `graph` framework package, so review happens at DESIGN time, before
      implementation, per the contracts' exported-surface rule
- [ ] 1.2 Resolve design Open Question 1 — which e2e tier owns the shape stage (`core` vs
      `structural`). Leaning `core`; the assertion needs no inference tier
- [ ] 1.3 Resolve Open Question 2 — adopter note enumerates affected GraphQL fields exhaustively, or
      states the rule. The enumeration in 2.1 produces the list either way

## 2. Enumerate before changing anything

- [ ] 2.1 **Enumerate the actual marshalled top-level shape of every subject routed through
      `handleNATSResponseWithExtensions`** — both families, all ~19 subjects. This is the discharge of
      the design's one data-loss risk (D1), and it also produces the adopter note's field list.
      Record the table in the PR
- [ ] 2.2 **Assert non-collision** in a table-driven test: no current payload matches the envelope
      discriminator without BEING an envelope. Falsifiable — adding a fake `{data,timestamp}`-only
      non-envelope case to the table must turn it RED
- [ ] 2.3 Confirm `graph.query.prefix` (`PrefixQueryResponse` = `{entities, next_cursor}`) fails the
      discriminator on both required keys, by test rather than by reading
- [ ] 2.4 **Disposition `graph.query.capabilities`** — routed by the gateway with no producer-side
      registration found. Grep for the consumer AND the producer; if dead, delete the route in a
      SEPARATE commit; if a real gap, file an issue. Do NOT fix it inside this change
- [ ] 2.5 **Confirm the `error` key is phantom before deleting its branch (D5):** grep every producer
      for an `error` key set on this envelope. A phantom is phantom once the PRODUCER side is checked,
      not once the type is read

## 3. Detector in `graph` (framework package — new exported surface)

- [ ] 3.1 Add the envelope predicate/unwrapper beside `QueryResponse[T]` in
      `graph/query_contracts.go`, per D2 — one description of the shape, co-located with the type, so
      a field added to the struct lands beside the predicate that must account for it
- [ ] 3.2 Discriminator is the CLOSED key set: `data` AND `timestamp` present, every key drawn from
      `{data, request_id, timestamp}`. Not `has("data")` — that is the data-loss direction
- [ ] 3.3 Unwrap EXACTLY ONCE (D3); no re-testing of the unwrapped payload. Test with an envelope
      whose `data` is itself envelope-shaped: exactly one layer comes off
- [ ] 3.4 Non-envelope input is returned byte-for-byte unchanged, and failing detection is NOT an
      error condition
- [ ] 3.5 Exported-surface rules from `.agents/contracts/semstreams-developer.md` apply: doc comment
      states the contract, and names what the function does NOT promise

## 4. Gateway wiring

- [ ] 4.1 Replace the `strings.HasPrefix(subject, "graph.index.query.")` gate at
      `gateway/graph-gateway/component.go:1720` with the detector call. The subject stops
      participating in the decision entirely
- [ ] 4.2 **Delete the dead `Error string` branch** (D5), and replace the stale comment describing
      `{data, error, timestamp: time}` with one naming the real error path (natsclient's `error: `
      body convention). Gated on 2.5 confirming
- [ ] 4.3 Detection runs before the `graph.query.prefix` special case, and the ORDER is pinned by a
      test (D4) — a future field addition must break a test, not a deployment
- [ ] 4.4 `graph.query.summary` → `graphSummary` projects `total_entities` at the top level. This is
      gh#762's motivating instance; it is a test case, not the acceptance criterion

## 5. The shape gate (gh#768) — falsifiable or it is not a gate

- [ ] 5.1 Add the e2e response-shape stage in the tier chosen at 1.2
- [ ] 5.2 Assertions are over **raw JSON keys**, never a decoded struct — both shapes unmarshal
      cleanly into a permissive target, so a decoding test passes under the defect AND the fix
- [ ] 5.3 Assert the ABSENCE of `data.data.*`, not merely the presence of expected leaves. Reaching
      the right value is exactly what the broken shape also permits
- [ ] 5.4 Cover representative subjects from **both** families, not only `graph.query.*`
- [ ] 5.5 **Record the stage RED against unfixed main, then green with the fix**, with output pasted
      in the PR. A stage never seen red is not evidence. Use `git stash` for the fails-without-fix
      check, never `git checkout`
- [ ] 5.6 Confirm the stage actually RAN and asserted — count assertions/PASSes; a green new gate may
      have skipped everything

## 6. Adopter obligation (ours: publish; theirs: conform)

- [ ] 6.1 Adopter note in `docs/operations/` — the shape change, affected fields per 1.3, and the
      before/after for each
- [ ] 6.2 Note that this lands INSIDE the v1.0.0-beta.159 lockstep wave, so adopters conform once.
      Activated by the tag alongside gh#753
- [ ] 6.3 **Task-list residency:** no cross-repo adoption task belongs in this file. Sister migration
      is gh#753's; problems they hit become new issues here

## 7. Gates

- [ ] 7.1 `task lint` clean (revive warnings = CI failure)
- [ ] 7.2 **Run what CI runs — BOTH suites:** `go test -race ./...` AND
      `go test -race -tags=integration -p 2 ./...`. Half of CI is how the last increment went red
- [ ] 7.3 Full `go vet` plain AND `-tags=integration` (`go test`'s vet is a SUBSET — no copylocks)
- [ ] 7.4 `task schema:generate` → no drift; `openspec validate --all --strict`
- [ ] 7.5 **BREAKING ⇒ a relevant e2e tier green before merge**, beyond the per-PR statistical. The
      gateway path is consumer-facing: run the tier covering it and state which
- [ ] 7.6 Re-verify against main after any long merge queue — `strict_required_status_checks_policy`
      is false, so checks can have run against a stale base

## 8. Review chain

- [ ] 8.1 `semstreams-reviewer` on the full diff
- [ ] 8.2 Owner-run Codex round; fix findings. **A fix is new code and inherits the full defect
      rate** — the remedy gets the same adversarial pass as the original. Codex reviews ONCE; a
      re-check is warranted only by a NEW blocking-class defect, a change to the reviewed CONTRACT,
      material scope growth, or a fix touching a SHARED primitive with callers outside the change
- [ ] 8.3 **Do not arm `--auto` until the Codex round closes** — this is a CODE PR. The ruleset
      requires 0 approvals and does not dismiss stale reviews on push, so arming early means a
      post-review fix push auto-merges UNREVIEWED the moment checks go green
- [ ] 8.4 One PR: detector + gateway + shape stage + adopter note (PR scope = complete system).
      Close gh#762 and gh#768 only on owner CONFIRM-CLOSE
- [ ] 8.5 Apply the delta and archive; `gateway-response-projection` is a NEW capability home, so its
      Purpose is WRITTEN at seed time, never the `TBD - created by archiving` stub
