# Tasks — entity-read-with-revision (gh#851)

**Amend a task line when the work HAPPENS, not only when it succeeds.** A deliberate
not-done gets `[~]`, its reasoning, AND propagation into the spec delta. Run
`task openspec:queue` before archiving.

## 1. Wire surface — opt-in revision on the entity query lane

- [ ] 1.1 Add `include_revision` to the entity query request type and the versioned
      `{entity, revision}` response type in `graph/` (additive; failing decode/round-trip
      tests first, production-decoder round-trip per payload).
- [ ] 1.2 Handler: when the flag is set, return the envelope with `entry.Revision()` from the
      same `Get` that produced the bytes (`processor/graph-ingest/query.go:87-104`); flag
      absent → existing bare bytes, byte-identical. Failing test pins both shapes.
- [ ] 1.3 Error-contract parity test: not-found, stub, and poisoned entity respond
      identically with and without the flag (spec scenario "Error contracts unchanged").
- [ ] 1.4 `task schema:generate` + `git diff schemas/ specs/` clean.

## 2. Client surface — projection package

- [ ] 2.1 Revision-bearing read variant on the client; extend the reader capability as a
      second narrow interface (bridge optional capabilities via the narrow method set —
      existing fakes must keep compiling). Bare-reply-from-old-server → typed
      revision-unavailable outcome; test both.
- [ ] 2.2 `ReplaceOwnedMutation.ExpectedRevision` (additive, zero = today's behavior
      exactly); pass through at request build (`mutation_client.go:711-718`). Failing test:
      non-zero flows to the wire request unchanged.
- [ ] 2.3 Make `MutationRevisionConflict` reachable and prove it: conflict test through the
      client returns the typed outcome with commit state not-committed. Mutation-check the
      WIRING: drop the pass-through and confirm the test FAILS (a mutation on the mapping
      alone proves nothing calls it).

## 3. Production-wire proof and docs

- [ ] 3.1 Integration test over NATS (testcontainers): revision read → two competing
      conditional writers from one revision → exactly one committed, one typed conflict →
      loser refetches and succeeds. Drives the real subjects, not the handler seam.
- [ ] 3.2 Versioned read-modify-write example in `docs/operations/34-projection-mutation-client.md`
      (the doc gh#851 asks to carry it), framed as revision-is-a-retry-token.
- [ ] 3.3 Reply on gh#851 with the shipped surface and the explicit scope answer (read half +
      conditional pass-through here; claim primitive is gh#689's change) so SemMachina can
      unblock task 8.1. Reply on gh#689 pointing at the surface it will consume.
      Communicate, do not edit — sister repos are hands-off.

## 4. Gates

- [ ] 4.1 `gofmt`, `task lint`, `go vet ./...` plain + `-tags=integration`.
- [ ] 4.2 BOTH suites: `go test -race ./...` AND
      `go test -race -tags=integration -p 2 -count=1 ./...`; grep `^FAIL`.
- [ ] 4.3 `go test ./test/contract/...`.
- [ ] 4.4 `task e2e:core` minimum; `task e2e:structural` if the graph-write path shows any
      drift.
- [ ] 4.5 `semstreams-reviewer` pass on the full diff.
- [ ] 4.6 Owner-run Codex round; arm `--auto` only AFTER it closes.
- [ ] 4.7 Owner CONFIRM-CLOSE before closing gh#851 (gh#689 stays open — its change consumes
      this surface).
- [ ] 4.8 Rebase note: if gh#810's rework lands first, re-run integration after rebase (the
      lane's callers gain the ack-rejection error class at `ClassifyReply`; no merge overlap
      expected).
