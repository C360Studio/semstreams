# Tasks — immutable-birth-predicates (gh#818)

**Amend a task line when the work HAPPENS, not only when it succeeds.** A deliberate
not-done gets `[~]`, its reasoning, AND propagation into the spec delta. Run
`task openspec:queue` before archiving.

## 1. Declaration surface

- [ ] 1.1 Add `Immutable bool` to vocabulary predicate metadata with `WithImmutable` option,
      mirroring the `RuleOpaque`/`WithRuleOpaque` pattern (`vocabulary/predicates.go:414-425`,
      `registry.go:190-203`); failing registration/lookup tests first.
- [ ] 1.2 Document the authority split (declaration grants nothing; mutation-lane access is
      the trust boundary; seeding principals are host ACL policy) in the predicate-contract
      docs — the gh#818 acceptance item on NATS ACL responsibility.

## 2. The shared gate

- [ ] 2.1 Implement the one gate function: (resident entity view, incoming delta,
      classification lookup) → refusal | filtered delta | pass. Pin the frozen-equality basis
      (canonical object value set + datatype, order-independent, envelope volatiles excluded)
      with a round-trip test; mutation-check by moving one volatile field into the basis and
      confirming the replay test FAILS.
- [ ] 2.2 Add `immutable_predicate` to the closed error-code set
      (`graph/mutation_responses.go`) with `errs.ErrorInvalid` class; the detail carries
      entity, predicate, lane. Contract test for the code's stability.

## 3. Enforcement wiring — every write path

- [ ] 3.1 Wire the gate into all eight request/reply handlers **inside each lane's existing
      CAS closure / resident-read** so retries re-evaluate against the state they replace
      (refresh the guard's baseline on every write path). Enumerate the lanes from
      `setupMutationHandlers` (`mutations.go:78-135`), not from this list.
- [ ] 3.2 Wire the Graphable merge preserve-and-continue by generalizing the
      indexing-profile drop-before-merge (`component.go:2574-2583`); add
      `immutable_drops_total` and the three-fact Warn. The hardcoded indexing-profile path
      stays as-is (its contract is ADR-054's, not this one) — record the relationship in a
      code comment at the seam.
- [ ] 3.3 Wire the delete refusal (`handleEntityDelete`) naming the frozen predicates
      present.
- [ ] 3.4 Grep-sweep for any write path outside the eight lanes + merge + delete that
      commits to `ENTITY_STATES` (restamp-stub path `mutations.go:653`, boot sweep, hierarchy
      inference) and either wire the gate or record with reasoning why the path cannot touch
      a frozen predicate. A guarantee defines its hole-class scope — cover the FULL class.

## 4. Integration proof — every lane, per the gh#818 acceptance list

- [ ] 4.1 Integration tests over real graph-ingest: for EACH of the eight lanes — divergent
      write refused with the stable code, exact replay no-ops; plus Graphable
      preserve-and-continue (divergent + exact), delete refusal, and late-declaration freeze.
      Drive the production wire for at least one lane end-to-end.
- [ ] 4.2 Mutation-check the wiring, not the primitive: disable the gate call in ONE lane and
      confirm that lane's test FAILS (per-lane, so a missed lane cannot hide behind the
      others).
- [ ] 4.3 Seed-authority test: a writer with lane access seeds; the same writer's divergent
      rewrite refuses (caller-independence pinned).

## 5. Downstream and gates

- [ ] 5.1 Reply on gh#818 with the shipped contract and the deferred privileged-teardown
      note so SemMachina can re-run mystery-companion-acceptance task 1.5 against real
      graph-ingest (communicate, do not edit — sister repos are hands-off).
- [ ] 5.2 `gofmt`, `task lint`, `go vet ./...` plain + `-tags=integration`.
- [ ] 5.3 BOTH suites: `go test -race ./...` AND
      `go test -race -tags=integration -p 2 -count=1 ./...`; grep `^FAIL`.
- [ ] 5.4 `task schema:generate` + `git diff schemas/ specs/` clean;
      `go test ./test/contract/...`.
- [ ] 5.5 `task e2e:structural` (graph-write path tier).
- [ ] 5.6 `semstreams-reviewer` pass on the full diff.
- [ ] 5.7 Owner-run Codex round; arm `--auto` only AFTER it closes.
- [ ] 5.8 Owner CONFIRM-CLOSE before closing gh#818.
